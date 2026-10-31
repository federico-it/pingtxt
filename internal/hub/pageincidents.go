package hub

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"mime"
	"mime/quotedprintable"
	"net/http"
	"net/mail"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/federico-it/pingtxt/internal/store"
)

// Incidents written on status pages, and the email subscribers who get
// their updates.
//
// An incident has a title, an impact and a timeline of updates; each update
// has a status (investigating, identified, monitoring, resolved) and a
// message. Visitors of a page can subscribe with their email address when
// the page has an email channel for it: they confirm through a link, then
// get every update posted with "notify", each email with its own
// unsubscribe link.

var (
	reportStatuses = []string{store.ReportInvestigating, store.ReportIdentified, store.ReportMonitoring, store.ReportResolved}
	reportImpacts  = []string{store.ImpactNone, store.ImpactMinor, store.ImpactMajor, store.ImpactCritical}
)

const (
	maxReportTitle   = 200
	maxReportMessage = 5000
	// maxPageSubscribers caps the addresses of one page, confirmed or not.
	maxPageSubscribers = 10000
	// Unconfirmed addresses are forgotten after a week.
	subscribeConfirmTTL = 7 * 24 * time.Hour
	// publicReportLimit caps the incidents shown on a public page.
	publicReportLimit = 20
	// Subscription requests allowed per client address and per email
	// address in the rate limit window.
	subscribesPerAddr  = 10
	subscribesPerEmail = 3
)

// subscribeLimits throttles subscription requests, which send emails.
type subscribeLimits struct {
	once  sync.Once
	addr  *failLimiter
	email *failLimiter
}

func (l *subscribeLimits) init() {
	l.once.Do(func() {
		l.addr = newFailLimiter(subscribesPerAddr)
		l.email = newFailLimiter(subscribesPerEmail)
	})
}

// dropPageCache makes the next public request rebuild the pages.
func (h *Hub) dropPageCache() {
	h.pages.mu.Lock()
	h.pages.cache = nil
	h.pages.mu.Unlock()
}

func pathInt(r *http.Request, name string) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue(name), 10, 64)
	return id, err == nil && id > 0
}

// pageFromPath loads the status page of the {id} path value, answering the
// request itself when it is missing.
func (h *Hub) pageFromPath(w http.ResponseWriter, r *http.Request) *store.StatusPage {
	id, ok := pathID(r)
	if !ok {
		writeErr(w, 400, "invalid id")
		return nil
	}
	p, err := h.store.StatusPage(id)
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, 404, "status page not found")
		return nil
	} else if err != nil {
		h.internalErr(w, err)
		return nil
	}
	return p
}

// ---------- admin API: incidents ----------

type reportInput struct {
	Title   string `json:"title"`
	Impact  string `json:"impact"`
	Status  string `json:"status"`
	Message string `json:"message"`
	// Notify emails the update to the page's subscribers (default true).
	Notify *bool `json:"notify"`
}

func (in *reportInput) notify() bool { return in.Notify == nil || *in.Notify }

func checkTitle(title string) (string, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return "", errors.New("title is required")
	}
	if utf8.RuneCountInString(title) > maxReportTitle || strings.ContainsAny(title, "\r\n") {
		return "", fmt.Errorf("title must be one line of at most %d characters", maxReportTitle)
	}
	return title, nil
}

func checkImpact(impact string) (string, error) {
	if impact == "" {
		return store.ImpactMinor, nil
	}
	if !slices.Contains(reportImpacts, impact) {
		return "", errors.New("impact must be none, minor, major or critical")
	}
	return impact, nil
}

func checkUpdate(status, message string) (string, string, error) {
	if !slices.Contains(reportStatuses, status) {
		return "", "", errors.New("status must be investigating, identified, monitoring or resolved")
	}
	message = strings.TrimSpace(strings.ReplaceAll(message, "\r\n", "\n"))
	if message == "" {
		return "", "", errors.New("message is required")
	}
	if utf8.RuneCountInString(message) > maxReportMessage {
		return "", "", fmt.Errorf("message is longer than %d characters", maxReportMessage)
	}
	return status, message, nil
}

func (h *Hub) listPageIncidents(w http.ResponseWriter, r *http.Request, _ *store.User) {
	p := h.pageFromPath(w, r)
	if p == nil {
		return
	}
	list, err := h.store.PageIncidents(p.ID, 0)
	if err != nil {
		h.internalErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"incidents": list})
}

func (h *Hub) createPageIncident(w http.ResponseWriter, r *http.Request, _ *store.User) {
	p := h.pageFromPath(w, r)
	if p == nil {
		return
	}
	var in reportInput
	if readJSON(r, &in) != nil {
		writeErr(w, 400, "invalid body")
		return
	}
	if in.Status == "" {
		in.Status = store.ReportInvestigating
	}
	inc := &store.PageIncident{PageID: p.ID}
	u := &store.PageIncidentUpdate{}
	var err error
	if inc.Title, err = checkTitle(in.Title); err == nil {
		if inc.Impact, err = checkImpact(in.Impact); err == nil {
			u.Status, u.Message, err = checkUpdate(in.Status, in.Message)
		}
	}
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if err := h.store.CreatePageIncident(inc, u); err != nil {
		h.internalErr(w, err)
		return
	}
	h.dropPageCache()
	n := 0
	if in.notify() {
		n = h.notifySubscribers(r, p, inc, *u)
	}
	writeJSON(w, 201, map[string]any{"incident": inc, "notified": n})
}

func (h *Hub) updatePageIncident(w http.ResponseWriter, r *http.Request, _ *store.User) {
	p := h.pageFromPath(w, r)
	if p == nil {
		return
	}
	id, ok := pathInt(r, "incident")
	if !ok {
		writeErr(w, 400, "invalid incident id")
		return
	}
	var in reportInput
	if readJSON(r, &in) != nil {
		writeErr(w, 400, "invalid body")
		return
	}
	inc := &store.PageIncident{ID: id, PageID: p.ID}
	var err error
	if inc.Title, err = checkTitle(in.Title); err == nil {
		inc.Impact, err = checkImpact(in.Impact)
	}
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if err := h.store.UpdatePageIncident(inc); errors.Is(err, store.ErrNotFound) {
		writeErr(w, 404, "incident not found")
		return
	} else if err != nil {
		h.internalErr(w, err)
		return
	}
	h.dropPageCache()
	h.writeIncident(w, p.ID, id)
}

func (h *Hub) deletePageIncident(w http.ResponseWriter, r *http.Request, _ *store.User) {
	p := h.pageFromPath(w, r)
	if p == nil {
		return
	}
	id, ok := pathInt(r, "incident")
	if !ok {
		writeErr(w, 400, "invalid incident id")
		return
	}
	if err := h.store.DeletePageIncident(p.ID, id); errors.Is(err, store.ErrNotFound) {
		writeErr(w, 404, "incident not found")
		return
	} else if err != nil {
		h.internalErr(w, err)
		return
	}
	h.dropPageCache()
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (h *Hub) addPageIncidentUpdate(w http.ResponseWriter, r *http.Request, _ *store.User) {
	p := h.pageFromPath(w, r)
	if p == nil {
		return
	}
	id, ok := pathInt(r, "incident")
	if !ok {
		writeErr(w, 400, "invalid incident id")
		return
	}
	var in reportInput
	if readJSON(r, &in) != nil {
		writeErr(w, 400, "invalid body")
		return
	}
	u := &store.PageIncidentUpdate{IncidentID: id}
	var err error
	if u.Status, u.Message, err = checkUpdate(in.Status, in.Message); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if err := h.store.AddPageIncidentUpdate(p.ID, u); errors.Is(err, store.ErrNotFound) {
		writeErr(w, 404, "incident not found")
		return
	} else if err != nil {
		h.internalErr(w, err)
		return
	}
	h.dropPageCache()
	inc, err := h.store.PageIncident(p.ID, id)
	if err != nil {
		h.internalErr(w, err)
		return
	}
	n := 0
	if in.notify() {
		n = h.notifySubscribers(r, p, inc, *u)
	}
	writeJSON(w, 201, map[string]any{"incident": inc, "notified": n})
}

func (h *Hub) deletePageIncidentUpdate(w http.ResponseWriter, r *http.Request, _ *store.User) {
	p := h.pageFromPath(w, r)
	if p == nil {
		return
	}
	id, ok1 := pathInt(r, "incident")
	uid, ok2 := pathInt(r, "update")
	if !ok1 || !ok2 {
		writeErr(w, 400, "invalid id")
		return
	}
	switch err := h.store.DeletePageIncidentUpdate(p.ID, id, uid); {
	case errors.Is(err, store.ErrNotFound):
		writeErr(w, 404, "update not found")
		return
	case errors.Is(err, store.ErrConflict):
		writeErr(w, 409, "an incident keeps at least one update: delete the incident instead")
		return
	case err != nil:
		h.internalErr(w, err)
		return
	}
	h.dropPageCache()
	h.writeIncident(w, p.ID, id)
}

// writeIncident answers with the incident as stored.
func (h *Hub) writeIncident(w http.ResponseWriter, pageID, id int64) {
	inc, err := h.store.PageIncident(pageID, id)
	if err != nil {
		h.internalErr(w, err)
		return
	}
	writeJSON(w, 200, inc)
}

// ---------- admin API: subscribers ----------

func (h *Hub) listPageSubscribers(w http.ResponseWriter, r *http.Request, _ *store.User) {
	p := h.pageFromPath(w, r)
	if p == nil {
		return
	}
	list, err := h.store.PageSubscribers(p.ID, false)
	if err != nil {
		h.internalErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"subscribers": list, "enabled": h.subscribeChannel(p) != nil})
}

func (h *Hub) deletePageSubscriber(w http.ResponseWriter, r *http.Request, _ *store.User) {
	p := h.pageFromPath(w, r)
	if p == nil {
		return
	}
	id, ok := pathInt(r, "subscriber")
	if !ok {
		writeErr(w, 400, "invalid subscriber id")
		return
	}
	if err := h.store.DeletePageSubscriber(p.ID, id); errors.Is(err, store.ErrNotFound) {
		writeErr(w, 404, "subscriber not found")
		return
	} else if err != nil {
		h.internalErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

// ---------- public API: subscriptions ----------

// subscribeChannel returns the SMTP settings that send the page's emails, or
// nil when subscriptions are off (no channel, disabled or not an email
// channel, or an invalid configuration).
func (h *Hub) subscribeChannel(p *store.StatusPage) *emailConfig {
	if p.SubscribeNotifierID == 0 {
		return nil
	}
	n, err := h.store.Notifier(p.SubscribeNotifierID)
	if err != nil || !n.Enabled || n.Type != "email" {
		return nil
	}
	cfg := make(map[string]string, len(n.Config))
	for k, v := range n.Config {
		cfg[k] = v
	}
	if cfg["to"] == "" {
		cfg["to"] = cfg["from"] // recipients come from the subscribers
	}
	ec, err := parseEmailConfig(cfg)
	if err != nil {
		return nil
	}
	return ec
}

// pageLinks are the addresses used in a page's emails.
type pageLinks struct {
	page string // the public page, ending with "/"
	api  string // base of the public API on the same host
}

// linksFor builds the links of a page: its custom domain when it has one,
// else the hub address (the public URL setting, or the request host).
func (h *Hub) linksFor(r *http.Request, p *store.StatusPage) pageLinks {
	if p.Domain != "" {
		base := "https://" + p.Domain
		return pageLinks{page: base + "/", api: base}
	}
	base := strings.TrimRight(h.hubURL(r), "/")
	return pageLinks{page: base + "/status/" + p.Slug + "/", api: base}
}

func (l pageLinks) confirm(token string) string     { return l.page + "?confirm=" + token }
func (l pageLinks) unsubscribe(token string) string { return l.page + "?unsubscribe=" + token }
func (l pageLinks) oneClick(token string) string {
	return l.api + "/api/public/subscriptions/" + token + "/unsubscribe"
}

func newSubscriberToken() string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// normalizeEmail returns the bare, lowercase address, or "" if invalid.
func normalizeEmail(s string) string {
	s = strings.TrimSpace(s)
	if s == "" || len(s) > 254 || strings.ContainsAny(s, "\r\n<>,;\"") {
		return ""
	}
	a, err := mail.ParseAddress(s)
	if err != nil || a.Address != s || !strings.Contains(a.Address[strings.LastIndexByte(a.Address, '@')+1:], ".") {
		return ""
	}
	return strings.ToLower(a.Address)
}

func (h *Hub) publicSubscribe(w http.ResponseWriter, r *http.Request) {
	h.serveSubscribe(w, r, strings.ToLower(r.PathValue("slug")))
}

// serveSubscribe adds an address to a page and sends it the confirmation
// link. The answer is the same whether the address was new, waiting or
// already confirmed, so the endpoint does not tell who is subscribed.
func (h *Hub) serveSubscribe(w http.ResponseWriter, r *http.Request, slug string) {
	notFound := func() { writeErr(w, 404, "status page not found") }
	if !slugRe.MatchString(slug) {
		notFound()
		return
	}
	p, err := h.store.StatusPageBySlug(slug)
	if errors.Is(err, store.ErrNotFound) || (err == nil && !p.Published) {
		notFound()
		return
	} else if err != nil {
		h.internalErr(w, err)
		return
	}
	ec := h.subscribeChannel(p)
	if ec == nil {
		writeErr(w, 404, "this status page has no email subscriptions")
		return
	}
	var in struct {
		Email string `json:"email"`
	}
	if readJSON(r, &in) != nil {
		writeErr(w, 400, "invalid body")
		return
	}
	email := normalizeEmail(in.Email)
	if email == "" {
		writeErr(w, 400, "enter a valid email address")
		return
	}

	h.subLimits.init()
	now := time.Now()
	wait, blocked := h.subLimits.addr.blocked(limitAddr(r), now)
	if w2, b := h.subLimits.email.blocked(email, now); b {
		blocked, wait = true, max(wait, w2)
	}
	if blocked {
		secs := max(int(wait.Seconds()+0.5), 1)
		w.Header().Set("Retry-After", strconv.Itoa(secs))
		writeErr(w, http.StatusTooManyRequests, "too many requests, try again in "+waitText(wait))
		return
	}
	h.subLimits.addr.fail(limitAddr(r), now)
	h.subLimits.email.fail(email, now)

	if _, err := h.store.PrunePageSubscribers(now.Add(-subscribeConfirmTTL).UnixMilli()); err != nil {
		h.log.Warn("prune subscribers", "err", err)
	}
	ok := func() { writeJSON(w, 202, map[string]bool{"ok": true}) }
	sub, err := h.store.PageSubscriberByEmail(p.ID, email)
	switch {
	case err == nil && sub.ConfirmedAt > 0:
		ok() // already subscribed: nothing to send
		return
	case err == nil:
		sub.Token = newSubscriberToken()
		err = h.store.RenewPageSubscriber(sub)
	case errors.Is(err, store.ErrNotFound):
		n, cerr := h.store.CountPageSubscribers(p.ID)
		if cerr != nil {
			h.internalErr(w, cerr)
			return
		}
		if n >= maxPageSubscribers {
			writeErr(w, 409, "this status page cannot take more subscribers")
			return
		}
		sub = &store.PageSubscriber{PageID: p.ID, Email: email, Token: newSubscriberToken()}
		err = h.store.CreatePageSubscriber(sub)
		if errors.Is(err, store.ErrConflict) { // a parallel request won
			ok()
			return
		}
	}
	if err != nil {
		h.internalErr(w, err)
		return
	}
	links := h.linksFor(r, p)
	msg := buildConfirmEmail(ec, p, sub, links, time.Now())
	h.sendBackground(ec, []outMail{{to: []string{sub.Email}, data: msg}}, "subscription confirmation", p.Slug)
	ok()
}

// subscriptionFromPath loads the subscriber of the {token} path value.
func (h *Hub) subscriptionFromPath(w http.ResponseWriter, r *http.Request) (*store.PageSubscriber, *store.StatusPage) {
	token := r.PathValue("token")
	if len(token) != 48 {
		writeErr(w, 404, "subscription not found")
		return nil, nil
	}
	sub, err := h.store.PageSubscriberByToken(token)
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, 404, "subscription not found")
		return nil, nil
	} else if err != nil {
		h.internalErr(w, err)
		return nil, nil
	}
	p, err := h.store.StatusPage(sub.PageID)
	if err != nil {
		h.internalErr(w, err)
		return nil, nil
	}
	return sub, p
}

func (h *Hub) confirmSubscription(w http.ResponseWriter, r *http.Request) {
	sub, p := h.subscriptionFromPath(w, r)
	if sub == nil {
		return
	}
	if sub.ConfirmedAt == 0 {
		if time.Since(time.UnixMilli(sub.CreatedAt)) > subscribeConfirmTTL {
			writeErr(w, 404, "subscription not found")
			return
		}
		if err := h.store.ConfirmPageSubscriber(sub.ID); err != nil {
			h.internalErr(w, err)
			return
		}
	}
	writeJSON(w, 200, map[string]any{"ok": true, "email": sub.Email, "page": p.Title})
}

func (h *Hub) unsubscribe(w http.ResponseWriter, r *http.Request) {
	sub, p := h.subscriptionFromPath(w, r)
	if sub == nil {
		return
	}
	if err := h.store.DeletePageSubscriber(sub.PageID, sub.ID); err != nil && !errors.Is(err, store.ErrNotFound) {
		h.internalErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "email": sub.Email, "page": p.Title})
}

// ---------- emails ----------

// notifySubscribers emails an update to the confirmed subscribers of the
// page, in the background. It returns how many emails were queued.
func (h *Hub) notifySubscribers(r *http.Request, p *store.StatusPage, inc *store.PageIncident, u store.PageIncidentUpdate) int {
	ec := h.subscribeChannel(p)
	if ec == nil || !p.Published {
		return 0
	}
	subs, err := h.store.PageSubscribers(p.ID, true)
	if err != nil {
		h.log.Error("list subscribers", "page", p.Slug, "err", err)
		return 0
	}
	if len(subs) == 0 {
		return 0
	}
	links := h.linksFor(r, p)
	now := time.Now()
	msgs := make([]outMail, len(subs))
	for i, s := range subs {
		msgs[i] = outMail{to: []string{s.Email}, data: buildUpdateEmail(ec, p, inc, u, s, links, now)}
	}
	h.sendBackground(ec, msgs, "incident update", p.Slug)
	return len(msgs)
}

// sendBackground delivers the messages without holding the request. The
// time allowed grows with the number of messages.
func (h *Hub) sendBackground(ec *emailConfig, msgs []outMail, what, slug string) {
	go func() {
		timeout := min(emailTimeout+time.Duration(len(msgs))*2*time.Second, time.Hour)
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		refused := 0
		err := h.notify.deliver(ctx, ec, msgs, func(i int, err error) {
			refused++
			h.log.Warn("subscriber email refused", "page", slug, "to", msgs[i].to, "err", err)
		})
		if err != nil {
			h.log.Error("send "+what, "page", slug, "emails", len(msgs), "err", err)
			return
		}
		h.log.Info("sent "+what, "page", slug, "emails", len(msgs)-refused)
	}()
}

// reportStatusText is the English label of an update status.
func reportStatusText(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

type emailHeaders struct {
	to, subject, unsubscribe string
}

// writeEmail renders a plain text message from the channel's sender.
func writeEmail(ec *emailConfig, hd emailHeaders, body string, now time.Time) []byte {
	domain := ec.From.Address[strings.LastIndexByte(ec.From.Address, '@')+1:]
	var b bytes.Buffer
	hdr := func(k, v string) { b.WriteString(k + ": " + v + "\r\n") }
	hdr("From", ec.From.String())
	hdr("To", hd.to)
	hdr("Subject", mime.QEncoding.Encode("utf-8", hd.subject))
	hdr("Date", now.Format(time.RFC1123Z))
	hdr("Message-ID", newMessageID(domain))
	hdr("MIME-Version", "1.0")
	hdr("Content-Type", "text/plain; charset=utf-8")
	hdr("Content-Transfer-Encoding", "quoted-printable")
	hdr("Auto-Submitted", "auto-generated")
	if hd.unsubscribe != "" {
		// RFC 8058 one-click unsubscribe, offered by mail clients.
		hdr("List-Unsubscribe", "<"+hd.unsubscribe+">")
		hdr("List-Unsubscribe-Post", "List-Unsubscribe=One-Click")
	}
	b.WriteString("\r\n")
	qp := quotedprintable.NewWriter(&b)
	_, _ = qp.Write([]byte(strings.ReplaceAll(body, "\n", "\r\n")))
	_ = qp.Close()
	return b.Bytes()
}

// oneLine keeps user text out of header syntax.
func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func buildConfirmEmail(ec *emailConfig, p *store.StatusPage, s *store.PageSubscriber, l pageLinks, now time.Time) []byte {
	body := fmt.Sprintf("Confirm that you want to get incident updates of %s by email:\n\n%s\n\n"+
		"The link expires in 7 days. If you did not ask for this, ignore this email: you will not get anything else.\n\n-- \nsent by pingtxt\n",
		oneLine(p.Title), l.confirm(s.Token))
	return writeEmail(ec, emailHeaders{to: s.Email, subject: "Confirm your subscription to " + oneLine(p.Title)}, body, now)
}

func buildUpdateEmail(ec *emailConfig, p *store.StatusPage, inc *store.PageIncident, u store.PageIncidentUpdate, s *store.PageSubscriber, l pageLinks, now time.Time) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "%s: %s\n\n", reportStatusText(u.Status), inc.Title)
	b.WriteString(u.Message)
	b.WriteString("\n\n")
	if inc.Impact != store.ImpactNone {
		fmt.Fprintf(&b, "Impact:  %s\n", inc.Impact)
	}
	fmt.Fprintf(&b, "Started: %s\n", time.UnixMilli(inc.CreatedAt).UTC().Format("2006-01-02 15:04 UTC"))
	if u.Status == store.ReportResolved && inc.ResolvedAt > 0 {
		fmt.Fprintf(&b, "Resolved: %s\n", time.UnixMilli(inc.ResolvedAt).UTC().Format("2006-01-02 15:04 UTC"))
	}
	fmt.Fprintf(&b, "\nStatus page: %s\n\n-- \nYou get this email because you subscribed to %s.\nUnsubscribe: %s\n",
		l.page, oneLine(p.Title), l.unsubscribe(s.Token))
	subject := fmt.Sprintf("[%s] %s: %s", oneLine(p.Title), reportStatusText(u.Status), inc.Title)
	return writeEmail(ec, emailHeaders{to: s.Email, subject: subject, unsubscribe: l.oneClick(s.Token)}, b.String(), now)
}

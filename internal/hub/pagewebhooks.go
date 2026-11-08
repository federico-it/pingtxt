package hub

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/federico-it/pingtxt/internal/store"
)

// Outgoing webhooks of status pages.
//
// Admins add addresses to a page; the hub POSTs the page's events to them
// as JSON: incidents written on the page (created, updated, resolved) and
// changes of the page status and of the status of its monitors, as visitors
// see them. Each request is signed with the webhook's secret:
//
//	X-Pingtxt-Signature: t=<unix seconds>,v1=<hex HMAC-SHA256 of "<t>.<body>">
//
// Only published pages send events. A failed delivery is retried twice.

// Status page webhook events.
const (
	PageEventIncidentCreated = "incident.created"
	PageEventIncidentUpdated = "incident.updated"
	PageEventIncidentResolve = "incident.resolved"
	PageEventStatusChanged   = "page.status_changed"
	PageEventMonitorChanged  = "monitor.status_changed"
	PageEventPing            = "ping" // test deliveries only
	// Maintenance windows announced on the page, see pagemaint.go.
	PageEventMaintScheduled  = "maintenance." + MaintScheduled
	PageEventMaintReminder   = "maintenance." + MaintReminder
	PageEventMaintInProgress = "maintenance." + MaintInProgress
	PageEventMaintCompleted  = "maintenance." + MaintCompleted
	PageEventMaintCancelled  = "maintenance." + MaintCancelled
)

// pageWebhookEvents are the events a webhook can pick. Other features add
// theirs here (e.g. maintenance.*).
var pageWebhookEvents = []string{
	PageEventIncidentCreated, PageEventIncidentUpdated, PageEventIncidentResolve,
	PageEventStatusChanged, PageEventMonitorChanged,
	PageEventMaintScheduled, PageEventMaintReminder, PageEventMaintInProgress, PageEventMaintCompleted, PageEventMaintCancelled,
}

const (
	maxPageWebhooks = 10
	// pageWatchEvery is how often the status of pages with webhooks is
	// compared with the last one seen.
	pageWatchEvery = 30 * time.Second
)

// pageWebhookRetry are the waits before the second and third try.
var pageWebhookRetry = []time.Duration{10 * time.Second, time.Minute}

// pageHooks remembers the status of the pages with webhooks, to send their
// changes. It starts empty: the first look at a page only records it.
// Each webhook gets its events one at a time, in order.
type pageHooks struct {
	mu     sync.Mutex
	seen   map[int64]pageSeen
	queues map[int64][]hookJob // by webhook id, while a sender runs
	retry  []time.Duration     // overrides pageWebhookRetry (tests only)
	wg     sync.WaitGroup      // deliveries in flight (tests only)
}

type hookJob struct {
	hook  *store.PageWebhook
	event string
	body  []byte
}

type pageSeen struct {
	status   string
	monitors map[int64]string
}

// ---------- admin API ----------

func newWebhookSecret() string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	return "whsec_" + hex.EncodeToString(b)
}

func validatePageWebhook(w *store.PageWebhook) error {
	w.URL = strings.TrimSpace(w.URL)
	u, err := url.Parse(w.URL)
	if w.URL == "" || len(w.URL) > 2048 || err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return errors.New("url must be an http:// or https:// address")
	}
	w.Secret = strings.TrimSpace(w.Secret)
	if w.Secret == "" {
		w.Secret = newWebhookSecret()
	}
	if len(w.Secret) < 16 || len(w.Secret) > 128 {
		return errors.New("secret must be 16 to 128 characters")
	}
	events := []string{}
	for _, e := range w.Events {
		if !slices.Contains(pageWebhookEvents, e) {
			return fmt.Errorf("unknown event %q", e)
		}
		if !slices.Contains(events, e) {
			events = append(events, e)
		}
	}
	w.Events = events
	return nil
}

func (h *Hub) listPageWebhooks(w http.ResponseWriter, r *http.Request, _ *store.User) {
	p := h.pageFromPath(w, r)
	if p == nil {
		return
	}
	list, err := h.store.PageWebhooks(p.ID)
	if err != nil {
		h.internalErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"webhooks": list, "events": pageWebhookEvents})
}

func (h *Hub) savePageWebhook(w http.ResponseWriter, r *http.Request, _ *store.User) {
	p := h.pageFromPath(w, r)
	if p == nil {
		return
	}
	create := r.Method == http.MethodPost
	hook := &store.PageWebhook{Enabled: true}
	if create {
		if readJSON(r, hook) != nil {
			writeErr(w, 400, "invalid body")
			return
		}
		n, err := h.store.CountPageWebhooks(p.ID)
		if err != nil {
			h.internalErr(w, err)
			return
		}
		if n >= maxPageWebhooks {
			writeErr(w, 409, fmt.Sprintf("a status page can have at most %d webhooks", maxPageWebhooks))
			return
		}
	} else {
		id, ok := pathInt(r, "webhook")
		if !ok {
			writeErr(w, 400, "invalid webhook id")
			return
		}
		cur, err := h.store.PageWebhook(p.ID, id)
		if errors.Is(err, store.ErrNotFound) {
			writeErr(w, 404, "webhook not found")
			return
		} else if err != nil {
			h.internalErr(w, err)
			return
		}
		hook = &store.PageWebhook{}
		if readJSONOver(r, cur, hook) != nil {
			writeErr(w, 400, "invalid body")
			return
		}
		hook.ID = id
	}
	hook.PageID = p.ID
	if err := validatePageWebhook(hook); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	var err error
	if create {
		err = h.store.CreatePageWebhook(hook)
	} else {
		err = h.store.UpdatePageWebhook(hook)
	}
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, 404, "webhook not found")
		return
	} else if err != nil {
		h.internalErr(w, err)
		return
	}
	got, err := h.store.PageWebhook(p.ID, hook.ID)
	if err != nil {
		h.internalErr(w, err)
		return
	}
	code := 200
	if create {
		code = 201
	}
	writeJSON(w, code, got)
}

func (h *Hub) deletePageWebhook(w http.ResponseWriter, r *http.Request, _ *store.User) {
	p := h.pageFromPath(w, r)
	if p == nil {
		return
	}
	id, ok := pathInt(r, "webhook")
	if !ok {
		writeErr(w, 400, "invalid webhook id")
		return
	}
	if err := h.store.DeletePageWebhook(p.ID, id); errors.Is(err, store.ErrNotFound) {
		writeErr(w, 404, "webhook not found")
		return
	} else if err != nil {
		h.internalErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

// testPageWebhook sends a "ping" event to one webhook, once, and answers
// with the result. It works on drafts and disabled webhooks too.
func (h *Hub) testPageWebhook(w http.ResponseWriter, r *http.Request, _ *store.User) {
	p := h.pageFromPath(w, r)
	if p == nil {
		return
	}
	id, ok := pathInt(r, "webhook")
	if !ok {
		writeErr(w, 400, "invalid webhook id")
		return
	}
	hook, err := h.store.PageWebhook(p.ID, id)
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, 404, "webhook not found")
		return
	} else if err != nil {
		h.internalErr(w, err)
		return
	}
	body := h.webhookBody(p, PageEventPing, map[string]any{"message": "Test event from pingtxt"})
	status, derr := h.postWebhook(r.Context(), hook, PageEventPing, body)
	h.recordWebhook(hook.ID, status, derr)
	out := map[string]any{"ok": derr == nil, "status": status}
	if derr != nil {
		out["error"] = derr.Error()
	}
	writeJSON(w, 200, out)
}

// ---------- events ----------

// pageURL is the public address of a page without a request: its custom
// domain, else the public URL setting ("" when neither is set).
func (h *Hub) pageURL(p *store.StatusPage) string {
	if p.Domain != "" {
		return "https://" + p.Domain + "/"
	}
	u := h.generalSettings().PublicURL
	if u == "" {
		return ""
	}
	if !strings.Contains(u, "://") {
		u = "https://" + u
	}
	return strings.TrimRight(u, "/") + "/status/" + p.Slug + "/"
}

// webhookBody builds the JSON of an event. data adds top-level fields when
// it is a map[string]any; any other value goes under the event's prefix
// ("maintenance" for "maintenance.started").
func (h *Hub) webhookBody(p *store.StatusPage, event string, data any) []byte {
	body := map[string]any{
		"event": event,
		"id":    newDeliveryID(),
		"at":    time.Now().UnixMilli(),
		"page":  map[string]any{"id": p.ID, "slug": p.Slug, "title": p.Title, "url": h.pageURL(p)},
	}
	switch d := data.(type) {
	case nil:
	case map[string]any:
		for k, v := range d {
			if _, ok := body[k]; !ok {
				body[k] = v
			}
		}
	default:
		key, _, _ := strings.Cut(event, ".")
		body[key] = d
	}
	b, _ := json.Marshal(body)
	return b
}

func newDeliveryID() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// pageEvent sends an event of a page to its enabled webhooks that want it,
// in the background. Drafts send nothing.
func (h *Hub) pageEvent(p *store.StatusPage, event string, data any) {
	if !p.Published {
		return
	}
	hooks, err := h.store.PageWebhooks(p.ID)
	if err != nil {
		h.log.Error("list page webhooks", "page", p.Slug, "err", err)
		return
	}
	var body []byte
	for _, hook := range hooks {
		if !hook.Enabled || (len(hook.Events) > 0 && !slices.Contains(hook.Events, event)) {
			continue
		}
		if body == nil {
			body = h.webhookBody(p, event, data)
		}
		h.queueWebhook(hookJob{hook: hook, event: event, body: body})
	}
}

// queueWebhook adds a delivery to the webhook's queue, starting its sender
// when none runs.
func (h *Hub) queueWebhook(j hookJob) {
	h.hooks.wg.Add(1)
	h.hooks.mu.Lock()
	defer h.hooks.mu.Unlock()
	if h.hooks.queues == nil {
		h.hooks.queues = map[int64][]hookJob{}
	}
	q, running := h.hooks.queues[j.hook.ID]
	h.hooks.queues[j.hook.ID] = append(q, j)
	if !running {
		go h.sendQueue(j.hook.ID)
	}
}

func (h *Hub) sendQueue(id int64) {
	for {
		h.hooks.mu.Lock()
		q := h.hooks.queues[id]
		if len(q) == 0 {
			delete(h.hooks.queues, id)
			h.hooks.mu.Unlock()
			return
		}
		j := q[0]
		h.hooks.queues[id] = q[1:]
		h.hooks.mu.Unlock()
		h.deliverWebhook(j.hook, j.event, j.body)
		h.hooks.wg.Done()
	}
}

// deliverWebhook posts one event, retrying when it fails.
func (h *Hub) deliverWebhook(hook *store.PageWebhook, event string, body []byte) {
	waits := pageWebhookRetry
	if h.hooks.retry != nil {
		waits = h.hooks.retry
	}
	var status int
	var err error
	for try := 0; ; try++ {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		status, err = h.postWebhook(ctx, hook, event, body)
		cancel()
		if err == nil || try >= len(waits) || (status >= 400 && status < 500 && status != 408 && status != 429) {
			break
		}
		time.Sleep(waits[try])
	}
	if err != nil {
		h.log.Warn("status page webhook failed", "webhook", hook.ID, "event", event, "err", err)
	}
	h.recordWebhook(hook.ID, status, err)
}

func (h *Hub) recordWebhook(id int64, status int, err error) {
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	if e := h.store.SetPageWebhookResult(id, time.Now().UnixMilli(), status, msg); e != nil {
		h.log.Warn("record webhook result", "webhook", id, "err", e)
	}
}

// webhookSignature is the X-Pingtxt-Signature value of a body sent at t.
func webhookSignature(secret string, t int64, body []byte) string {
	ts := strconv.FormatInt(t, 10)
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte(ts + "."))
	m.Write(body)
	return "t=" + ts + ",v1=" + hex.EncodeToString(m.Sum(nil))
}

// postWebhook sends one request and returns the HTTP status (0 = no answer).
func (h *Hub) postWebhook(ctx context.Context, hook *store.PageWebhook, event string, body []byte) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, hook.URL, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "pingtxt-webhook")
	req.Header.Set("X-Pingtxt-Event", event)
	req.Header.Set("X-Pingtxt-Signature", webhookSignature(hook.Secret, time.Now().Unix(), body))
	resp, err := h.notify.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
		return resp.StatusCode, fmt.Errorf("http %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	return resp.StatusCode, nil
}

// incidentEvent sends an update of an incident written on a page.
func (h *Hub) incidentEvent(p *store.StatusPage, inc *store.PageIncident, u store.PageIncidentUpdate, created bool) {
	event := PageEventIncidentUpdated
	switch {
	case created:
		event = PageEventIncidentCreated
	case u.Status == store.ReportResolved:
		event = PageEventIncidentResolve
	}
	h.pageEvent(p, event, map[string]any{"incident": inc, "update": u})
}

// ---------- status changes ----------

func (h *Hub) pageWatchLoop(ctx context.Context) {
	t := time.NewTicker(pageWatchEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			h.watchPages()
		}
	}
}

// watchPages compares the status of each page with webhooks, and of its
// monitors, with the last one seen and sends the changes.
func (h *Hub) watchPages() {
	ids, err := h.store.PagesWithWebhooks()
	if err != nil {
		h.log.Error("list pages with webhooks", "err", err)
		return
	}
	keep := map[int64]bool{}
	for _, id := range ids {
		keep[id] = true
		sp, err := h.store.StatusPage(id)
		if err != nil {
			continue
		}
		page, err := h.cachedPublicPage(sp.Slug)
		if err != nil {
			h.log.Warn("build status page", "page", sp.Slug, "err", err)
			continue
		}
		h.comparePage(sp, page)
	}
	h.hooks.mu.Lock()
	for id := range h.hooks.seen {
		if !keep[id] {
			delete(h.hooks.seen, id)
		}
	}
	h.hooks.mu.Unlock()
}

func (h *Hub) comparePage(sp *store.StatusPage, page *publicPage) {
	now := pageSeen{status: page.Status, monitors: map[int64]string{}}
	for _, m := range page.Monitors {
		now.monitors[m.ID] = m.Status
	}
	h.hooks.mu.Lock()
	if h.hooks.seen == nil {
		h.hooks.seen = map[int64]pageSeen{}
	}
	before, ok := h.hooks.seen[sp.ID]
	h.hooks.seen[sp.ID] = now
	h.hooks.mu.Unlock()
	if !ok {
		return
	}
	for _, m := range page.Monitors {
		old, had := before.monitors[m.ID]
		if !had || old == m.Status || old == StatusPending || m.Status == StatusPending {
			continue
		}
		h.pageEvent(sp, PageEventMonitorChanged, map[string]any{
			"monitor": map[string]any{"id": m.ID, "name": m.Name, "group": m.Group, "status": m.Status, "previous_status": old},
		})
	}
	if before.status != page.Status && before.status != PageUnknown && page.Status != PageUnknown {
		h.pageEvent(sp, PageEventStatusChanged, map[string]any{"status": page.Status, "previous_status": before.status})
	}
}

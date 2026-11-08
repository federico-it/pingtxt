package hub

import (
	"cmp"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/xml"
	"errors"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/federico-it/pingtxt/internal/store"
)

// RSS and Atom feeds of a status page.
//
// A feed lists what the page shows over time: the incidents written on the
// page (one entry each, with its whole timeline) and, when the page shows
// them, the outages the hub detected. Feeds follow the page's access rules:
// drafts have none, and a password protected page answers only visitors
// who unlocked it, logged-in users, or a private link whose key is derived
// from the password (changing the password changes the link).

const feedEntryLimit = 50

// feedKey is the key of a protected page's private feed links.
func feedKey(id int64, hash string) string {
	m := hmac.New(sha256.New, []byte("pingtxt status feed\x00"+hash))
	m.Write([]byte(strconv.FormatInt(id, 10)))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))[:32]
}

func feedKeyOK(r *http.Request, page *publicPage) bool {
	k := r.URL.Query().Get("key")
	return k != "" && page.passHash != "" && hmac.Equal([]byte(k), []byte(feedKey(page.id, page.passHash)))
}

func (h *Hub) publicAtom(w http.ResponseWriter, r *http.Request) {
	h.serveFeed(w, r, strings.ToLower(r.PathValue("slug")), "atom", false)
}

func (h *Hub) publicRSS(w http.ResponseWriter, r *http.Request) {
	h.serveFeed(w, r, strings.ToLower(r.PathValue("slug")), "rss", false)
}

func (h *Hub) serveFeed(w http.ResponseWriter, r *http.Request, slug, format string, byDomain bool) {
	page, err := h.cachedPublicPage(slug)
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, 404, "status page not found")
		return
	} else if err != nil {
		h.internalErr(w, err)
		return
	}
	if !page.Published && (byDomain || h.currentUser(r) == nil) {
		writeErr(w, 404, "status page not found")
		return
	}
	if !h.pageOpen(r, page, byDomain) && !feedKeyOK(r, page) {
		writeErr(w, http.StatusForbidden, "this status page needs a password")
		return
	}
	sp, err := h.store.StatusPageBySlug(slug)
	if err != nil {
		h.internalErr(w, err)
		return
	}
	links := h.linksFor(r, sp)
	f := buildFeed(page, links, h.feedLink(r, sp, format, ""))
	var body []byte
	if format == "atom" {
		w.Header().Set("Content-Type", "application/atom+xml; charset=utf-8")
		body, err = f.atom()
	} else {
		w.Header().Set("Content-Type", "application/rss+xml; charset=utf-8")
		body, err = f.rss()
	}
	if err != nil {
		h.internalErr(w, err)
		return
	}
	if page.Published && page.passHash == "" {
		w.Header().Set("Cache-Control", "public, max-age=60")
		w.Header().Set("Access-Control-Allow-Origin", "*")
	} else {
		w.Header().Set("Cache-Control", "private, no-store")
	}
	w.Header().Set("Last-Modified", time.UnixMilli(f.updated).UTC().Format(http.TimeFormat))
	_, _ = w.Write([]byte(xml.Header))
	_, _ = w.Write(body)
}

// feedLink is the address of a page's feed, on its custom domain when it
// has one. key is added for protected pages.
func (h *Hub) feedLink(r *http.Request, p *store.StatusPage, format, key string) string {
	var u string
	if p.Domain != "" {
		u = "https://" + p.Domain + "/api/public/status/feed." + format
	} else {
		u = strings.TrimRight(h.hubURL(r), "/") + "/api/public/status/" + p.Slug + "/feed." + format
	}
	if key != "" {
		u += "?key=" + url.QueryEscape(key)
	}
	return u
}

// pageFeedLinks gives admins the feed addresses of a page; for a protected
// page they carry the private key.
func (h *Hub) pageFeedLinks(w http.ResponseWriter, r *http.Request, _ *store.User) {
	p := h.pageFromPath(w, r)
	if p == nil {
		return
	}
	key := ""
	if p.PasswordHash != "" {
		key = feedKey(p.ID, p.PasswordHash)
	}
	writeJSON(w, 200, map[string]any{
		"atom":      h.feedLink(r, p, "atom", key),
		"rss":       h.feedLink(r, p, "rss", key),
		"private":   key != "",
		"published": p.Published,
	})
}

// ---------- building ----------

type feed struct {
	title, description string
	page, self         string
	host               string
	updated            int64
	entries            []feedEntry
}

type feedEntry struct {
	id        string
	title     string
	link      string
	published int64
	updated   int64
	html      string // content as HTML
	category  string
}

func buildFeed(p *publicPage, l pageLinks, self string) *feed {
	host := "pingtxt"
	if u, err := url.Parse(l.page); err == nil && u.Hostname() != "" {
		host = u.Hostname()
	}
	f := &feed{title: p.Title, description: p.Description, page: l.page, self: self, host: host}
	if f.description == "" {
		f.description = "Status of " + p.Title
	}
	for _, inc := range p.Reports {
		f.entries = append(f.entries, reportEntry(f, inc))
	}
	if p.ShowIncidents {
		for _, inc := range p.Incidents {
			f.entries = append(f.entries, outageEntry(f, inc))
		}
	}
	sortEntries(f.entries)
	if len(f.entries) > feedEntryLimit {
		f.entries = f.entries[:feedEntryLimit]
	}
	for _, e := range f.entries {
		f.updated = max(f.updated, e.updated)
	}
	if f.updated == 0 {
		f.updated = p.UpdatedAt
	}
	return f
}

// sortEntries puts the latest change first, ties by id for a stable order.
func sortEntries(list []feedEntry) {
	slices.SortStableFunc(list, func(a, b feedEntry) int {
		if c := cmp.Compare(b.updated, a.updated); c != 0 {
			return c
		}
		return strings.Compare(a.id, b.id)
	})
}

// tagID is a stable entry id (RFC 4151), not tied to the URL scheme.
func (f *feed) tagID(what string) string {
	return "tag:" + f.host + ",2026:" + what
}

func fmtUTC(ms int64) string {
	return time.UnixMilli(ms).UTC().Format("2006-01-02 15:04 UTC")
}

// textHTML turns plain text into HTML paragraphs.
func textHTML(s string) string {
	var b strings.Builder
	for _, para := range strings.Split(strings.TrimSpace(s), "\n\n") {
		if strings.TrimSpace(para) == "" {
			continue
		}
		b.WriteString("<p>")
		b.WriteString(strings.ReplaceAll(html.EscapeString(strings.TrimSpace(para)), "\n", "<br>"))
		b.WriteString("</p>")
	}
	return b.String()
}

func reportEntry(f *feed, inc *store.PageIncident) feedEntry {
	title := inc.Title
	if inc.ResolvedAt != 0 {
		title = "Resolved: " + title
	} else {
		title = reportStatusText(inc.Status) + ": " + title
	}
	var b strings.Builder
	if inc.Impact != "" && inc.Impact != store.ImpactNone {
		fmt.Fprintf(&b, "<p><strong>Impact:</strong> %s</p>", html.EscapeString(inc.Impact))
	}
	for _, u := range inc.Updates { // newest first, like the page
		fmt.Fprintf(&b, "<p><strong>%s</strong> · %s</p>%s", html.EscapeString(reportStatusText(u.Status)), fmtUTC(u.CreatedAt), textHTML(u.Message))
	}
	return feedEntry{
		id: f.tagID("incident-" + strconv.FormatInt(inc.ID, 10)), title: title,
		link:      f.page + "#incident-" + strconv.FormatInt(inc.ID, 10),
		published: inc.CreatedAt, updated: max(inc.UpdatedAt, inc.CreatedAt), html: b.String(), category: "incident",
	}
}

func outageEntry(f *feed, inc publicIncident) feedEntry {
	e := feedEntry{
		id: f.tagID("outage-" + strconv.FormatInt(inc.ID, 10)), link: f.page,
		published: inc.StartedAt, updated: inc.StartedAt, category: "outage",
	}
	name := html.EscapeString(inc.MonitorName)
	if inc.ResolvedAt == 0 {
		e.title = inc.MonitorName + " is down"
		e.html = fmt.Sprintf("<p>%s is down since %s.</p>", name, fmtUTC(inc.StartedAt))
	} else {
		e.title = inc.MonitorName + " was down"
		e.updated = inc.ResolvedAt
		e.html = fmt.Sprintf("<p>%s was down from %s to %s (%s).</p>", name, fmtUTC(inc.StartedAt), fmtUTC(inc.ResolvedAt),
			fmtDuration(time.Duration(inc.ResolvedAt-inc.StartedAt)*time.Millisecond))
	}
	return e
}

// ---------- Atom (RFC 4287) ----------

type atomLink struct {
	Href string `xml:"href,attr"`
	Rel  string `xml:"rel,attr,omitempty"`
	Type string `xml:"type,attr,omitempty"`
}

type atomText struct {
	Type string `xml:"type,attr,omitempty"`
	Body string `xml:",chardata"`
}

type atomCategory struct {
	Term string `xml:"term,attr"`
}

type atomEntry struct {
	Title     string        `xml:"title"`
	ID        string        `xml:"id"`
	Link      atomLink      `xml:"link"`
	Published string        `xml:"published"`
	Updated   string        `xml:"updated"`
	Category  *atomCategory `xml:"category,omitempty"`
	Content   atomText      `xml:"content"`
}

type atomFeed struct {
	XMLName  xml.Name   `xml:"http://www.w3.org/2005/Atom feed"`
	Title    string     `xml:"title"`
	Subtitle string     `xml:"subtitle,omitempty"`
	ID       string     `xml:"id"`
	Updated  string     `xml:"updated"`
	Links    []atomLink `xml:"link"`
	Author   struct {
		Name string `xml:"name"`
	} `xml:"author"`
	Generator string      `xml:"generator"`
	Entries   []atomEntry `xml:"entry"`
}

func rfc3339(ms int64) string { return time.UnixMilli(ms).UTC().Format(time.RFC3339) }

func (f *feed) atom() ([]byte, error) {
	a := atomFeed{
		Title: f.title, Subtitle: f.description, ID: f.tagID("page"), Updated: rfc3339(f.updated),
		Links:     []atomLink{{Href: f.page, Rel: "alternate", Type: "text/html"}, {Href: f.self, Rel: "self", Type: "application/atom+xml"}},
		Generator: "pingtxt", Entries: []atomEntry{},
	}
	a.Author.Name = f.title
	for _, e := range f.entries {
		a.Entries = append(a.Entries, atomEntry{
			Title: e.title, ID: e.id, Link: atomLink{Href: e.link, Rel: "alternate", Type: "text/html"},
			Published: rfc3339(e.published), Updated: rfc3339(e.updated), Category: &atomCategory{Term: e.category},
			Content: atomText{Type: "html", Body: e.html},
		})
	}
	return xml.MarshalIndent(a, "", "  ")
}

// ---------- RSS 2.0 ----------

type rssGUID struct {
	IsPermaLink string `xml:"isPermaLink,attr"`
	Value       string `xml:",chardata"`
}

type rssItem struct {
	Title       string  `xml:"title"`
	Link        string  `xml:"link"`
	GUID        rssGUID `xml:"guid"`
	PubDate     string  `xml:"pubDate"`
	Category    string  `xml:"category,omitempty"`
	Description string  `xml:"description"`
}

type rssChannel struct {
	Title         string    `xml:"title"`
	Link          string    `xml:"link"`
	Description   string    `xml:"description"`
	AtomLink      atomLink  `xml:"http://www.w3.org/2005/Atom link"`
	LastBuildDate string    `xml:"lastBuildDate"`
	Generator     string    `xml:"generator"`
	TTL           int       `xml:"ttl"`
	Items         []rssItem `xml:"item"`
}

type rssFeed struct {
	XMLName xml.Name   `xml:"rss"`
	Version string     `xml:"version,attr"`
	Channel rssChannel `xml:"channel"`
}

func rfc1123(ms int64) string { return time.UnixMilli(ms).UTC().Format(time.RFC1123Z) }

func (f *feed) rss() ([]byte, error) {
	c := rssChannel{
		Title: f.title, Link: f.page, Description: f.description,
		AtomLink:      atomLink{Href: f.self, Rel: "self", Type: "application/rss+xml"},
		LastBuildDate: rfc1123(f.updated), Generator: "pingtxt", TTL: 5, Items: []rssItem{},
	}
	for _, e := range f.entries {
		// pubDate is the latest change, so an update moves the incident up.
		c.Items = append(c.Items, rssItem{
			Title: e.title, Link: e.link, GUID: rssGUID{IsPermaLink: "false", Value: e.id},
			PubDate: rfc1123(e.updated), Category: e.category, Description: e.html,
		})
	}
	return xml.MarshalIndent(rssFeed{Version: "2.0", Channel: c}, "", "  ")
}

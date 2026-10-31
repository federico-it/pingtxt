package hub

import (
	"errors"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/federico-it/pingtxt/internal/store"
)

// statusPages holds the custom domain routing table and a short cache of the
// public page payloads, so a busy status page does not hammer SQLite.
type statusPages struct {
	mu      sync.RWMutex
	domains map[string]string // domain -> slug
	cache   map[string]cachedPage
	badges  map[string]cachedBadge // see badges.go
}

type cachedPage struct {
	at   time.Time
	page *publicPage
}

const publicCacheTTL = 15 * time.Second

func (h *Hub) reloadPageDomains() {
	d, err := h.store.StatusPageDomains()
	if err != nil {
		h.log.Error("load status page domains", "err", err)
		return
	}
	h.pages.mu.Lock()
	h.pages.domains = d
	h.pages.cache = nil
	h.pages.badges = nil
	h.pages.mu.Unlock()
}

// pageSlugForHost returns the slug of the page served on a custom domain.
func (h *Hub) pageSlugForHost(r *http.Request) string {
	h.pages.mu.RLock()
	defer h.pages.mu.RUnlock()
	if len(h.pages.domains) == 0 {
		return ""
	}
	for _, host := range []string{r.Host, r.Header.Get("X-Forwarded-Host")} {
		if slug, ok := h.pages.domains[normalizeHost(host)]; ok {
			return slug
		}
	}
	return ""
}

func normalizeHost(host string) string {
	host = strings.ToLower(strings.TrimSpace(host))
	if i := strings.IndexByte(host, ','); i >= 0 {
		host = strings.TrimSpace(host[:i])
	}
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	return strings.TrimSuffix(host, ".")
}

// customDomains serves only the public status page on hosts that belong to
// one: the admin UI and API are not reachable there.
func (h *Hub) customDomains(next http.Handler, ui fs.FS) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		slug := h.pageSlugForHost(r)
		if slug == "" {
			next.ServeHTTP(w, r)
			return
		}
		p := r.URL.Path
		switch {
		case (p == "/api/public/status" || p == "/api/public/status/") && r.Method == http.MethodGet:
			h.servePublicPage(w, r, slug, true)
		case p == "/api/public/status/subscribe" && r.Method == http.MethodPost:
			h.serveSubscribe(w, r, slug, true)
		case p == "/api/public/status/unlock" && r.Method == http.MethodPost:
			h.serveUnlock(w, r, slug)
		case p == "/api/health", strings.HasPrefix(p, "/api/public/subscriptions/"), strings.HasPrefix(p, "/_next/"), p == "/icon.svg",
			strings.HasPrefix(p, "/api/badge/"+slug+"/"): // badges of this page only
			next.ServeHTTP(w, r)
		case strings.HasPrefix(p, "/api/"), strings.HasPrefix(p, "/download/"), p == "/install-agent.sh":
			writeErr(w, http.StatusNotFound, "not found")
		default:
			if _, err := fs.Stat(ui, "status/index.html"); err != nil {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Cache-Control", "no-cache")
			serveFile(w, r, ui, "status/index.html")
		}
	})
}

// ---------- admin API ----------

var (
	slugRe   = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,48}[a-z0-9])?$`)
	domainRe = regexp.MustCompile(`^(?:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z0-9-]{2,63}$`)
)

func (h *Hub) validateStatusPage(r *http.Request, p *store.StatusPage) error {
	p.Slug = strings.ToLower(strings.TrimSpace(p.Slug))
	p.Title = strings.TrimSpace(p.Title)
	p.Description = strings.TrimSpace(p.Description)
	p.Domain = strings.TrimSpace(strings.ToLower(p.Domain))
	if u, err := url.Parse(p.Domain); err == nil && u.Host != "" {
		p.Domain = u.Host // pasted as a URL
	}
	p.Domain = normalizeHost(p.Domain)
	if p.Title == "" {
		return errors.New("title is required")
	}
	if len(p.Title) > 100 || len(p.Description) > 1000 {
		return errors.New("title or description is too long")
	}
	if !slugRe.MatchString(p.Slug) {
		return errors.New("slug must be 1-50 lowercase letters, digits or dashes")
	}
	if p.Domain != "" {
		if !domainRe.MatchString(p.Domain) {
			return errors.New("domain must be a hostname like status.example.com")
		}
		// Pointing a page at the hub's own host would hide the admin UI.
		own := []string{normalizeHost(r.Host)}
		if u, err := url.Parse(h.hubURL(r)); err == nil && u.Host != "" {
			own = append(own, normalizeHost(u.Host))
		}
		for _, o := range own {
			if p.Domain == o {
				return errors.New("domain cannot be the address of this pingtxt instance")
			}
		}
	}
	seen := map[int64]bool{}
	ids := []int64{}
	for _, id := range p.MonitorIDs {
		if seen[id] {
			continue
		}
		seen[id] = true
		if _, err := h.store.Monitor(id); err == nil {
			ids = append(ids, id)
		}
	}
	p.MonitorIDs = ids
	gseen := map[int64]bool{}
	gids := []int64{}
	for _, id := range p.GroupIDs {
		if gseen[id] {
			continue
		}
		gseen[id] = true
		if _, err := h.store.Group(id); err == nil {
			gids = append(gids, id)
		}
	}
	p.GroupIDs = gids
	if p.SubscribeNotifierID < 0 {
		p.SubscribeNotifierID = 0
	}
	if p.SubscribeNotifierID != 0 {
		n, err := h.store.Notifier(p.SubscribeNotifierID)
		if err != nil || n.Type != "email" {
			return errors.New("email subscriptions need an email notification channel")
		}
	}
	return nil
}

func (h *Hub) listStatusPages(w http.ResponseWriter, r *http.Request, _ *store.User) {
	list, err := h.store.ListStatusPages()
	if err != nil {
		h.internalErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"pages": list, "hub_url": h.hubURL(r)})
}

func (h *Hub) getStatusPage(w http.ResponseWriter, r *http.Request, _ *store.User) {
	id, ok := pathID(r)
	if !ok {
		writeErr(w, 400, "invalid id")
		return
	}
	p, err := h.store.StatusPage(id)
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, 404, "status page not found")
		return
	} else if err != nil {
		h.internalErr(w, err)
		return
	}
	writeJSON(w, 200, p)
}

func (h *Hub) saveStatusPage(w http.ResponseWriter, r *http.Request, _ *store.User) {
	var p store.StatusPage
	if readJSON(r, &p) != nil {
		writeErr(w, 400, "invalid body")
		return
	}
	create := r.Method == http.MethodPost
	if !create {
		id, ok := pathID(r)
		if !ok {
			writeErr(w, 400, "invalid id")
			return
		}
		p.ID = id
	}
	if err := h.validateStatusPage(r, &p); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	old := ""
	if !create {
		cur, err := h.store.StatusPage(p.ID)
		if errors.Is(err, store.ErrNotFound) {
			writeErr(w, 404, "status page not found")
			return
		} else if err != nil {
			h.internalErr(w, err)
			return
		}
		old = cur.PasswordHash
	}
	if err := applyPagePassword(&p, old); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	var err error
	if create {
		err = h.store.CreateStatusPage(&p)
	} else {
		err = h.store.UpdateStatusPage(&p)
	}
	switch {
	case errors.Is(err, store.ErrConflict):
		writeErr(w, 409, "slug or domain already used by another status page")
		return
	case errors.Is(err, store.ErrNotFound):
		writeErr(w, 404, "status page not found")
		return
	case err != nil:
		h.internalErr(w, err)
		return
	}
	h.reloadPageDomains()
	got, err := h.store.StatusPage(p.ID)
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

func (h *Hub) deleteStatusPage(w http.ResponseWriter, r *http.Request, _ *store.User) {
	id, ok := pathID(r)
	if !ok {
		writeErr(w, 400, "invalid id")
		return
	}
	if err := h.store.DeleteStatusPage(id); err != nil {
		h.internalErr(w, err)
		return
	}
	h.reloadPageDomains()
	writeJSON(w, 200, map[string]bool{"ok": true})
}

// ---------- public API ----------

// Overall page states.
const (
	PageOperational   = "operational"
	PageDegraded      = "degraded"
	PagePartialOutage = "partial_outage"
	PageMajorOutage   = "major_outage"
	PageUnknown       = "unknown"
)

// publicPage is what anonymous visitors see: no targets, no error messages,
// no locations.
type publicPage struct {
	Slug          string           `json:"slug"`
	Title         string           `json:"title"`
	Description   string           `json:"description"`
	Published     bool             `json:"published"`
	Status        string           `json:"status"`
	Days          int              `json:"days"`
	Monitors      []publicMonitor  `json:"monitors"`
	ShowIncidents bool             `json:"show_incidents"`
	Incidents     []publicIncident `json:"incidents"`
	// Reports are the incidents written on the page, with their updates.
	Reports []*store.PageIncident `json:"reports"`
	// Subscribe is true when visitors can subscribe by email.
	Subscribe bool  `json:"subscribe"`
	UpdatedAt int64 `json:"updated_at"`

	id       int64
	passHash string // "" = no password
}

type publicMonitor struct {
	ID        int64   `json:"id"`
	Name      string  `json:"name"`
	Group     string  `json:"group"` // section title, "" = no section
	Status    string  `json:"status"`
	Uptime    float64 `json:"uptime"`
	Uptime24h float64 `json:"uptime_24h"`
	Bars      []bar   `json:"bars"`
}

type publicIncident struct {
	ID          int64  `json:"id"`
	MonitorName string `json:"monitor_name"`
	StartedAt   int64  `json:"started_at"`
	ResolvedAt  int64  `json:"resolved_at"`
}

func (h *Hub) publicStatus(w http.ResponseWriter, r *http.Request) {
	h.servePublicPage(w, r, strings.ToLower(r.PathValue("slug")), false)
}

func (h *Hub) servePublicPage(w http.ResponseWriter, r *http.Request, slug string, byDomain bool) {
	page, err := h.cachedPublicPage(slug)
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, 404, "status page not found")
		return
	} else if err != nil {
		h.internalErr(w, err)
		return
	}
	// Drafts are visible to logged-in users only (preview from the admin UI).
	if !page.Published && (byDomain || h.currentUser(r) == nil) {
		writeErr(w, 404, "status page not found")
		return
	}
	if !h.pageOpen(r, page, byDomain) {
		passwordRequired(w, page)
		return
	}
	// Published pages are public: any site may read them, for example to
	// show a status dot. "*" never carries cookies, so drafts and unlocked
	// password pages stay out of reach of other sites.
	if page.Published && page.passHash == "" {
		w.Header().Set("Access-Control-Allow-Origin", "*")
	}
	writeJSON(w, 200, page)
}

// cachedPublicPage returns the payload of a status page, drafts included,
// built at most every publicCacheTTL.
func (h *Hub) cachedPublicPage(slug string) (*publicPage, error) {
	if !slugRe.MatchString(slug) {
		return nil, store.ErrNotFound
	}
	h.pages.mu.RLock()
	c, ok := h.pages.cache[slug]
	h.pages.mu.RUnlock()
	if ok && time.Since(c.at) <= publicCacheTTL {
		return c.page, nil
	}
	sp, err := h.store.StatusPageBySlug(slug)
	if err != nil {
		return nil, err
	}
	page, err := h.buildPublicPage(sp)
	if err != nil {
		return nil, err
	}
	h.pages.mu.Lock()
	if h.pages.cache == nil {
		h.pages.cache = map[string]cachedPage{}
	}
	h.pages.cache[slug] = cachedPage{at: time.Now(), page: page}
	h.pages.mu.Unlock()
	return page, nil
}

// publicDays is how many daily bars a page shows: the retention period,
// between one week and 90 days.
func (h *Hub) publicDays() int {
	return min(max(h.generalSettings().RetentionDays, 7), 90)
}

func (h *Hub) buildPublicPage(sp *store.StatusPage) (*publicPage, error) {
	now := time.Now().UTC()
	day := int64(24 * 3600 * 1000)
	days := h.publicDays()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).UnixMilli()
	from := today - int64(days-1)*day
	p := &publicPage{
		Slug: sp.Slug, Title: sp.Title, Description: sp.Description, Published: sp.Published,
		Days: days, ShowIncidents: sp.ShowIncidents, Monitors: []publicMonitor{}, Incidents: []publicIncident{},
		Subscribe: h.subscribeChannel(sp) != nil, UpdatedAt: now.UnixMilli(),
		id: sp.ID, passHash: sp.PasswordHash,
	}
	reports, err := h.store.PublicPageIncidents(sp.ID, from, publicReportLimit)
	if err != nil {
		return nil, err
	}
	for _, r := range reports {
		r.PageID = 0 // internal
	}
	p.Reports = reports
	ms, err := h.pageMonitors(sp)
	if err != nil {
		return nil, err
	}
	ids := make([]int64, 0, len(ms))
	for _, e := range ms {
		m := e.m
		ids = append(ids, m.ID)
		pm := publicMonitor{ID: m.ID, Name: m.Name, Group: e.group, Status: m.Status}
		if pm.Uptime, _, err = h.store.Uptime(m.ID, from); err != nil {
			return nil, err
		}
		if pm.Uptime24h, _, err = h.store.Uptime(m.ID, now.UnixMilli()-day); err != nil {
			return nil, err
		}
		buckets, err := h.store.Buckets(m.ID, from, today+day, day, false)
		if err != nil {
			return nil, err
		}
		byStart := map[int64]store.Bucket{}
		for _, b := range buckets {
			byStart[b.Start] = b
		}
		pm.Bars = make([]bar, 0, days)
		for i := range days {
			t := from + int64(i)*day
			b := byStart[t]
			pm.Bars = append(pm.Bars, bar{T: t, Total: b.Total, OK: b.OK, Degraded: b.Degraded, AvgMs: b.AvgMs})
		}
		p.Monitors = append(p.Monitors, pm)
	}
	p.Status = worsePageStatus(overallStatus(p.Monitors), reportsStatus(p.Reports))
	if sp.ShowIncidents {
		incs, err := h.store.IncidentsSince(ids, from, 50)
		if err != nil {
			return nil, err
		}
		for _, i := range incs {
			p.Incidents = append(p.Incidents, publicIncident{ID: i.ID, MonitorName: i.MonitorName, StartedAt: i.StartedAt, ResolvedAt: i.ResolvedAt})
		}
	}
	return p, nil
}

type pageMonitor struct {
	m     *store.Monitor
	group string
}

// pageMonitors lists what a status page shows, in sections: monitors picked
// one by one keep their order and sit under their group's title (if any);
// the groups picked as a whole follow, each with all its monitors by name.
// A section is never split: a later monitor of an earlier section joins it.
func (h *Hub) pageMonitors(sp *store.StatusPage) ([]pageMonitor, error) {
	groups, err := h.store.ListGroups()
	if err != nil {
		return nil, err
	}
	names := map[int64]string{}
	for _, g := range groups {
		names[g.ID] = g.Name
	}
	var order []string
	sections := map[string][]pageMonitor{}
	seen := map[int64]bool{}
	add := func(m *store.Monitor) {
		if seen[m.ID] {
			return
		}
		seen[m.ID] = true
		g := names[m.GroupID]
		if _, ok := sections[g]; !ok {
			order = append(order, g)
		}
		sections[g] = append(sections[g], pageMonitor{m: m, group: g})
	}
	for _, id := range sp.MonitorIDs {
		m, err := h.store.Monitor(id)
		if errors.Is(err, store.ErrNotFound) {
			continue
		} else if err != nil {
			return nil, err
		}
		add(m)
	}
	for _, gid := range sp.GroupIDs {
		ms, err := h.store.MonitorsInGroups([]int64{gid})
		if err != nil {
			return nil, err
		}
		for _, m := range ms {
			add(m)
		}
	}
	out := []pageMonitor{}
	for _, g := range order {
		out = append(out, sections[g]...)
	}
	return out, nil
}

// pageStatusRank orders page states from the best.
var pageStatusRank = map[string]int{PageUnknown: 0, PageOperational: 1, PageDegraded: 2, PagePartialOutage: 3, PageMajorOutage: 4}

func worsePageStatus(a, b string) string {
	if pageStatusRank[b] > pageStatusRank[a] {
		return b
	}
	return a
}

// reportsStatus is the page state the open incidents written on the page
// call for: a page never looks better than what its admins announced.
func reportsStatus(list []*store.PageIncident) string {
	out := PageUnknown
	for _, r := range list {
		if r.ResolvedAt != 0 {
			continue
		}
		switch r.Impact {
		case store.ImpactMinor:
			out = worsePageStatus(out, PageDegraded)
		case store.ImpactMajor:
			out = worsePageStatus(out, PagePartialOutage)
		case store.ImpactCritical:
			out = worsePageStatus(out, PageMajorOutage)
		}
	}
	return out
}

func overallStatus(ms []publicMonitor) string {
	active, down, degraded, known := 0, 0, 0, 0
	for _, m := range ms {
		if m.Status == StatusPaused {
			continue
		}
		active++
		switch m.Status {
		case StatusDown:
			down++
		case StatusDegraded:
			degraded++
		}
		if m.Status != StatusPending {
			known++
		}
	}
	switch {
	case known == 0:
		return PageUnknown
	case down > 0 && down == active:
		return PageMajorOutage
	case down > 0:
		return PagePartialOutage
	case degraded > 0:
		return PageDegraded
	default:
		return PageOperational
	}
}

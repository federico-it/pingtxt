package hub

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/federico-it/pingtxt/internal/check"
	"github.com/federico-it/pingtxt/internal/store"
)

// statuspageInfo describes an Atlassian Statuspage page for the monitor
// form: whether a URL is one (to suggest the statuspage type) and its
// components (to pick the ones to follow).
type statuspageInfo struct {
	Statuspage  bool                  `json:"statuspage"`
	URL         string                `json:"url,omitempty"`
	Name        string                `json:"name,omitempty"`
	Indicator   string                `json:"indicator,omitempty"`
	Description string                `json:"description,omitempty"`
	Components  []statuspageComponent `json:"components,omitempty"`
	Error       string                `json:"error,omitempty"`
}

type statuspageComponent struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Group  string `json:"group,omitempty"` // name of the group it belongs to
	Status string `json:"status"`
}

// statuspageCacheTTL keeps a lookup for a while: the form asks again as
// the user edits the monitor.
const statuspageCacheTTL = 5 * time.Minute

type statuspageCache struct {
	mu   sync.Mutex
	list map[string]statuspageCached
}

type statuspageCached struct {
	info statuspageInfo
	at   time.Time
}

func (c *statuspageCache) get(key string, now time.Time) (statuspageInfo, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.list[key]
	if !ok || now.Sub(e.at) > statuspageCacheTTL {
		return statuspageInfo{}, false
	}
	return e.info, true
}

func (c *statuspageCache) put(key string, info statuspageInfo, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.list == nil || len(c.list) > 200 {
		c.list = map[string]statuspageCached{}
	}
	c.list[key] = statuspageCached{info, now}
}

// spCache is shared by the hub: lookups are public data.
var spCache statuspageCache

// detectStatuspage tells whether a URL is an Atlassian Statuspage page and
// lists its components. It answers 200 with statuspage false when it is
// not one, so the form can ask about any URL.
func (h *Hub) detectStatuspage(w http.ResponseWriter, r *http.Request, _ *store.User) {
	var req struct {
		Target    string `json:"target"`
		IgnoreTLS bool   `json:"ignore_tls"`
	}
	if readJSON(r, &req) != nil {
		writeErr(w, 400, "invalid body")
		return
	}
	base, err := check.StatuspageBase(req.Target)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	key := base
	if req.IgnoreTLS {
		key += "#insecure"
	}
	now := time.Now()
	if info, ok := spCache.get(key, now); ok {
		writeJSON(w, 200, info)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	s, _, err := check.FetchStatuspage(ctx, check.Job{Target: base, IgnoreTLS: req.IgnoreTLS}, 8*time.Second)
	info := statuspageInfo{URL: base}
	if err != nil {
		info.Error = err.Error()
	} else {
		info = statuspageInfoOf(base, s)
	}
	if ctx.Err() == nil || err == nil {
		spCache.put(key, info, now)
	}
	writeJSON(w, 200, info)
}

// statuspageInfoOf lists the components of a page in page order, groups
// left out: their members are listed with the group's name.
func statuspageInfoOf(base string, s *check.StatuspageSummary) statuspageInfo {
	info := statuspageInfo{Statuspage: true, URL: base, Name: s.Page.Name, Indicator: s.Status.Indicator, Description: s.Status.Description,
		Components: []statuspageComponent{}}
	groups := map[string]string{}
	for _, c := range s.Components {
		if c.Group {
			groups[c.ID] = c.Name
		}
	}
	for _, c := range s.Components {
		if c.Group {
			continue
		}
		info.Components = append(info.Components, statuspageComponent{ID: c.ID, Name: c.Name, Group: groups[c.GroupID], Status: c.Status})
	}
	return info
}

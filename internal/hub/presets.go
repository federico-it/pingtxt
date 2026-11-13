package hub

import (
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/federico-it/pingtxt/internal/check"
	"github.com/federico-it/pingtxt/internal/store"
)

// Service presets: a catalog of popular services whose public status page
// pingtxt can follow with one click. Every entry is an Atlassian Statuspage
// page (or one that serves the same /api/v2/summary.json), so a preset is a
// statuspage monitor on the hub's own location. The catalog ships with the
// hub: no request leaves the hub until a preset is added.

// ServicePreset is one service of the catalog.
type ServicePreset struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Category string `json:"category"`
	// URL is the status page, used as the monitor target.
	URL string `json:"url"`
	// Color is the brand color (#rrggbb) for the tile of the picker.
	Color string `json:"color"`
	// Keywords are other names the search finds it by (products, old names).
	Keywords []string `json:"keywords"`
	// MonitorID is a monitor that already follows this page, 0 = none.
	MonitorID int64 `json:"monitor_id"`
}

// PresetCategory groups the catalog in the picker.
type PresetCategory struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

var presetCategories = []PresetCategory{
	{"ai", "AI"},
	{"dev", "Developer tools"},
	{"cloud", "Cloud & hosting"},
	{"observability", "Observability"},
	{"comms", "Email & messaging"},
	{"productivity", "Productivity"},
	{"commerce", "Commerce & web"},
	{"social", "Social & gaming"},
}

func preset(id, name, cat, url, color string, keywords ...string) ServicePreset {
	if keywords == nil {
		keywords = []string{}
	}
	return ServicePreset{ID: id, Name: name, Category: cat, URL: url, Color: color, Keywords: keywords}
}

// servicePresets is the catalog, by category. Each URL answered a
// Statuspage summary.json when it was added (TestPresetsLive checks again).
var servicePresets = []ServicePreset{
	preset("claude", "Claude", "ai", "https://status.claude.com", "#d97757", "anthropic", "claude code", "claude.ai"),
	preset("openai", "OpenAI", "ai", "https://status.openai.com", "#10a37f", "chatgpt", "gpt", "codex", "sora"),
	preset("mistral", "Mistral AI", "ai", "https://status.mistral.ai", "#fa520f", "le chat"),
	preset("groq", "Groq", "ai", "https://groqstatus.com", "#f55036", "groqcloud"),
	preset("perplexity", "Perplexity", "ai", "https://status.perplexity.com", "#20808d"),
	preset("cohere", "Cohere", "ai", "https://status.cohere.com", "#39594d"),
	preset("elevenlabs", "ElevenLabs", "ai", "https://status.elevenlabs.io", "#000000", "tts", "voice"),

	preset("github", "GitHub", "dev", "https://www.githubstatus.com", "#181717", "copilot", "actions", "git"),
	preset("bitbucket", "Bitbucket", "dev", "https://bitbucket.status.atlassian.com", "#0052cc", "atlassian", "git"),
	preset("npm", "npm", "dev", "https://status.npmjs.org", "#cb3837", "registry", "node"),
	preset("docker", "Docker Hub", "dev", "https://www.dockerstatus.com", "#2496ed", "registry", "containers"),
	preset("circleci", "CircleCI", "dev", "https://status.circleci.com", "#343434", "ci"),
	preset("codecov", "Codecov", "dev", "https://status.codecov.com", "#f01f7a", "coverage"),
	preset("hashicorp", "HashiCorp", "dev", "https://status.hashicorp.com", "#000000", "terraform", "vault", "hcp"),
	preset("postman", "Postman", "dev", "https://status.postman.com", "#ff6c37", "api"),
	preset("expo", "Expo", "dev", "https://status.expo.dev", "#000020", "eas", "react native"),
	preset("snyk", "Snyk", "dev", "https://status.snyk.io", "#4c4a73", "security"),
	preset("browserstack", "BrowserStack", "dev", "https://status.browserstack.com", "#e66f32", "testing"),
	preset("clerk", "Clerk", "dev", "https://status.clerk.com", "#6c47ff", "auth", "login"),
	preset("readme", "ReadMe", "dev", "https://www.readmestatus.com", "#018ef5", "docs"),

	preset("cloudflare", "Cloudflare", "cloud", "https://www.cloudflarestatus.com", "#f38020", "cdn", "dns", "workers"),
	preset("vercel", "Vercel", "cloud", "https://www.vercel-status.com", "#000000", "next.js", "hosting"),
	preset("netlify", "Netlify", "cloud", "https://www.netlifystatus.com", "#00c7b7", "hosting"),
	preset("digitalocean", "DigitalOcean", "cloud", "https://status.digitalocean.com", "#0080ff", "droplets", "vps"),
	preset("linode", "Linode (Akamai)", "cloud", "https://status.linode.com", "#00a95c", "akamai", "vps"),
	preset("render", "Render", "cloud", "https://status.render.com", "#46e3b7", "hosting"),
	preset("flyio", "Fly.io", "cloud", "https://status.flyio.net", "#7b3be2", "fly", "hosting"),
	preset("scaleway", "Scaleway", "cloud", "https://status.scaleway.com", "#4f0599", "vps"),
	preset("tailscale", "Tailscale", "cloud", "https://status.tailscale.com", "#242424", "vpn", "wireguard"),
	preset("supabase", "Supabase", "cloud", "https://status.supabase.com", "#3ecf8e", "postgres", "database"),
	preset("mongodb", "MongoDB Atlas", "cloud", "https://status.mongodb.com", "#47a248", "atlas", "database"),
	preset("planetscale", "PlanetScale", "cloud", "https://www.planetscalestatus.com", "#000000", "mysql", "database"),
	preset("elastic", "Elastic Cloud", "cloud", "https://status.elastic.co", "#005571", "elasticsearch", "kibana"),
	preset("wasabi", "Wasabi", "cloud", "https://status.wasabi.com", "#01cd3e", "s3", "storage"),

	preset("sentry", "Sentry", "observability", "https://status.sentry.io", "#362d59", "errors"),
	preset("newrelic", "New Relic", "observability", "https://status.newrelic.com", "#1ce783", "apm"),
	preset("grafana", "Grafana Cloud", "observability", "https://status.grafana.com", "#f46800", "prometheus", "loki"),
	preset("segment", "Segment", "observability", "https://status.segment.com", "#52bd94", "analytics", "twilio"),

	preset("twilio", "Twilio", "comms", "https://status.twilio.com", "#f22f46", "sendgrid", "sms", "email"),
	preset("mailgun", "Mailgun", "comms", "https://status.mailgun.com", "#f06b66", "email", "smtp"),
	preset("mailjet", "Mailjet", "comms", "https://status.mailjet.com", "#9585f4", "email", "smtp"),
	preset("brevo", "Brevo", "comms", "https://status.brevo.com", "#0b996e", "sendinblue", "email", "smtp"),
	preset("zoom", "Zoom", "comms", "https://www.zoomstatus.com", "#0b5cff", "video", "meetings"),

	preset("jira", "Jira", "productivity", "https://jira-software.status.atlassian.com", "#0052cc", "atlassian"),
	preset("confluence", "Confluence", "productivity", "https://confluence.status.atlassian.com", "#172b4d", "atlassian", "wiki"),
	preset("trello", "Trello", "productivity", "https://trello.status.atlassian.com", "#0079bf", "atlassian"),
	preset("notion", "Notion", "productivity", "https://www.notion-status.com", "#000000", "wiki", "notes"),
	preset("linear", "Linear", "productivity", "https://linearstatus.com", "#5e6ad2", "issues"),
	preset("asana", "Asana", "productivity", "https://status.asana.com", "#f06a6a", "tasks"),
	preset("monday", "monday.com", "productivity", "https://status.monday.com", "#ff3d57", "tasks"),
	preset("clickup", "ClickUp", "productivity", "https://status.clickup.com", "#7b68ee", "tasks"),
	preset("airtable", "Airtable", "productivity", "https://status.airtable.com", "#18bfff", "spreadsheet"),
	preset("miro", "Miro", "productivity", "https://status.miro.com", "#ffd02f", "whiteboard"),
	preset("figma", "Figma", "productivity", "https://status.figma.com", "#f24e1e", "design"),
	preset("dropbox", "Dropbox", "productivity", "https://status.dropbox.com", "#0061ff", "storage", "files"),
	preset("box", "Box", "productivity", "https://status.box.com", "#0061d5", "storage", "files"),
	preset("1password", "1Password", "productivity", "https://status.1password.com", "#0094f5", "passwords"),
	preset("zapier", "Zapier", "productivity", "https://status.zapier.com", "#ff4f00", "automation"),
	preset("make", "Make", "productivity", "https://status.make.com", "#6d00cc", "integromat", "automation"),
	preset("hubspot", "HubSpot", "productivity", "https://status.hubspot.com", "#ff7a59", "crm"),

	preset("shopify", "Shopify", "commerce", "https://www.shopifystatus.com", "#7ab55c", "shop", "ecommerce"),
	preset("squarespace", "Squarespace", "commerce", "https://status.squarespace.com", "#000000", "website"),
	preset("webflow", "Webflow", "commerce", "https://status.webflow.com", "#146ef5", "website"),
	preset("godaddy", "GoDaddy", "commerce", "https://status.godaddy.com", "#1bdbdb", "domains", "dns"),
	preset("coinbase", "Coinbase", "commerce", "https://status.coinbase.com", "#0052ff", "crypto"),

	preset("discord", "Discord", "social", "https://discordstatus.com", "#5865f2", "chat"),
	preset("reddit", "Reddit", "social", "https://www.redditstatus.com", "#ff4500"),
	preset("twitch", "Twitch", "social", "https://status.twitch.com", "#9146ff", "streaming"),
	preset("epicgames", "Epic Games", "social", "https://status.epicgames.com", "#313131", "fortnite"),
}

// presetDefaultInterval: status pages change slowly and are served from a
// CDN cache, so a check every two minutes is enough.
const presetDefaultInterval = 120

func presetByID(id string) (ServicePreset, bool) {
	i := slices.IndexFunc(servicePresets, func(p ServicePreset) bool { return p.ID == id })
	if i < 0 {
		return ServicePreset{}, false
	}
	return servicePresets[i], true
}

// presetMonitors maps the address of each followed status page to the
// first statuspage monitor that follows it, whatever its components.
func (h *Hub) presetMonitors() (map[string]int64, error) {
	list, err := h.store.ListMonitors()
	if err != nil {
		return nil, err
	}
	out := map[string]int64{}
	for _, m := range list {
		if m.Type != check.TypeStatuspage {
			continue
		}
		base, err := check.StatuspageBase(m.Target)
		if err != nil {
			continue
		}
		if _, ok := out[base]; !ok || m.ID < out[base] {
			out[base] = m.ID
		}
	}
	return out, nil
}

type presetCatalog struct {
	Categories []PresetCategory `json:"categories"`
	Services   []ServicePreset  `json:"services"`
}

// listPresets serves the catalog, with the monitor that already follows
// each service.
func (h *Hub) listPresets(w http.ResponseWriter, r *http.Request, _ *store.User) {
	have, err := h.presetMonitors()
	if err != nil {
		h.internalErr(w, err)
		return
	}
	out := presetCatalog{Categories: presetCategories, Services: make([]ServicePreset, 0, len(servicePresets))}
	cat := r.URL.Query().Get("category")
	q := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	for _, p := range servicePresets {
		if cat != "" && p.Category != cat {
			continue
		}
		if q != "" && !presetMatches(p, q) {
			continue
		}
		p.MonitorID = have[p.URL]
		out.Services = append(out.Services, p)
	}
	writeJSON(w, 200, out)
}

// presetMatches tells whether a search (lower case) finds a preset by its
// id, name, keywords or status page address.
func presetMatches(p ServicePreset, q string) bool {
	if strings.Contains(strings.ToLower(p.Name), q) || strings.Contains(p.ID, q) || strings.Contains(p.URL, q) {
		return true
	}
	return slices.ContainsFunc(p.Keywords, func(k string) bool { return strings.Contains(k, q) })
}

type presetRequest struct {
	IDs []string `json:"ids"`
	// GroupID puts the new monitors in an existing group (0 = none).
	GroupID int64 `json:"group_id"`
	// GroupName puts them in the group with this name, created when missing.
	GroupName   string   `json:"group_name"`
	IntervalSec int      `json:"interval_sec"`
	Locations   []string `json:"locations"`
	TagIDs      []int64  `json:"tag_ids"`
}

type presetSkip struct {
	ID        string `json:"id"`
	MonitorID int64  `json:"monitor_id"`
	Reason    string `json:"reason"` // exists
}

type presetResult struct {
	Created []*store.Monitor `json:"created"`
	Skipped []presetSkip     `json:"skipped"`
	Group   *store.Group     `json:"group,omitempty"`
}

// addPresets creates one statuspage monitor per chosen service. Services
// already followed by a monitor are skipped, so the call can be repeated.
func (h *Hub) addPresets(w http.ResponseWriter, r *http.Request, _ *store.User) {
	var in presetRequest
	if readJSON(r, &in) != nil {
		writeErr(w, 400, "invalid body")
		return
	}
	var ids []string
	for _, id := range in.IDs {
		id = strings.ToLower(strings.TrimSpace(id))
		if _, ok := presetByID(id); !ok {
			writeErr(w, 400, fmt.Sprintf("unknown service %q", id))
			return
		}
		if !slices.Contains(ids, id) {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		writeErr(w, 400, "no services selected")
		return
	}
	if in.GroupID > 0 && strings.TrimSpace(in.GroupName) != "" {
		writeErr(w, 400, "give group_id or group_name, not both")
		return
	}
	if in.IntervalSec == 0 {
		in.IntervalSec = presetDefaultInterval
	}
	h.importMu.Lock()
	defer h.importMu.Unlock()
	have, err := h.presetMonitors()
	if err != nil {
		h.internalErr(w, err)
		return
	}
	out := presetResult{Created: []*store.Monitor{}, Skipped: []presetSkip{}}
	var todo []ServicePreset
	for _, id := range ids {
		p, _ := presetByID(id)
		if mid := have[p.URL]; mid > 0 {
			out.Skipped = append(out.Skipped, presetSkip{ID: id, MonitorID: mid, Reason: "exists"})
			continue
		}
		todo = append(todo, p)
	}
	// Check every monitor before creating the group or any of them.
	ms := make([]*store.Monitor, 0, len(todo))
	for _, p := range todo {
		m := &store.Monitor{Name: p.Name, Type: check.TypeStatuspage, Target: p.URL, IntervalSec: in.IntervalSec,
			Retries: 2, Locations: in.Locations, GroupID: in.GroupID, TagIDs: in.TagIDs}
		if len(m.Locations) == 0 {
			m.Locations = []string{LocalLocation}
		}
		if err := h.validateMonitor(m); err != nil {
			writeErr(w, 400, err.Error())
			return
		}
		ms = append(ms, m)
	}
	rec := auditFrom(r)
	if len(ms) > 0 && strings.TrimSpace(in.GroupName) != "" {
		g, created, err := h.presetGroup(in.GroupName)
		if err != nil {
			writeErr(w, 400, err.Error())
			return
		}
		if created {
			rec.Item("group.create", "group", g.ID, g.Name, map[string]any{"name": g.Name})
		}
		out.Group = g
		for _, m := range ms {
			m.GroupID = g.ID
		}
	} else if in.GroupID > 0 {
		out.Group, _ = h.store.Group(in.GroupID)
	}
	for i, m := range ms {
		if err := h.store.CreateMonitor(m); err != nil {
			h.internalErr(w, err)
			return
		}
		h.MonitorChanged(m.ID)
		got, err := h.store.Monitor(m.ID)
		if err != nil {
			got = m
		}
		out.Created = append(out.Created, got)
		rec.Item("monitor.create", "monitor", m.ID, m.Name, map[string]any{"_preset": todo[i].ID, "target": m.Target})
	}
	if len(out.Created) == 0 {
		rec.Skip()
	}
	writeJSON(w, 201, out)
}

// presetGroup finds the group with this name (any case) or creates it at
// the top level.
func (h *Hub) presetGroup(name string) (g *store.Group, created bool, err error) {
	g = &store.Group{Name: name}
	if err := validateGroup(g); err != nil {
		return nil, false, err
	}
	list, err := h.store.ListGroups()
	if err != nil {
		return nil, false, err
	}
	for _, e := range list {
		if strings.EqualFold(e.Name, g.Name) {
			return e, false, nil
		}
	}
	if err := h.store.CreateGroup(g); err != nil {
		if errors.Is(err, store.ErrConflict) {
			return nil, false, errors.New("a group with this name already exists")
		}
		return nil, false, err
	}
	h.pages.mu.Lock()
	h.pages.cache = nil
	h.pages.badges = nil
	h.pages.mu.Unlock()
	g, err = h.store.Group(g.ID)
	return g, err == nil, err
}

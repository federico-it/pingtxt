package hub

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/federico-it/pingtxt/internal/check"
	"github.com/federico-it/pingtxt/internal/store"
)

// A configuration export is a JSON document with the monitors, groups, tags,
// notification channels, maintenance windows, status pages, agents and
// instance settings. It holds no history, users, sessions or API tokens:
// that is what database backups are for.
//
// Objects keep their ids so that references (a monitor's group, a status
// page's monitors, "agent:<id>" locations) can be followed. On import the ids
// are only used to resolve those references: objects get new ids, or are
// matched to existing ones by name (merge mode).
//
// Without secrets, passwords, tokens and secret webhook URLs are replaced by
// redactedValue. On import a redacted value keeps the value of the matched
// existing object; when there is none the object is skipped or paused and a
// warning says what to fill in.

const (
	configFormat  = "pingtxt-config"
	configVersion = 1
	redactedValue = "[redacted]"
	// redactedPassword replaces passwords inside target URLs, where brackets
	// would make the URL invalid.
	redactedPassword = "__redacted__"
)

// ConfigDoc is the configuration export document.
type ConfigDoc struct {
	Format       string           `json:"format"`
	Version      int              `json:"version"`
	HubVersion   string           `json:"hub_version"`
	ExportedAt   string           `json:"exported_at"`
	Secrets      bool             `json:"secrets"`
	Settings     *configSettings  `json:"settings,omitempty"`
	Agents       []cfgAgent       `json:"agents"`
	Groups       []cfgGroup       `json:"groups"`
	Tags         []cfgTag         `json:"tags"`
	Notifiers    []cfgNotifier    `json:"notifiers"`
	Monitors     []cfgMonitor     `json:"monitors"`
	Maintenances []cfgMaintenance `json:"maintenances"`
	StatusPages  []cfgStatusPage  `json:"status_pages"`
}

type configSettings struct {
	General      *GeneralSettings     `json:"general,omitempty"`
	Auth         *AuthSettings        `json:"auth,omitempty"`
	AgentUpdates *AgentUpdateSettings `json:"agent_updates,omitempty"`
	Backups      *BackupSettings      `json:"backups,omitempty"`
	Reports      *ReportSettings      `json:"reports,omitempty"`
}

type cfgAgent struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Location string `json:"location"`
	// TokenHash lets the agents keep their tokens when the configuration
	// moves to another hub. Only in exports with secrets.
	TokenHash string `json:"token_hash,omitempty"`
}

// The cfg* types reuse the API objects and drop state that is not
// configuration: the outer empty fields shadow the inner ones in JSON.
type cfgGroup struct {
	store.Group
	Monitors  int   `json:"monitors,omitempty"`
	CreatedAt int64 `json:"created_at,omitempty"`
}

type cfgTag struct {
	store.Tag
	Monitors  int   `json:"monitors,omitempty"`
	CreatedAt int64 `json:"created_at,omitempty"`
}

type cfgNotifier struct {
	store.Notifier
	CreatedAt int64 `json:"created_at,omitempty"`
}

type cfgMonitor struct {
	store.Monitor
	Status      string `json:"status,omitempty"`
	StatusSince int64  `json:"status_since,omitempty"`
	CreatedAt   int64  `json:"created_at,omitempty"`
	PushToken   string `json:"push_token,omitempty"`
}

type cfgMaintenance struct {
	store.Maintenance
	CreatedAt int64 `json:"created_at,omitempty"`
}

type cfgStatusPage struct {
	store.StatusPage
	CreatedAt int64 `json:"created_at,omitempty"`
	// PasswordHash is the visitors' password, only in exports with secrets.
	PasswordHash string `json:"password_hash,omitempty"`
}

// notifierSecrets are the config keys redacted from exports without secrets.
// Discord and Slack webhook URLs carry their token in the path.
var notifierSecrets = map[string][]string{
	"email":    {"password"},
	"telegram": {"bot_token"},
	"ntfy":     {"token"},
	"discord":  {"url"},
	"slack":    {"url"},
}

var (
	// Request headers whose value is a credential.
	secretHeaderRe = regexp.MustCompile(`(?i)auth|token|key|secret|cookie|session|passw`)
)

// exportTarget hides the password in a target for an export without secrets.
// It matches the same URLs and DSNs as redactTarget (read-only users), with a
// marker that import recognizes.
func exportTarget(t string) string {
	re := credentialsInDSN
	if strings.Contains(t, "://") {
		re = credentialsInURL
	}
	return re.ReplaceAllString(t, "${1}"+redactedPassword+"@")
}

// ExportConfig builds the configuration document.
func (h *Hub) ExportConfig(secrets bool) (*ConfigDoc, error) {
	doc := &ConfigDoc{
		Format: configFormat, Version: configVersion, HubVersion: h.cfg.Version,
		ExportedAt: time.Now().UTC().Format(time.RFC3339), Secrets: secrets,
		Agents: []cfgAgent{}, Groups: []cfgGroup{}, Tags: []cfgTag{}, Notifiers: []cfgNotifier{},
		Monitors: []cfgMonitor{}, Maintenances: []cfgMaintenance{}, StatusPages: []cfgStatusPage{},
	}
	g := h.generalSettings()
	a := h.authSettings()
	au := h.agentUpdateSettings()
	bk := h.backupSettings()
	rp := h.reportSettings()
	if !secrets {
		for id, p := range a.Providers {
			if p.ClientSecret != "" {
				p.ClientSecret = redactedValue
			}
			a.Providers[id] = p
		}
	}
	doc.Settings = &configSettings{General: &g, Auth: &a, AgentUpdates: &au, Backups: &bk, Reports: &rp}

	agents, err := h.store.ListAgents()
	if err != nil {
		return nil, err
	}
	for _, ag := range agents {
		c := cfgAgent{ID: ag.ID, Name: ag.Name, Location: ag.Location}
		if secrets {
			c.TokenHash = ag.TokenHash
		}
		doc.Agents = append(doc.Agents, c)
	}
	groups, err := h.store.ListGroups()
	if err != nil {
		return nil, err
	}
	for _, x := range groups {
		doc.Groups = append(doc.Groups, cfgGroup{Group: *x})
	}
	tags, err := h.store.ListTags()
	if err != nil {
		return nil, err
	}
	for _, x := range tags {
		doc.Tags = append(doc.Tags, cfgTag{Tag: *x})
	}
	notifiers, err := h.store.ListNotifiers()
	if err != nil {
		return nil, err
	}
	for _, n := range notifiers {
		if !secrets {
			for _, k := range notifierSecrets[n.Type] {
				if n.Config[k] != "" {
					n.Config[k] = redactedValue
				}
			}
		}
		doc.Notifiers = append(doc.Notifiers, cfgNotifier{Notifier: *n})
	}
	monitors, err := h.store.ListMonitors()
	if err != nil {
		return nil, err
	}
	for _, m := range monitors {
		c := cfgMonitor{Monitor: *m}
		if secrets {
			c.PushToken = m.PushToken
		} else {
			c.Target = exportTarget(m.Target)
			hdr := make(map[string]string, len(m.Headers))
			for k, v := range m.Headers {
				if secretHeaderRe.MatchString(k) && v != "" {
					v = redactedValue
				}
				hdr[k] = v
			}
			c.Headers = hdr
		}
		doc.Monitors = append(doc.Monitors, c)
	}
	maints, err := h.store.ListMaintenances()
	if err != nil {
		return nil, err
	}
	for _, m := range maints {
		doc.Maintenances = append(doc.Maintenances, cfgMaintenance{Maintenance: *m})
	}
	pages, err := h.store.ListStatusPages()
	if err != nil {
		return nil, err
	}
	for _, p := range pages {
		c := cfgStatusPage{StatusPage: *p}
		if secrets {
			c.PasswordHash = p.PasswordHash
		}
		doc.StatusPages = append(doc.StatusPages, c)
	}
	return doc, nil
}

// ImportOptions control ImportConfig.
type ImportOptions struct {
	// Replace deletes the current monitors (with their history), groups,
	// tags, channels, maintenance windows and status pages first. Otherwise
	// objects are matched by name and updated, and the rest is added.
	Replace bool
	// Settings applies the instance settings of the document too.
	Settings bool
	// Backup takes a database backup before changing anything.
	Backup bool
	// Host is the hub's own host name: status pages cannot use it as domain.
	Host string
}

// ImportCounts counts objects by kind.
type ImportCounts map[string]int

// ImportResult reports what an import changed.
type ImportResult struct {
	Mode     string       `json:"mode"`
	Backup   string       `json:"backup,omitempty"`
	Created  ImportCounts `json:"created"`
	Updated  ImportCounts `json:"updated"`
	Deleted  ImportCounts `json:"deleted"`
	Skipped  ImportCounts `json:"skipped"`
	Settings bool         `json:"settings"`
	Warnings []string     `json:"warnings"`
}

func (r *ImportResult) warn(format string, a ...any) {
	if len(r.Warnings) < 200 {
		r.Warnings = append(r.Warnings, fmt.Sprintf(format, a...))
	}
}

// ParseConfig decodes and checks a configuration document.
func ParseConfig(b []byte) (*ConfigDoc, error) {
	var doc ConfigDoc
	if err := json.Unmarshal(b, &doc); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}
	if doc.Format != configFormat {
		return nil, errors.New("not a pingtxt configuration export")
	}
	if doc.Version < 1 || doc.Version > configVersion {
		return nil, fmt.Errorf("unsupported configuration version %d", doc.Version)
	}
	return &doc, nil
}

// ImportConfig applies a configuration document.
func (h *Hub) ImportConfig(doc *ConfigDoc, opt ImportOptions) (*ImportResult, error) {
	h.importMu.Lock()
	defer h.importMu.Unlock()
	res := &ImportResult{Mode: "merge", Created: ImportCounts{}, Updated: ImportCounts{}, Deleted: ImportCounts{}, Skipped: ImportCounts{}, Warnings: []string{}}
	if opt.Replace {
		res.Mode = "replace"
	}
	if opt.Backup && !h.FileBackups() {
		res.Warnings = append(res.Warnings, "no automatic backup before the import with "+h.store.Driver()+": back up the database server first if you may want to undo it")
	} else if opt.Backup {
		b, err := h.CreateBackup(backupPreImport)
		if err != nil {
			return nil, fmt.Errorf("backup before import: %w", err)
		}
		res.Backup = b.Name
	}
	im := &importer{h: h, doc: doc, opt: opt, res: res,
		agents: map[int64]int64{}, groups: map[int64]int64{}, tags: map[int64]int64{}, monitors: map[int64]int64{}, notifiers: map[int64]int64{}}
	err := im.run()
	// Reload what the hub keeps in memory, also after a partial import.
	h.configReloaded()
	return res, err
}

type importer struct {
	h   *Hub
	doc *ConfigDoc
	opt ImportOptions
	res *ImportResult

	// old id (in the document) -> id in this hub
	agents, groups, tags, monitors, notifiers map[int64]int64
}

func (im *importer) run() error {
	st := im.h.store
	// Old objects are needed to restore redacted secrets, also in replace mode.
	oldNotifiers, err := st.ListNotifiers()
	if err != nil {
		return err
	}
	oldMonitors, err := st.ListMonitors()
	if err != nil {
		return err
	}
	if im.opt.Replace {
		counts := map[string]int{"monitors": len(oldMonitors), "notifiers": len(oldNotifiers)}
		if l, err := st.ListGroups(); err == nil {
			counts["groups"] = len(l)
		}
		if l, err := st.ListTags(); err == nil {
			counts["tags"] = len(l)
		}
		if l, err := st.ListMaintenances(); err == nil {
			counts["maintenances"] = len(l)
		}
		if l, err := st.ListStatusPages(); err == nil {
			counts["status_pages"] = len(l)
		}
		if err := st.ClearConfig(); err != nil {
			return err
		}
		for k, v := range counts {
			if v > 0 {
				im.res.Deleted[k] = v
			}
		}
	}
	if im.opt.Settings && im.doc.Settings != nil {
		if err := im.settings(); err != nil {
			return err
		}
	}
	steps := []func() error{
		im.importAgents,
		im.importGroups,
		im.importTags,
		func() error { return im.importNotifiers(oldNotifiers) },
		func() error { return im.importMonitors(oldMonitors) },
		im.importMaintenances,
		im.importStatusPages,
		im.importReports,
	}
	for _, step := range steps {
		if err := step(); err != nil {
			return err
		}
	}
	return nil
}

func (im *importer) settings() error {
	h, s := im.h, im.doc.Settings
	im.res.Settings = true
	if g := s.General; g != nil {
		g.normalize()
		if err := g.validate(); err != nil {
			im.res.warn("general settings not applied: %v", err)
		} else if err := h.store.SetSetting(keyGeneral, g); err != nil {
			return err
		}
	}
	if a := s.Auth; a != nil {
		cur := h.authSettings()
		if a.Providers == nil {
			a.Providers = map[string]ProviderSettings{}
		}
		for id, p := range a.Providers {
			if p.ClientSecret == redactedValue || p.ClientSecret == "" {
				p.ClientSecret = cur.Providers[id].ClientSecret
			}
			if p.Enabled && p.ClientSecret == "" {
				p.Enabled = false
				im.res.warn("login provider %s turned off: its client secret is not in the file", providerLabel(id, p))
			}
			a.Providers[id] = p
		}
		a.normalize()
		if err := a.validate(); err != nil {
			im.res.warn("login settings not applied: %v", err)
		} else if err := h.store.SetSetting(keyAuth, a); err != nil {
			return err
		}
	}
	if u := s.AgentUpdates; u != nil {
		if err := h.setAgentUpdateSettings(*u); err != nil {
			return err
		}
	}
	if b := s.Backups; b != nil {
		b.normalize()
		if err := h.store.SetSetting(keyBackups, b); err != nil {
			return err
		}
	}
	return nil
}

func (im *importer) importAgents() error {
	st := im.h.store
	cur, err := st.ListAgents()
	if err != nil {
		return err
	}
	for _, a := range im.doc.Agents {
		name := strings.TrimSpace(a.Name)
		if name == "" {
			im.res.Skipped["agents"]++
			continue
		}
		var match *store.Agent
		if a.TokenHash != "" {
			match, _ = st.AgentByToken(a.TokenHash)
		}
		if match == nil {
			for _, c := range cur {
				if strings.EqualFold(c.Name, name) {
					match = c
					break
				}
			}
		}
		if match != nil {
			im.agents[a.ID] = match.ID
			if match.Name != name || match.Location != a.Location {
				if err := st.UpdateAgent(match.ID, name, strings.TrimSpace(a.Location)); err != nil {
					return err
				}
				im.res.Updated["agents"]++
			}
			continue
		}
		hash := a.TokenHash
		if hash == "" {
			_, hash = NewToken("pt_")
			im.res.warn("agent %q created with a new token: open agents and rotate its token to connect it", name)
		}
		created, err := st.CreateAgent(name, strings.TrimSpace(a.Location), hash)
		if err != nil {
			return err
		}
		cur = append(cur, created)
		im.agents[a.ID] = created.ID
		im.res.Created["agents"]++
	}
	return nil
}

func (im *importer) importGroups() error {
	st := im.h.store
	cur, err := st.ListGroups()
	if err != nil {
		return err
	}
	for _, x := range im.doc.Groups {
		g := x.Group
		old := g.ID
		g.ID = 0
		if err := validateGroup(&g); err != nil {
			im.res.warn("group %q skipped: %v", x.Name, err)
			im.res.Skipped["groups"]++
			continue
		}
		for _, c := range cur {
			if strings.EqualFold(c.Name, g.Name) {
				g.ID = c.ID
			}
		}
		if g.ID != 0 {
			if err := st.UpdateGroup(&g); err != nil {
				return err
			}
			im.res.Updated["groups"]++
		} else {
			if err := st.CreateGroup(&g); err != nil {
				return err
			}
			cur = append(cur, &g)
			im.res.Created["groups"]++
		}
		im.groups[old] = g.ID
	}
	return nil
}

func (im *importer) importTags() error {
	st := im.h.store
	cur, err := st.ListTags()
	if err != nil {
		return err
	}
	for _, x := range im.doc.Tags {
		t := x.Tag
		old := t.ID
		t.ID = 0
		if err := validateTag(&t); err != nil {
			im.res.warn("tag %q skipped: %v", x.Name, err)
			im.res.Skipped["tags"]++
			continue
		}
		for _, c := range cur {
			if strings.EqualFold(c.Name, t.Name) {
				t.ID = c.ID
			}
		}
		if t.ID != 0 {
			if err := st.UpdateTag(&t); err != nil {
				return err
			}
			im.res.Updated["tags"]++
		} else {
			if err := st.CreateTag(&t); err != nil {
				return err
			}
			cur = append(cur, &t)
			im.res.Created["tags"]++
		}
		im.tags[old] = t.ID
	}
	return nil
}

func (im *importer) importNotifiers(old []*store.Notifier) error {
	st := im.h.store
	cur := []*store.Notifier{}
	if !im.opt.Replace {
		cur = old
	}
	find := func(list []*store.Notifier, n *store.Notifier) *store.Notifier {
		for _, c := range list {
			if c.Type == n.Type && strings.EqualFold(c.Name, strings.TrimSpace(n.Name)) {
				return c
			}
		}
		return nil
	}
	for _, x := range im.doc.Notifiers {
		n := x.Notifier
		docID := n.ID
		n.ID = 0
		if n.Config == nil {
			n.Config = map[string]string{}
		}
		missing := []string{}
		for k, v := range n.Config {
			if v != redactedValue {
				continue
			}
			if prev := find(old, &n); prev != nil && prev.Config[k] != "" {
				n.Config[k] = prev.Config[k]
			} else {
				missing = append(missing, k)
			}
		}
		if len(missing) > 0 {
			im.res.warn("notification channel %q skipped: %s not in the file (export with secrets, or add it by hand)", n.Name, strings.Join(missing, ", "))
			im.res.Skipped["notifiers"]++
			continue
		}
		if err := validateNotifier(&n); err != nil {
			im.res.warn("notification channel %q skipped: %v", n.Name, err)
			im.res.Skipped["notifiers"]++
			continue
		}
		if c := find(cur, &n); c != nil {
			n.ID = c.ID
			if err := st.UpdateNotifier(&n); err != nil {
				return err
			}
			im.res.Updated["notifiers"]++
			im.notifiers[docID] = n.ID
			continue
		}
		if err := st.CreateNotifier(&n); err != nil {
			return err
		}
		im.notifiers[docID] = n.ID
		cur = append(cur, &n)
		im.res.Created["notifiers"]++
	}
	return nil
}

// importReports applies the report settings of the document, with the ids
// of the channels and groups in this hub. They are settings: only imported
// with them.
func (im *importer) importReports() error {
	if !im.opt.Settings || im.doc.Settings == nil || im.doc.Settings.Reports == nil {
		return nil
	}
	r := *im.doc.Settings.Reports
	r.NotifierIDs = im.mapIDs(r.NotifierIDs, im.notifiers)
	r.GroupIDs = im.mapIDs(r.GroupIDs, im.groups)
	r.normalize()
	if (r.Weekly || r.Monthly) && len(r.NotifierIDs) == 0 {
		r.Weekly, r.Monthly = false, false
		im.res.warn("periodic reports turned off: none of their email channels was imported")
	}
	if err := im.h.validateReportSettings(&r); err != nil {
		im.res.warn("report settings not applied: %v", err)
		return nil
	}
	return im.h.saveReportSettings(r, time.Now())
}

func (im *importer) mapIDs(ids []int64, m map[int64]int64) []int64 {
	out := []int64{}
	for _, id := range ids {
		if n, ok := m[id]; ok {
			out = append(out, n)
		}
	}
	return out
}

func (im *importer) importMonitors(old []*store.Monitor) error {
	h, st := im.h, im.h.store
	cur := []*store.Monitor{}
	if !im.opt.Replace {
		cur = old
	}
	// match finds the only monitor with the same name and type.
	match := func(list []*store.Monitor, m *store.Monitor) *store.Monitor {
		var found *store.Monitor
		for _, c := range list {
			if c.Type == m.Type && strings.EqualFold(c.Name, strings.TrimSpace(m.Name)) {
				if found != nil {
					return nil
				}
				found = c
			}
		}
		return found
	}
	for _, x := range im.doc.Monitors {
		m := x.Monitor
		oldID := m.ID
		m.ID = 0
		m.GroupID = im.groups[m.GroupID]
		m.TagIDs = im.mapIDs(m.TagIDs, im.tags)
		locs := []string{}
		for _, l := range m.Locations {
			if id, ok := parseAgentLocation(l); ok {
				if n, ok := im.agents[id]; ok {
					locs = append(locs, AgentLocation(n))
				}
				continue
			}
			locs = append(locs, l)
		}
		m.Locations = locs
		prev := match(old, &m)
		redacted := false
		if strings.Contains(m.Target, redactedPassword) {
			if prev != nil && prev.Target != "" && exportTarget(prev.Target) == m.Target {
				m.Target = prev.Target
			} else {
				redacted = true
			}
		}
		for k, v := range m.Headers {
			if v != redactedValue {
				continue
			}
			if prev != nil && prev.Headers[k] != "" {
				m.Headers[k] = prev.Headers[k]
			} else {
				redacted = true
			}
		}
		paused := m.Paused
		if redacted && !paused {
			paused = true
			im.res.warn("monitor %q paused: its password or credential header is not in the file. edit it and resume", m.Name)
		}
		if err := h.validateMonitor(&m); err != nil {
			im.res.warn("monitor %q skipped: %v", x.Name, err)
			im.res.Skipped["monitors"]++
			continue
		}
		if c := match(cur, &m); c != nil {
			m.ID = c.ID
			if err := st.UpdateMonitor(&m); err != nil {
				return err
			}
			im.res.Updated["monitors"]++
		} else {
			m.Paused = false
			if err := st.CreateMonitor(&m); err != nil {
				return err
			}
			cur = append(cur, &m)
			im.res.Created["monitors"]++
		}
		if m.Type == check.TypePush {
			if err := im.pushToken(&m, x.PushToken, prev); err != nil {
				return err
			}
		}
		if cm, err := st.Monitor(m.ID); err == nil && cm.Paused != paused {
			if err := st.SetMonitorPaused(m.ID, paused); err != nil {
				return err
			}
		}
		im.monitors[oldID] = m.ID
	}
	return nil
}

// pushToken keeps heartbeat URLs working: the token from the file, else the
// one of the matching old monitor, else a new one.
func (im *importer) pushToken(m *store.Monitor, fromDoc string, prev *store.Monitor) error {
	st := im.h.store
	cur, err := st.Monitor(m.ID)
	if err != nil {
		return err
	}
	want := fromDoc
	if want == "" && prev != nil {
		want = prev.PushToken
	}
	if want != "" && want != cur.PushToken {
		if o, err := st.MonitorByPushToken(want); err == nil && o.ID != m.ID {
			want = "" // used by another monitor
		}
	}
	if want == "" {
		if cur.PushToken != "" {
			return nil
		}
		want = newPushToken()
		im.res.warn("monitor %q has a new heartbeat URL: update the job that calls it", m.Name)
	}
	if want == cur.PushToken {
		return nil
	}
	return st.SetPushToken(m.ID, want)
}

func (im *importer) importMaintenances() error {
	h, st := im.h, im.h.store
	cur, err := st.ListMaintenances()
	if err != nil {
		return err
	}
	for _, x := range im.doc.Maintenances {
		m := x.Maintenance
		m.ID = 0
		m.MonitorIDs = im.mapIDs(m.MonitorIDs, im.monitors)
		m.GroupIDs = im.mapIDs(m.GroupIDs, im.groups)
		m.TagIDs = im.mapIDs(m.TagIDs, im.tags)
		if err := h.validateMaintenance(&m); err != nil {
			im.res.warn("maintenance %q skipped: %v", x.Name, err)
			im.res.Skipped["maintenances"]++
			continue
		}
		for _, c := range cur {
			if strings.EqualFold(c.Name, m.Name) {
				m.ID, m.CreatedAt = c.ID, c.CreatedAt
			}
		}
		if m.ID != 0 {
			if err := st.UpdateMaintenance(&m); err != nil {
				return err
			}
			im.res.Updated["maintenances"]++
			continue
		}
		if err := st.CreateMaintenance(&m); err != nil {
			return err
		}
		cur = append(cur, &m)
		im.res.Created["maintenances"]++
	}
	return nil
}

func (im *importer) importStatusPages() error {
	h, st := im.h, im.h.store
	req := &http.Request{Host: im.opt.Host}
	for _, x := range im.doc.StatusPages {
		p := x.StatusPage
		p.ID = 0
		p.MonitorIDs = im.mapIDs(p.MonitorIDs, im.monitors)
		p.GroupIDs = im.mapIDs(p.GroupIDs, im.groups)
		p.SubscribeNotifierID = im.notifiers[p.SubscribeNotifierID] // 0 if the channel was skipped
		if err := h.validateStatusPage(req, &p); err != nil {
			im.res.warn("status page %q skipped: %v", x.Slug, err)
			im.res.Skipped["status_pages"]++
			continue
		}
		// The password comes with an export with secrets. Without it a
		// protected page keeps its current password, and a new one is
		// created as a draft rather than opened to everyone.
		p.Password = ""
		p.PasswordHash = ""
		if strings.HasPrefix(x.PasswordHash, "$2") {
			p.PasswordHash = x.PasswordHash
		}
		c, e := st.StatusPageBySlug(p.Slug)
		if p.PasswordHash == "" && x.Protected {
			if e == nil && c.PasswordHash != "" {
				p.PasswordHash = c.PasswordHash
			} else if p.Published {
				p.Published = false
				im.res.warn("status page %q saved as a draft: its password is not in the export, set one and publish it", p.Slug)
			}
		}
		p.Protected = p.PasswordHash != ""
		var err error
		if e == nil {
			p.ID = c.ID
			err = st.UpdateStatusPage(&p)
			if err == nil {
				im.res.Updated["status_pages"]++
			}
		} else {
			err = st.CreateStatusPage(&p)
			if err == nil {
				im.res.Created["status_pages"]++
			}
		}
		if errors.Is(err, store.ErrConflict) {
			im.res.warn("status page %q skipped: its domain is used by another page", p.Slug)
			im.res.Skipped["status_pages"]++
			continue
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// configReloaded refreshes the in-memory state after an import.
func (h *Hub) configReloaded() {
	if err := h.loadSettings(); err != nil {
		h.log.Error("load settings", "err", err)
	}
	h.oidc.reset()
	ms, err := h.store.ListMonitors()
	if err != nil {
		h.log.Error("reload monitors", "err", err)
		return
	}
	keep := map[int64]bool{}
	for _, m := range ms {
		keep[m.ID] = true
	}
	h.mu.Lock()
	gone := []int64{}
	for id := range h.monitors {
		if !keep[id] {
			gone = append(gone, id)
		}
	}
	h.mu.Unlock()
	for _, id := range gone {
		h.MonitorChanged(id)
	}
	for _, m := range ms {
		h.MonitorChanged(m.ID)
	}
	h.labelsChanged() // also reloads maintenance windows and status page caches
	h.reloadPageDomains()
}

// ---------- API ----------

// exportConfig serves the configuration as a JSON download. ?secrets=1
// includes passwords and tokens; it needs a browser session.
func (h *Hub) exportConfig(w http.ResponseWriter, r *http.Request, u *store.User) {
	if !u.IsAdmin() {
		writeErr(w, http.StatusForbidden, "only admins can do this")
		return
	}
	secrets := r.URL.Query().Get("secrets") == "1" || r.URL.Query().Get("secrets") == "true"
	if secrets {
		if _, tok, _ := h.authenticate(r); tok != nil {
			writeErr(w, http.StatusForbidden, "API tokens cannot export secrets, log in from the browser")
			return
		}
	}
	doc, err := h.ExportConfig(secrets)
	if err != nil {
		h.internalErr(w, err)
		return
	}
	b, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		h.internalErr(w, err)
		return
	}
	name := "pingtxt-config-" + time.Now().UTC().Format("20060102-150405") + ".json"
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	w.Write(append(b, '\n'))
}

// importConfig applies a configuration document sent as the request body.
// Query: mode=merge|replace, settings=0 to leave instance settings alone.
func (h *Hub) importConfig(w http.ResponseWriter, r *http.Request, _ *store.User) {
	q := r.URL.Query()
	mode := q.Get("mode")
	if mode == "" {
		mode = "merge"
	}
	if mode != "merge" && mode != "replace" {
		writeErr(w, 400, "mode must be merge or replace")
		return
	}
	var raw json.RawMessage
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<20)).Decode(&raw); err != nil {
		writeErr(w, 400, "invalid body")
		return
	}
	doc, err := ParseConfig(raw)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	res, err := h.ImportConfig(doc, ImportOptions{
		Replace:  mode == "replace",
		Settings: q.Get("settings") != "0" && q.Get("settings") != "false",
		Backup:   true,
		Host:     r.Host,
	})
	if err != nil {
		if res != nil && res.Backup != "" {
			writeErr(w, 500, "import failed: "+err.Error()+". restore "+res.Backup+" to undo the partial import")
			return
		}
		h.internalErr(w, err)
		return
	}
	writeJSON(w, 200, res)
}

package hub

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"sort"
	"strings"

	"sigs.k8s.io/yaml"

	"github.com/federico-it/pingtxt/internal/check"
	"github.com/federico-it/pingtxt/internal/store"
)

// Configuration as code: the monitors, groups, tags, notification channels,
// maintenance windows and status pages as a YAML file that people write by
// hand and keep in git. Unlike the export (configio.go) the file refers to
// other objects by name, leaves out every value that is the default, and is
// declarative: applying it makes the hub match it. A plan shows what applying
// would change first.
//
// Objects are matched by name (monitors and channels by type and name,
// status pages by slug). Applying only touches the sections present in the
// file; with prune, objects of those sections that the file does not list are
// deleted. Agents are not in the file (they have tokens): monitors name them
// in their locations.
//
// The code form is built from the export document, so a new field of a
// monitor, channel, maintenance or page is in it with no change here. Only the
// references (ids) are translated to names.

const codeVersion = 1

// obj is one object of the code form, as decoded from JSON.
type obj = map[string]any

// CodeDoc is the configuration as code document. A nil section is not in
// the file and is left alone.
type CodeDoc struct {
	Version      int                        `json:"version"`
	Settings     map[string]json.RawMessage `json:"settings,omitempty"`
	Groups       *[]obj                     `json:"groups,omitempty"`
	Tags         *[]obj                     `json:"tags,omitempty"`
	Channels     *[]obj                     `json:"channels,omitempty"`
	Monitors     *[]obj                     `json:"monitors,omitempty"`
	Maintenances *[]obj                     `json:"maintenances,omitempty"`
	StatusPages  *[]obj                     `json:"status_pages,omitempty"`
}

// codeKinds are the sections in the order they are applied; deletions go
// the other way.
var codeKinds = []string{"groups", "tags", "channels", "monitors", "maintenances", "status_pages"}

func (d *CodeDoc) section(kind string) *[]obj {
	switch kind {
	case "groups":
		return d.Groups
	case "tags":
		return d.Tags
	case "channels":
		return d.Channels
	case "monitors":
		return d.Monitors
	case "maintenances":
		return d.Maintenances
	}
	return d.StatusPages
}

func (d *CodeDoc) setSection(kind string, l []obj) {
	switch kind {
	case "groups":
		d.Groups = &l
	case "tags":
		d.Tags = &l
	case "channels":
		d.Channels = &l
	case "monitors":
		d.Monitors = &l
	case "maintenances":
		d.Maintenances = &l
	default:
		d.StatusPages = &l
	}
}

// Settings sections the file can hold. Auth (login providers) only from a
// browser session or the hub's own config file; reports refer to channels
// and groups by id and stay in the UI.
var codeSettings = []string{"general", "agent_updates", "backups", "auth"}

// ParseCode decodes a YAML (or JSON) configuration as code document.
func ParseCode(b []byte) (*CodeDoc, error) {
	j, err := yaml.YAMLToJSON(b)
	if err != nil {
		return nil, fmt.Errorf("invalid YAML: %w", err)
	}
	if bytes.Equal(bytes.TrimSpace(j), []byte("null")) {
		return nil, errors.New("the file is empty")
	}
	dec := json.NewDecoder(bytes.NewReader(j))
	dec.DisallowUnknownFields()
	var d CodeDoc
	if err := dec.Decode(&d); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}
	if d.Version == 0 {
		return nil, errors.New("version is missing: start the file with \"version: 1\"")
	}
	if d.Version > codeVersion {
		return nil, fmt.Errorf("unsupported version %d: this hub reads version %d", d.Version, codeVersion)
	}
	for k := range d.Settings {
		if !slices.Contains(codeSettings, k) {
			return nil, fmt.Errorf("settings: unknown section %q (allowed: %s)", k, strings.Join(codeSettings, ", "))
		}
	}
	return &d, nil
}

// ---------- helpers ----------

func toObj(v any) obj {
	b, _ := json.Marshal(v)
	var o obj
	_ = json.Unmarshal(b, &o)
	if o == nil {
		o = obj{}
	}
	return o
}

// fromObj decodes o into v; unknown fields are an error, to catch typos.
func fromObj(o obj, v any) error {
	b, err := json.Marshal(o)
	if err != nil {
		return err
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return errors.New(strings.TrimPrefix(err.Error(), "json: "))
	}
	return nil
}

func isZero(v any) bool {
	switch x := v.(type) {
	case nil:
		return true
	case bool:
		return !x
	case float64:
		return x == 0
	case string:
		return x == ""
	case []any:
		return len(x) == 0
	case map[string]any:
		return len(x) == 0
	}
	return false
}

// Keys whose value is a map of user data: its entries are kept as they are.
var userMaps = map[string]bool{"headers": true, "config": true}

// stripZero drops zero values from nested objects (not from user maps).
func stripZero(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := map[string]any{}
		for k, e := range x {
			if userMaps[k] {
				if !isZero(e) {
					out[k] = e
				}
				continue
			}
			e = stripZero(e)
			if !isZero(e) {
				out[k] = e
			}
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = stripZero(e)
		}
		return out
	}
	return v
}

// canon drops the keys equal to their default (or zero, without default).
func canon(o, defaults obj) obj {
	out := obj{}
	for k, v := range o {
		if !userMaps[k] {
			v = stripZero(v)
		}
		if d, ok := defaults[k]; ok {
			if !reflect.DeepEqual(v, d) {
				out[k] = v
			}
			continue
		}
		if !isZero(v) {
			out[k] = v
		}
	}
	return out
}

func str(o obj, k string) string {
	s, _ := o[k].(string)
	return strings.TrimSpace(s)
}

func lower(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

// strList reads a list of names (a single string counts as a list of one).
func strList(v any) ([]string, error) {
	switch x := v.(type) {
	case nil:
		return nil, nil
	case string:
		return []string{x}, nil
	case []any:
		out := []string{}
		for _, e := range x {
			s, ok := e.(string)
			if !ok {
				return nil, errors.New("must be a list of names")
			}
			out = append(out, s)
		}
		return out, nil
	}
	return nil, errors.New("must be a list of names")
}

func sortedAny(l []string) []any {
	l = slices.Clone(l)
	sort.Slice(l, func(i, j int) bool { return lower(l[i]) < lower(l[j]) })
	out := make([]any, len(l))
	for i, s := range l {
		out[i] = s
	}
	return out
}

// codeKey identifies an object of a section: how files and hubs are matched.
func codeKey(kind string, o obj) string {
	switch kind {
	case "channels", "monitors":
		return str(o, "type") + "/" + lower(str(o, "name"))
	case "status_pages":
		return lower(str(o, "slug"))
	}
	return lower(str(o, "name"))
}

// codeLabel names an object in plans and errors.
func codeLabel(kind string, o obj) string {
	switch kind {
	case "channels", "monitors":
		return str(o, "name") + " (" + str(o, "type") + ")"
	case "status_pages":
		return str(o, "slug")
	}
	return str(o, "name")
}

// Default link events of a monitor's channel: every event a single monitor
// can trigger.
func defaultLinkEvents() []string { return store.CleanLinkEvents(store.DefaultNotifyEvents) }

// ---------- hub -> code ----------

// codeNames maps the ids of an export to names.
type codeNames struct {
	groups, tags, channels, monitors, agents map[int64]string
}

func (n codeNames) list(ids []int64, m map[int64]string) []string {
	out := []string{}
	for _, id := range ids {
		if s, ok := m[id]; ok {
			out = append(out, s)
		}
	}
	return out
}

// ToCode turns an export document into the code form. Secret values the
// export redacted are left out (the hub keeps its own when applied).
func (h *Hub) ToCode(doc *ConfigDoc) *CodeDoc {
	n := codeNames{map[int64]string{}, map[int64]string{}, map[int64]string{}, map[int64]string{}, map[int64]string{}}
	for _, x := range doc.Groups {
		n.groups[x.ID] = x.Name
	}
	for _, x := range doc.Tags {
		n.tags[x.ID] = x.Name
	}
	for _, x := range doc.Notifiers {
		n.channels[x.ID] = x.Name
	}
	for _, x := range doc.Monitors {
		n.monitors[x.ID] = x.Name
	}
	for _, x := range doc.Agents {
		n.agents[x.ID] = x.Name
	}
	c := &CodeDoc{Version: codeVersion}
	groups, tags, channels, monitors, maints, pages := []obj{}, []obj{}, []obj{}, []obj{}, []obj{}, []obj{}
	for _, x := range doc.Groups {
		o := obj{"name": x.Name, "description": x.Description}
		if p, ok := n.groups[x.ParentID]; ok {
			o["parent"] = p
		}
		groups = append(groups, canon(o, nil))
	}
	for _, x := range doc.Tags {
		tags = append(tags, canon(obj{"name": x.Name, "color": x.Color}, nil))
	}
	for _, x := range doc.Notifiers {
		o := toObj(x.Notifier)
		delete(o, "id")
		delete(o, "created_at")
		delete(o, "monitors") // links are on the monitors
		delete(o, "group_ids")
		delete(o, "tag_ids")
		o["groups"] = sortedAny(n.list(x.GroupIDs, n.groups))
		o["tags"] = sortedAny(n.list(x.TagIDs, n.tags))
		cfg := obj{}
		for k, v := range x.Config {
			if v != redactedValue && v != "" {
				cfg[k] = v
			}
		}
		o["config"] = cfg
		channels = append(channels, canonChannel(o))
	}
	for _, x := range doc.Monitors {
		m := x.Monitor
		o := toObj(m)
		o["group"] = n.groups[m.GroupID]
		o["tags"] = sortedAny(n.list(m.TagIDs, n.tags))
		o["depends_on"] = sortedAny(n.list(m.DependsOn, n.monitors))
		locs := []string{}
		for _, l := range m.Locations {
			if id, ok := parseAgentLocation(l); ok {
				if a, ok := n.agents[id]; ok {
					locs = append(locs, a)
				}
			} else if l == LocalLocation {
				locs = append(locs, l)
			}
		}
		o["locations"] = sortedAny(locs)
		links := []any{}
		for _, l := range m.Notifiers {
			if name, ok := n.channels[l.NotifierID]; ok {
				ev := make([]any, len(l.Events))
				for i, e := range l.Events {
					ev[i] = e
				}
				links = append(links, obj{"channel": name, "events": ev})
			}
		}
		o["channels"] = links
		monitors = append(monitors, h.canonMonitor(o))
	}
	for _, x := range doc.Maintenances {
		o := toObj(x.Maintenance)
		o["monitors"] = sortedAny(n.list(x.MonitorIDs, n.monitors))
		o["groups"] = sortedAny(n.list(x.GroupIDs, n.groups))
		o["tags"] = sortedAny(n.list(x.TagIDs, n.tags))
		maints = append(maints, canonMaint(o))
	}
	for _, x := range doc.StatusPages {
		o := toObj(x.StatusPage)
		o["monitors"] = sortedAny(n.list(x.MonitorIDs, n.monitors))
		o["groups"] = sortedAny(n.list(x.GroupIDs, n.groups))
		o["subscribe_channel"] = n.channels[x.SubscribeNotifierID]
		pages = append(pages, canonPage(o))
	}
	c.setSection("groups", groups)
	c.setSection("tags", tags)
	c.setSection("channels", channels)
	c.setSection("monitors", monitors)
	c.setSection("maintenances", maints)
	c.setSection("status_pages", pages)
	return c
}

// Fields of the API objects that are state or ids, not configuration.
var (
	monitorDrop = []string{"id", "push_token", "status", "status_since", "created_at", "group_id", "tag_ids", "notifiers", "icon_url", "icon_dark_url", "icon_image"}
	maintDrop   = []string{"id", "created_at", "monitor_ids", "group_ids", "tag_ids"}
	pageDrop    = []string{"id", "created_at", "monitor_ids", "group_ids", "subscribe_notifier_id", "password", "protected"}
)

func dropKeys(o obj, keys []string) obj {
	o = maps.Clone(o)
	for _, k := range keys {
		delete(o, k)
	}
	return o
}

// monitorRefs are the code fields of a monitor that name other objects.
var monitorRefs = []string{"group", "tags", "depends_on", "locations", "channels"}

// canonMonitor returns the code form of a monitor without its default
// values. The defaults are those the hub fills in for a monitor of the same
// type and target, with the instance's monitor defaults.
func (h *Hub) canonMonitor(o obj) obj {
	o = dropKeys(o, monitorDrop)
	refs := obj{}
	for _, k := range monitorRefs {
		if v, ok := o[k]; ok {
			refs[k] = v
			delete(o, k)
		}
	}
	full := o
	var base obj
	if m, err := h.monitorWithDefaults(o); err == nil {
		full = dropKeys(toObj(m), append(slices.Clone(monitorDrop), "locations", "depends_on"))
		// The defaults: the same type and target with nothing else set.
		if b, err := h.monitorWithDefaults(obj{"name": "x", "type": o["type"], "target": o["target"], "keyword": o["keyword"]}); err == nil {
			base = dropKeys(toObj(b), append(slices.Clone(monitorDrop), "locations", "depends_on", "name", "type", "target", "keyword"))
		}
	}
	out := canon(full, base)
	out["name"], out["type"] = str(o, "name"), str(o, "type")
	// References: sorted names, defaults left out.
	if g, _ := refs["group"].(string); strings.TrimSpace(g) != "" {
		out["group"] = strings.TrimSpace(g)
	}
	for _, k := range []string{"tags", "depends_on", "locations"} {
		l, _ := strList(refs[k])
		if k == "locations" && (str(out, "type") == check.TypePush || slices.Equal(l, []string{LocalLocation})) {
			continue
		}
		if len(l) > 0 {
			out[k] = sortedAny(l)
		}
	}
	if links := canonLinks(refs["channels"]); len(links) > 0 {
		out["channels"] = links
	}
	return out
}

// canonLinks: a channel with the default events is just its name.
func canonLinks(v any) []any {
	l, _ := v.([]any)
	out := []any{}
	for _, e := range l {
		switch x := e.(type) {
		case string:
			out = append(out, x)
		case map[string]any:
			name := str(x, "channel")
			ev, err := strList(x["events"])
			if err != nil {
				out = append(out, x)
				continue
			}
			ev = store.CleanLinkEvents(ev)
			if _, ok := x["events"]; !ok || slices.Equal(ev, defaultLinkEvents()) {
				out = append(out, name)
			} else {
				out = append(out, obj{"channel": name, "events": sortedAny(ev)})
			}
		default:
			out = append(out, e)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return lower(linkName(out[i])) < lower(linkName(out[j])) })
	return out
}

func linkName(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	if m, ok := v.(map[string]any); ok {
		return str(m, "channel")
	}
	return ""
}

// monitorWithDefaults fills in a monitor written without references the way
// the hub does on save.
func (h *Hub) monitorWithDefaults(o obj) (*store.Monitor, error) {
	var m store.Monitor
	if err := fromObj(dropKeys(o, monitorRefs), &m); err != nil {
		return nil, err
	}
	d := h.generalSettings().MonitorDefaults
	if m.IntervalSec == 0 && m.Type != check.TypePush {
		m.IntervalSec = d.IntervalSec
	}
	if m.TimeoutMs == 0 {
		m.TimeoutMs = d.TimeoutMs
	}
	if m.Retries == 0 {
		m.Retries = d.Retries
	}
	m.GroupID, m.TagIDs, m.DependsOn, m.Locations, m.Notifiers = 0, nil, nil, nil, nil
	if err := h.validateMonitor(&m); err != nil {
		return nil, err
	}
	return &m, nil
}

var channelDefaults = obj{"enabled": true, "all_monitors": true}

func canonChannel(o obj) obj {
	d := maps.Clone(channelDefaults)
	d["events"] = sortedAny(store.DefaultNotifyEvents)
	if ev, err := strList(o["events"]); err == nil && o["events"] != nil {
		o["events"] = sortedAny(store.CleanEvents(ev))
	}
	for _, k := range []string{"groups", "tags"} {
		if l, err := strList(o[k]); err == nil {
			o[k] = sortedAny(l)
		}
	}
	if cfg, ok := o["config"].(map[string]any); ok {
		for k, v := range cfg {
			if v == "" {
				delete(cfg, k)
			}
		}
	}
	return canon(o, d)
}

var maintDefaults = obj{"enabled": true, "timezone": "UTC"}

func canonMaint(o obj) obj {
	o = dropKeys(o, maintDrop)
	for _, k := range []string{"monitors", "groups", "tags"} {
		if l, err := strList(o[k]); err == nil {
			o[k] = sortedAny(l)
		}
	}
	if o["all_monitors"] == true {
		delete(o, "monitors")
		delete(o, "groups")
		delete(o, "tags")
	}
	return canon(o, maintDefaults)
}

var pageDefaults = obj{"published": true}

func canonPage(o obj) obj {
	o = dropKeys(o, pageDrop)
	for _, k := range []string{"monitors", "groups"} {
		if l, err := strList(o[k]); err == nil {
			o[k] = sortedAny(l)
		}
	}
	return canon(o, pageDefaults)
}

// canonObj is the canonical form of an object of a section, as compared by
// plans and written by exports.
func (h *Hub) canonObj(kind string, o obj) obj {
	o = maps.Clone(o)
	switch kind {
	case "groups", "tags":
		return canon(o, nil)
	case "channels":
		return canonChannel(o)
	case "monitors":
		return h.canonMonitor(o)
	case "maintenances":
		return canonMaint(o)
	}
	return canonPage(o)
}

// ---------- plan ----------

// CodeChange is one change of a plan.
type CodeChange struct {
	Kind   string   `json:"kind"`   // groups, tags, channels, monitors, maintenances, status_pages, settings
	Name   string   `json:"name"`   // name (monitors and channels: "name (type)", pages: slug)
	Action string   `json:"action"` // create, update, delete
	Fields []string `json:"fields,omitempty"`
}

// CodePlan is what applying a file would change.
type CodePlan struct {
	Changes   []CodeChange   `json:"changes"`
	Unchanged map[string]int `json:"unchanged"`
	Errors    []string       `json:"errors"`
	Warnings  []string       `json:"warnings"`
}

// CodeApplyResult is the answer of an apply.
type CodeApplyResult struct {
	Plan    *CodePlan      `json:"plan"`
	Applied bool           `json:"applied"`
	Import  *ImportResult  `json:"import,omitempty"`
	Deleted map[string]int `json:"deleted"`
}

// CodeOptions control a plan or an apply.
type CodeOptions struct {
	// Prune deletes the objects of the sections in the file that the file
	// does not list.
	Prune bool
	// AllowAuth lets the file change login settings (browser session or the
	// hub's own config file, not API tokens).
	AllowAuth bool
	Host      string
}

// codeState is the hub as code and the file merged onto it.
type codeState struct {
	plan    *CodePlan
	current *CodeDoc
	cur     map[string]map[string]obj // kind -> key -> canonical current object
	desired map[string][]obj          // kind -> desired objects, canonical
	same    map[string]map[string]bool
	export  *ConfigDoc // current configuration, with secrets
	// settings to apply, merged onto the current ones
	settings *configSettings
}

func (h *Hub) planCode(file *CodeDoc, opt CodeOptions) (*codeState, error) {
	exp, err := h.ExportConfig(true)
	if err != nil {
		return nil, err
	}
	st := &codeState{
		plan:    &CodePlan{Changes: []CodeChange{}, Unchanged: map[string]int{}, Errors: []string{}, Warnings: []string{}},
		current: h.ToCode(exp),
		cur:     map[string]map[string]obj{},
		desired: map[string][]obj{},
		same:    map[string]map[string]bool{},
		export:  exp,
	}
	p := st.plan
	// Stored secrets of the channels, for the ones the file leaves out.
	secrets := map[string]map[string]string{}
	for _, x := range exp.Notifiers {
		secrets[x.Type+"/"+lower(x.Name)] = x.Config
	}
	for _, kind := range codeKinds {
		cur := map[string]obj{}
		order := []string{}
		for _, o := range *st.current.section(kind) {
			k := codeKey(kind, o)
			if _, dup := cur[k]; dup {
				p.Warnings = append(p.Warnings, fmt.Sprintf("%s: two objects named %q in the hub, only the first one is managed", kind, codeLabel(kind, o)))
				continue
			}
			cur[k] = o
			order = append(order, k)
		}
		st.cur[kind] = cur
		st.same[kind] = map[string]bool{}
		fileList := file.section(kind)
		if fileList == nil { // section not in the file: stays as it is
			for _, k := range order {
				st.desired[kind] = append(st.desired[kind], cur[k])
				st.same[kind][k] = true
			}
			continue
		}
		want := map[string]obj{}
		wantOrder := []string{}
		for i, o := range *fileList {
			if o == nil {
				p.Errors = append(p.Errors, fmt.Sprintf("%s[%d]: empty entry", kind, i))
				continue
			}
			key := codeKey(kind, o)
			label := codeLabel(kind, o)
			if strings.TrimPrefix(key, str(o, "type")+"/") == "" {
				what := "name"
				if kind == "status_pages" {
					what = "slug"
				}
				p.Errors = append(p.Errors, fmt.Sprintf("%s[%d]: %s is required", kind, i, what))
				continue
			}
			if _, dup := want[key]; dup {
				p.Errors = append(p.Errors, fmt.Sprintf("%s: %q is listed twice", kind, label))
				continue
			}
			if kind == "channels" {
				o = maps.Clone(o)
				cfg, _ := o["config"].(map[string]any)
				cfg = maps.Clone(cfg)
				if cfg == nil {
					cfg = map[string]any{}
				}
				// Secrets left out of the file (or redacted) keep their stored value.
				for _, sk := range notifierSecrets[str(o, "type")] {
					if v, ok := cfg[sk]; !ok || v == redactedValue || v == "" {
						if s := secrets[key][sk]; s != "" && s != redactedValue {
							cfg[sk] = s
						}
					}
				}
				o["config"] = cfg
			}
			if kind == "monitors" {
				o = keepMonitorSecrets(o, cur[key])
			}
			want[key] = h.canonObj(kind, o)
			wantOrder = append(wantOrder, key)
		}
		for _, k := range wantOrder {
			w := want[k]
			st.desired[kind] = append(st.desired[kind], w)
			c, ok := cur[k]
			switch {
			case !ok:
				p.Changes = append(p.Changes, CodeChange{Kind: kind, Name: codeLabel(kind, w), Action: "create"})
			case reflect.DeepEqual(c, w):
				st.same[kind][k] = true
				p.Unchanged[kind]++
			default:
				p.Changes = append(p.Changes, CodeChange{Kind: kind, Name: codeLabel(kind, w), Action: "update", Fields: diffFields(c, w)})
			}
		}
		for _, k := range order {
			if _, ok := want[k]; ok {
				continue
			}
			if opt.Prune {
				p.Changes = append(p.Changes, CodeChange{Kind: kind, Name: codeLabel(kind, cur[k]), Action: "delete"})
				continue
			}
			st.desired[kind] = append(st.desired[kind], cur[k])
			st.same[kind][k] = true
		}
	}
	if err := h.planSettings(st, file, opt); err != nil {
		return nil, err
	}
	// Check every reference by building the import document.
	if _, _, err := h.codeToDoc(st); err != nil {
		var refs refErrors
		if errors.As(err, &refs) {
			p.Errors = append(p.Errors, refs...)
		} else {
			return nil, err
		}
	}
	return st, nil
}

// diffFields lists the keys whose values differ, sorted.
func diffFields(a, b obj) []string {
	out := []string{}
	for k, v := range a {
		if !reflect.DeepEqual(v, b[k]) {
			out = append(out, k)
		}
	}
	for k := range b {
		if _, ok := a[k]; !ok {
			out = append(out, k)
		}
	}
	slices.Sort(out)
	return out
}

// planSettings merges the settings sections of the file onto the current
// settings: keys left out keep their value.
func (h *Hub) planSettings(st *codeState, file *CodeDoc, opt CodeOptions) error {
	if len(file.Settings) == 0 {
		return nil
	}
	cur := st.export.Settings
	out := &configSettings{}
	for _, name := range codeSettings {
		raw, ok := file.Settings[name]
		if !ok {
			continue
		}
		if name == "auth" && !opt.AllowAuth {
			st.plan.Errors = append(st.plan.Errors, "settings.auth: login settings can only be applied from a browser session or the hub's own config file, not with an API token")
			continue
		}
		var add obj
		if err := json.Unmarshal(raw, &add); err != nil || add == nil {
			st.plan.Errors = append(st.plan.Errors, "settings."+name+": must be a map")
			continue
		}
		var target any
		switch name {
		case "general":
			target = cur.General
		case "agent_updates":
			target = cur.AgentUpdates
		case "backups":
			target = cur.Backups
		case "auth":
			target = cur.Auth
		}
		before := toObj(target)
		after := maps.Clone(before)
		maps.Copy(after, add)
		var err error
		switch name {
		case "general":
			out.General = &GeneralSettings{}
			err = fromObj(after, out.General)
		case "agent_updates":
			out.AgentUpdates = &AgentUpdateSettings{}
			err = fromObj(after, out.AgentUpdates)
		case "backups":
			out.Backups = &BackupSettings{}
			err = fromObj(after, out.Backups)
		case "auth":
			out.Auth = &AuthSettings{}
			err = fromObj(after, out.Auth)
		}
		if err != nil {
			st.plan.Errors = append(st.plan.Errors, "settings."+name+": "+err.Error())
			continue
		}
		if changed := diffFields(before, toObjOf(name, out)); len(changed) > 0 {
			st.plan.Changes = append(st.plan.Changes, CodeChange{Kind: "settings", Name: name, Action: "update", Fields: changed})
		} else {
			st.plan.Unchanged["settings"]++
		}
	}
	st.settings = out
	return nil
}

func toObjOf(name string, s *configSettings) obj {
	switch name {
	case "general":
		return toObj(s.General)
	case "agent_updates":
		return toObj(s.AgentUpdates)
	case "backups":
		return toObj(s.Backups)
	}
	return toObj(s.Auth)
}

// ---------- code -> import document ----------

type refErrors []string

func (e refErrors) Error() string { return strings.Join(e, "; ") }

// codeToDoc builds the import document of the desired state, with ids made
// up for the references. Unknown names are reported as refErrors.
func (h *Hub) codeToDoc(st *codeState) (*ConfigDoc, map[string]map[int64]bool, error) {
	doc := &ConfigDoc{Format: configFormat, Version: configVersion, Secrets: true,
		Agents: []cfgAgent{}, Groups: []cfgGroup{}, Tags: []cfgTag{}, Notifiers: []cfgNotifier{},
		Monitors: []cfgMonitor{}, Maintenances: []cfgMaintenance{}, StatusPages: []cfgStatusPage{}}
	var errs refErrors
	fail := func(format string, a ...any) { errs = append(errs, fmt.Sprintf(format, a...)) }

	// Agents are only referenced: the ones of this hub, by name.
	agents := map[string]int64{}
	for _, a := range st.export.Agents {
		if _, dup := agents[lower(a.Name)]; dup {
			agents[lower(a.Name)] = -1 // ambiguous
			continue
		}
		agents[lower(a.Name)] = a.ID
		doc.Agents = append(doc.Agents, cfgAgent{ID: a.ID, Name: a.Name, Location: a.Location})
	}
	ids := map[string]map[string]int64{} // kind -> lower(name) -> made-up id (-1 = ambiguous)
	for _, kind := range codeKinds {
		ids[kind] = map[string]int64{}
		for i, o := range st.desired[kind] {
			n := lower(str(o, "name"))
			if _, dup := ids[kind][n]; dup {
				ids[kind][n] = -1
				continue
			}
			ids[kind][n] = int64(i + 1)
		}
	}
	ref := func(kind, owner, name string) int64 {
		id, ok := ids[kind][lower(name)]
		switch {
		case !ok:
			fail("%s: unknown %s %q", owner, strings.TrimSuffix(strings.ReplaceAll(kind, "_", " "), "s"), name)
		case id < 0:
			fail("%s: more than one %s is named %q", owner, strings.TrimSuffix(kind, "s"), name)
		}
		return max(id, 0)
	}
	refs := func(kind, owner string, v any) []int64 {
		l, err := strList(v)
		if err != nil {
			fail("%s: %s %v", owner, kind, err)
		}
		out := []int64{}
		for _, n := range l {
			if id := ref(kind, owner, n); id > 0 {
				out = append(out, id)
			}
		}
		return out
	}
	unchanged := func(kind string, o obj) bool { return st.same[kind][codeKey(kind, o)] }
	opt := map[string]map[int64]bool{}
	mark := func(kind, impKind string, o obj, id int64) {
		if unchanged(kind, o) {
			if opt[impKind] == nil {
				opt[impKind] = map[int64]bool{}
			}
			opt[impKind][id] = true
		}
	}

	for i, o := range st.desired["groups"] {
		id := int64(i + 1)
		owner := "group " + str(o, "name")
		var g store.Group
		if err := fromObj(dropKeys(o, []string{"parent"}), &g); err != nil {
			fail("%s: %v", owner, err)
		}
		g.ID = id
		if p := str(o, "parent"); p != "" {
			g.ParentID = ref("groups", owner, p)
		}
		doc.Groups = append(doc.Groups, cfgGroup{Group: g})
		mark("groups", "groups", o, id)
	}
	for i, o := range st.desired["tags"] {
		var t store.Tag
		if err := fromObj(o, &t); err != nil {
			fail("tag %s: %v", str(o, "name"), err)
		}
		t.ID = int64(i + 1)
		doc.Tags = append(doc.Tags, cfgTag{Tag: t})
		mark("tags", "tags", o, t.ID)
	}
	for i, o := range st.desired["channels"] {
		owner := "channel " + codeLabel("channels", o)
		full := maps.Clone(o)
		for k, v := range channelDefaults {
			if _, ok := full[k]; !ok {
				full[k] = v
			}
		}
		if _, ok := full["events"]; !ok {
			full["events"] = sortedAny(store.DefaultNotifyEvents)
		}
		var n store.Notifier
		if err := fromObj(dropKeys(full, []string{"groups", "tags"}), &n); err != nil {
			fail("%s: %v", owner, err)
		}
		if c := n; validateNotifier(&c) != nil {
			fail("%s: %v", owner, validateNotifier(&c))
		}
		n.ID = int64(i + 1)
		n.GroupIDs = refs("groups", owner, o["groups"])
		n.TagIDs = refs("tags", owner, o["tags"])
		if n.Config == nil {
			n.Config = map[string]string{}
		}
		for _, sk := range notifierSecrets[n.Type] {
			if n.Config[sk] == "" {
				n.Config[sk] = redactedValue // keep the stored one (or skip a new channel)
			}
		}
		all, events := n.AllMonitors, slices.Clone(n.Events)
		doc.Notifiers = append(doc.Notifiers, cfgNotifier{Notifier: n, AllMonitors: &all, Events: &events})
		mark("channels", "notifiers", o, n.ID)
	}
	for i, o := range st.desired["monitors"] {
		owner := "monitor " + codeLabel("monitors", o)
		m, err := monitorFromCode(o)
		if err != nil {
			fail("%s: %v", owner, err)
			m = &store.Monitor{}
		} else if _, err := h.monitorWithDefaults(o); err != nil {
			fail("%s: %v", owner, err)
		}
		m.ID = int64(i + 1)
		d := h.generalSettings().MonitorDefaults
		if m.IntervalSec == 0 && m.Type != check.TypePush {
			m.IntervalSec = d.IntervalSec
		}
		if m.TimeoutMs == 0 {
			m.TimeoutMs = d.TimeoutMs
		}
		if m.Retries == 0 {
			m.Retries = d.Retries
		}
		if g := str(o, "group"); g != "" {
			m.GroupID = ref("groups", owner, g)
		}
		m.TagIDs = refs("tags", owner, o["tags"])
		m.DependsOn = refs("monitors", owner, o["depends_on"])
		locs, err := strList(o["locations"])
		if err != nil {
			fail("%s: locations %v", owner, err)
		}
		m.Locations = []string{}
		for _, l := range locs {
			if lower(l) == LocalLocation || lower(l) == lower(h.localName()) {
				m.Locations = append(m.Locations, LocalLocation)
				continue
			}
			switch id, ok := agents[lower(l)]; {
			case !ok:
				fail("%s: unknown location %q (use \"local\" or the name of an agent)", owner, l)
			case id < 0:
				fail("%s: more than one agent is named %q", owner, l)
			default:
				m.Locations = append(m.Locations, AgentLocation(id))
			}
		}
		if len(m.Locations) == 0 {
			m.Locations = []string{LocalLocation}
		}
		m.Notifiers = []store.ChannelLink{}
		links, _ := o["channels"].([]any)
		if o["channels"] != nil && links == nil {
			fail("%s: channels must be a list", owner)
		}
		for _, l := range links {
			name, ev := linkName(l), defaultLinkEvents()
			if x, ok := l.(map[string]any); ok {
				if e, err := strList(x["events"]); err != nil {
					fail("%s: channel %q events %v", owner, name, err)
				} else if _, has := x["events"]; has {
					ev = e
				}
			}
			if id := ref("channels", owner, name); id > 0 {
				m.Notifiers = append(m.Notifiers, store.ChannelLink{NotifierID: id, Events: ev})
			}
		}
		doc.Monitors = append(doc.Monitors, cfgMonitor{Monitor: *m})
		mark("monitors", "monitors", o, m.ID)
	}
	for i, o := range st.desired["maintenances"] {
		owner := "maintenance " + str(o, "name")
		full := maps.Clone(o)
		for k, v := range maintDefaults {
			if _, ok := full[k]; !ok {
				full[k] = v
			}
		}
		var m store.Maintenance
		if err := fromObj(dropKeys(full, []string{"monitors", "groups", "tags"}), &m); err != nil {
			fail("%s: %v", owner, err)
		}
		m.ID = int64(i + 1)
		m.MonitorIDs = refs("monitors", owner, o["monitors"])
		m.GroupIDs = refs("groups", owner, o["groups"])
		m.TagIDs = refs("tags", owner, o["tags"])
		doc.Maintenances = append(doc.Maintenances, cfgMaintenance{Maintenance: m})
		mark("maintenances", "maintenances", o, m.ID)
	}
	for i, o := range st.desired["status_pages"] {
		owner := "status page " + str(o, "slug")
		full := maps.Clone(o)
		for k, v := range pageDefaults {
			if _, ok := full[k]; !ok {
				full[k] = v
			}
		}
		var p store.StatusPage
		if err := fromObj(dropKeys(full, []string{"monitors", "groups", "subscribe_channel"}), &p); err != nil {
			fail("%s: %v", owner, err)
		}
		p.ID = int64(i + 1)
		p.MonitorIDs = refs("monitors", owner, o["monitors"])
		p.GroupIDs = refs("groups", owner, o["groups"])
		if c := str(o, "subscribe_channel"); c != "" {
			p.SubscribeNotifierID = ref("channels", owner, c)
		}
		// A protected page keeps its password (the import leaves it alone).
		if c, err := h.store.StatusPageBySlug(p.Slug); err == nil && c.PasswordHash != "" {
			p.Protected = true
		}
		doc.StatusPages = append(doc.StatusPages, cfgStatusPage{StatusPage: p})
		mark("status_pages", "status_pages", o, p.ID)
	}
	if st.settings != nil {
		doc.Settings = st.settings
	}
	if len(errs) > 0 {
		return nil, nil, errs
	}
	return doc, opt, nil
}

// monitorFromCode decodes a monitor of the file without its references.
func monitorFromCode(o obj) (*store.Monitor, error) {
	var m store.Monitor
	if err := fromObj(dropKeys(o, monitorRefs), &m); err != nil {
		return nil, err
	}
	return &m, nil
}

// ---------- apply ----------

// PlanCode works out what applying file would change.
func (h *Hub) PlanCode(file *CodeDoc, opt CodeOptions) (*CodePlan, error) {
	st, err := h.planCode(file, opt)
	if err != nil {
		return nil, err
	}
	return st.plan, nil
}

// ApplyCode makes the hub match file. Nothing is changed when the plan has
// errors.
func (h *Hub) ApplyCode(file *CodeDoc, opt CodeOptions) (*CodeApplyResult, error) {
	st, err := h.planCode(file, opt)
	if err != nil {
		return nil, err
	}
	res := &CodeApplyResult{Plan: st.plan, Deleted: map[string]int{}}
	if len(st.plan.Errors) > 0 || len(st.plan.Changes) == 0 {
		return res, nil
	}
	doc, unchanged, err := h.codeToDoc(st)
	if err != nil {
		return nil, err
	}
	ir, err := h.ImportConfig(doc, ImportOptions{
		Settings:  doc.Settings != nil,
		Backup:    true,
		Host:      opt.Host,
		Unchanged: unchanged,
	})
	res.Import = ir
	if err != nil {
		return res, err
	}
	res.Applied = true
	if opt.Prune {
		if err := h.pruneCode(st, res); err != nil {
			return res, err
		}
	}
	return res, nil
}

// pruneCode deletes the objects the plan marked for deletion, pages first.
func (h *Hub) pruneCode(st *codeState, res *CodeApplyResult) error {
	sx := h.store
	del := map[string]map[string]bool{}
	for _, c := range st.plan.Changes {
		if c.Action == "delete" {
			if del[c.Kind] == nil {
				del[c.Kind] = map[string]bool{}
			}
			del[c.Kind][c.Name] = true
		}
	}
	if len(del) == 0 {
		return nil
	}
	e := st.export
	label := func(kind string, v any) string { return codeLabel(kind, toObj(v)) }
	type item struct {
		kind string
		id   int64
		fn   func(int64) error
	}
	var items []item
	for _, x := range e.StatusPages {
		if del["status_pages"][x.Slug] {
			items = append(items, item{"status_pages", x.ID, sx.DeleteStatusPage})
		}
	}
	for _, x := range e.Maintenances {
		if del["maintenances"][x.Name] {
			items = append(items, item{"maintenances", x.ID, sx.DeleteMaintenance})
		}
	}
	for _, x := range e.Monitors {
		if del["monitors"][label("monitors", x.Monitor)] {
			items = append(items, item{"monitors", x.ID, sx.DeleteMonitor})
		}
	}
	for _, x := range e.Notifiers {
		if del["channels"][label("channels", x.Notifier)] {
			items = append(items, item{"channels", x.ID, sx.DeleteNotifier})
		}
	}
	for _, x := range e.Tags {
		if del["tags"][x.Name] {
			items = append(items, item{"tags", x.ID, sx.DeleteTag})
		}
	}
	for _, x := range e.Groups {
		if del["groups"][x.Name] {
			items = append(items, item{"groups", x.ID, sx.DeleteGroup})
		}
	}
	defer h.configReloaded()
	for _, it := range items {
		if err := it.fn(it.id); err != nil && !errors.Is(err, store.ErrNotFound) {
			return fmt.Errorf("delete %s %d: %w", it.kind, it.id, err)
		}
		res.Deleted[it.kind]++
	}
	return nil
}

// keepMonitorSecrets puts back the credentials an export without secrets
// redacted (header values, the password in the target) from the monitor of
// the hub, so that such a file plans no change.
func keepMonitorSecrets(o, cur obj) obj {
	if cur == nil {
		return o
	}
	o = maps.Clone(o)
	if t := str(o, "target"); strings.Contains(t, redactedPassword) {
		if ct := str(cur, "target"); ct != "" && exportTarget(ct) == t {
			o["target"] = ct
		}
	}
	hs, _ := o["headers"].(map[string]any)
	ch, _ := cur["headers"].(map[string]any)
	if len(hs) > 0 && len(ch) > 0 {
		hs = maps.Clone(hs)
		for k, v := range hs {
			if v == redactedValue && ch[k] != nil {
				hs[k] = ch[k]
			}
		}
		o["headers"] = hs
	}
	return o
}

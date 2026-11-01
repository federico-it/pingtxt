package hub

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"net"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/federico-it/pingtxt/internal/check"
	"github.com/federico-it/pingtxt/internal/store"
)

// Import of the monitors of an Uptime Kuma backup (Settings → Backup →
// Export in Uptime Kuma 1.x). Groups become pingtxt groups, tags become
// tags, paused monitors stay paused. Notification channels are not imported.

// kumaBackup is the part of an Uptime Kuma backup file that is used.
type kumaBackup struct {
	Version          string          `json:"version"`
	NotificationList json.RawMessage `json:"notificationList"`
	MonitorList      []kumaMonitor   `json:"monitorList"`
}

type kumaMonitor struct {
	ID                  int64      `json:"id"`
	Name                string     `json:"name"`
	Type                string     `json:"type"`
	URL                 string     `json:"url"`
	Method              string     `json:"method"`
	Hostname            string     `json:"hostname"`
	Port                kumaNumber `json:"port"`
	Interval            kumaNumber `json:"interval"`
	Timeout             kumaNumber `json:"timeout"`
	MaxRetries          kumaNumber `json:"maxretries"`
	Active              kumaBool   `json:"active"`
	Keyword             string     `json:"keyword"`
	InvertKeyword       kumaBool   `json:"invertKeyword"`
	IgnoreTLS           kumaBool   `json:"ignoreTls"`
	UpsideDown          kumaBool   `json:"upsideDown"`
	ExpiryNotification  kumaBool   `json:"expiryNotification"`
	AcceptedStatusCodes []string   `json:"accepted_statuscodes"`
	DNSResolveType      string     `json:"dns_resolve_type"`
	DNSResolveServer    string     `json:"dns_resolve_server"`
	DockerContainer     string     `json:"docker_container"`
	DatabaseConnection  string     `json:"databaseConnectionString"`
	DatabaseQuery       string     `json:"databaseQuery"`
	Headers             string     `json:"headers"`
	Body                string     `json:"body"`
	BasicAuthUser       string     `json:"basic_auth_user"`
	BasicAuthPass       string     `json:"basic_auth_pass"`
	AuthMethod          string     `json:"authMethod"`
	Parent              kumaNumber `json:"parent"`
	Tags                []kumaTag  `json:"tags"`
	SMTPSecurity        string     `json:"smtpSecurity"`
}

type kumaTag struct {
	Name  string `json:"name"`
	Color string `json:"color"`
	Value string `json:"value"`
}

// kumaNumber reads numbers that Kuma writes as numbers, strings or null.
type kumaNumber float64

func (n *kumaNumber) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if s == "" || s == "null" {
		*n = 0
		return nil
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return nil // unknown value: treat as unset
	}
	*n = kumaNumber(f)
	return nil
}

// kumaBool reads booleans that Kuma writes as true/false, 1/0 or null.
type kumaBool bool

func (v *kumaBool) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	*v = s == "true" || s == "1"
	return nil
}

// kumaPlanItem says what happens to one Kuma monitor.
type kumaPlanItem struct {
	Name     string `json:"name"`
	KumaType string `json:"kuma_type"`
	Type     string `json:"type,omitempty"`
	Target   string `json:"target,omitempty"`
	Group    string `json:"group,omitempty"`
	Paused   bool   `json:"paused,omitempty"`
	// Action is "create", "exists" (a monitor with this name is already
	// there) or "skip" (not supported, see Reason).
	Action string `json:"action"`
	Reason string `json:"reason,omitempty"`
	Note   string `json:"note,omitempty"` // created, but something was left out

	monitor *store.Monitor
	tags    []kumaTag
}

type kumaPlan struct {
	Version  string         `json:"version"`
	Monitors []kumaPlanItem `json:"monitors"`
	Groups   []string       `json:"groups"`   // groups the import uses (new or existing)
	Tags     []string       `json:"tags"`     // tags the import uses (new or existing)
	Warnings []string       `json:"warnings"` // about the whole file
	Created  int            `json:"created"`  // monitors created (0 for a preview)
}

// kumaDNSTypes are the record types pingtxt checks.
var kumaDNSTypes = []string{"A", "AAAA", "CNAME", "MX", "TXT", "NS"}

// planKuma decides what to do with each monitor of a Kuma backup.
func (h *Hub) planKuma(b *kumaBackup) (*kumaPlan, error) {
	existing, err := h.store.ListMonitors()
	if err != nil {
		return nil, err
	}
	names := map[string]bool{}
	for _, m := range existing {
		names[strings.ToLower(m.Name)] = true
	}
	plan := &kumaPlan{Version: b.Version, Monitors: []kumaPlanItem{}, Groups: []string{}, Tags: []string{}, Warnings: []string{}}
	var notifiers []json.RawMessage
	if json.Unmarshal(b.NotificationList, &notifiers) == nil && len(notifiers) > 0 {
		plan.Warnings = append(plan.Warnings, fmt.Sprintf("%d notification channels are not imported: set them up again in pingtxt", len(notifiers)))
	}

	// Kuma "group" monitors are folders: they become pingtxt groups.
	groupNames := map[int64]string{}
	for _, km := range b.MonitorList {
		if km.Type == "group" {
			groupNames[km.ID] = strings.TrimSpace(km.Name)
		}
	}
	seenGroup, seenTag := map[string]bool{}, map[string]bool{}
	for _, km := range b.MonitorList {
		if km.Type == "group" {
			continue
		}
		it := kumaPlanItem{Name: strings.TrimSpace(km.Name), KumaType: km.Type, Paused: !bool(km.Active)}
		if it.Name == "" {
			it.Name = fmt.Sprintf("kuma monitor %d", km.ID)
		}
		it.Group = groupNames[int64(km.Parent)]
		m, note, reason := kumaToMonitor(&km)
		switch {
		case reason != "":
			it.Action, it.Reason = "skip", reason
		case names[strings.ToLower(it.Name)]:
			it.Action, it.Reason = "exists", "a monitor with this name already exists"
		default:
			m.Name = it.Name
			if err := h.validateMonitor(m); err != nil {
				it.Action, it.Reason = "skip", err.Error()
				break
			}
			it.Action, it.Note = "create", note
			it.Type, it.Target = m.Type, check.RedactTarget(m.Target)
			it.monitor, it.tags = m, km.Tags
			names[strings.ToLower(it.Name)] = true // a second one with the same name is a duplicate
			if it.Group != "" && !seenGroup[strings.ToLower(it.Group)] {
				seenGroup[strings.ToLower(it.Group)] = true
				plan.Groups = append(plan.Groups, it.Group)
			}
			for _, t := range km.Tags {
				if n := strings.TrimSpace(t.Name); n != "" && !seenTag[strings.ToLower(n)] {
					seenTag[strings.ToLower(n)] = true
					plan.Tags = append(plan.Tags, n)
				}
			}
		}
		plan.Monitors = append(plan.Monitors, it)
	}
	return plan, nil
}

// kumaToMonitor converts one Kuma monitor. reason is set when it cannot be
// imported; note lists what was left out.
func kumaToMonitor(km *kumaMonitor) (m *store.Monitor, note, reason string) {
	m = &store.Monitor{
		IntervalSec:       int(km.Interval),
		Retries:           int(km.MaxRetries) + 1, // Kuma: retries after the first failure
		Locations:         []string{LocalLocation},
		Headers:           map[string]string{},
		CertExpiryAlert:   bool(km.ExpiryNotification),
		DomainExpiryAlert: false,
		IPFamily:          ipDefault,
	}
	if km.Timeout > 0 {
		m.TimeoutMs = int(math.Round(float64(km.Timeout) * 1000))
	}
	if m.IntervalSec <= 0 {
		m.IntervalSec = 60
	}
	m.IntervalSec = min(max(m.IntervalSec, 10), 86400)
	var notes []string
	if km.UpsideDown {
		return nil, "", "upside down mode is not supported"
	}
	hostPort := func() string {
		return net.JoinHostPort(strings.Trim(km.Hostname, "[]"), strconv.Itoa(int(km.Port)))
	}
	switch km.Type {
	case "http", "keyword", "json-query":
		m.Type = check.TypeHTTP
		if km.Type == "keyword" {
			m.Type = check.TypeKeyword
			m.Keyword = km.Keyword
			m.KeywordInvert = bool(km.InvertKeyword)
		}
		if km.Type == "json-query" {
			notes = append(notes, "the JSON query was not imported: add a response rule")
		}
		m.Target = km.URL
		m.Method = strings.ToUpper(km.Method)
		m.IgnoreTLS = bool(km.IgnoreTLS)
		m.Body = km.Body
		m.ExpectedStatus = strings.Join(km.AcceptedStatusCodes, ",")
		if m.ExpectedStatus == "200-299" {
			m.ExpectedStatus = "" // pingtxt's default is close enough (200-399 follows redirects anyway)
		}
		if km.Headers != "" {
			var hd map[string]any
			if err := json.Unmarshal([]byte(km.Headers), &hd); err != nil {
				notes = append(notes, "headers are not valid JSON and were left out")
			}
			for k, v := range hd {
				m.Headers[k] = fmt.Sprint(v)
			}
		}
		switch km.AuthMethod {
		case "", "basic":
			if km.BasicAuthUser != "" {
				m.Headers["Authorization"] = "Basic " + base64.StdEncoding.EncodeToString([]byte(km.BasicAuthUser+":"+km.BasicAuthPass))
			}
		default:
			notes = append(notes, km.AuthMethod+" authentication was not imported")
		}
	case "port":
		if km.Hostname == "" || km.Port <= 0 {
			return nil, "", "host or port missing"
		}
		m.Type, m.Target = check.TypeTCP, hostPort()
	case "ping":
		m.Type, m.Target = check.TypePing, km.Hostname
	case "dns":
		rt := strings.ToUpper(km.DNSResolveType)
		if rt == "" {
			rt = "A"
		}
		if !slices.Contains(kumaDNSTypes, rt) {
			return nil, "", rt + " records are not supported"
		}
		m.Type, m.Target, m.DNSRecord, m.DNSServer = check.TypeDNS, km.Hostname, rt, km.DNSResolveServer
	case "push":
		m.Type = check.TypePush
		m.GraceSec = 300
		m.Locations = nil
		notes = append(notes, "new push URL: update the job that calls it")
	case "docker":
		m.Type, m.Target = check.TypeDocker, km.DockerContainer
		notes = append(notes, "checked on the docker host of the location (DOCKER_HOST or /var/run/docker.sock)")
	case "postgres", "mysql", "mongodb", "redis":
		m.Type, m.Target = km.Type, km.DatabaseConnection
		if km.Type == "mysql" && !strings.Contains(m.Target, "://") {
			return nil, "", "the MySQL connection string must be a mysql:// URL"
		}
		if km.Type == "postgres" || km.Type == "mysql" {
			m.Body = km.DatabaseQuery
		}
	case "smtp":
		m.Type = check.TypeSMTP
		scheme := "smtp"
		if km.SMTPSecurity == "secure" {
			scheme = "smtps"
		}
		m.Target = scheme + "://" + hostPort()
	case "group":
		return nil, "", "groups become pingtxt groups"
	default:
		return nil, "", "monitor type " + km.Type + " is not supported"
	}
	return m, strings.Join(notes, "; "), ""
}

// kumaColor maps a Kuma tag color (hex) to the closest pingtxt tag color.
func kumaColor(hex string) string {
	hex = strings.TrimPrefix(strings.TrimSpace(hex), "#")
	if len(hex) == 3 {
		hex = string([]byte{hex[0], hex[0], hex[1], hex[1], hex[2], hex[2]})
	}
	v, err := strconv.ParseUint(hex, 16, 32)
	if len(hex) != 6 || err != nil {
		return ""
	}
	r, g, b := float64(v>>16&0xff)/255, float64(v>>8&0xff)/255, float64(v&0xff)/255
	mx, mn := max(r, g, b), min(r, g, b)
	if mx-mn < 0.15 {
		return "gray"
	}
	var hue float64
	switch mx {
	case r:
		hue = math.Mod((g-b)/(mx-mn), 6)
	case g:
		hue = (b-r)/(mx-mn) + 2
	default:
		hue = (r-g)/(mx-mn) + 4
	}
	hue *= 60
	if hue < 0 {
		hue += 360
	}
	switch {
	case hue < 15 || hue >= 345:
		return "red"
	case hue < 40:
		return "orange"
	case hue < 70:
		return "yellow"
	case hue < 168:
		return "green"
	case hue < 200:
		return "cyan"
	case hue < 255:
		return "blue"
	case hue < 300:
		return "purple"
	default:
		return "pink"
	}
}

// applyKuma creates the groups, tags and monitors of a plan.
func (h *Hub) applyKuma(plan *kumaPlan) error {
	groups, err := h.store.ListGroups()
	if err != nil {
		return err
	}
	groupID := map[string]int64{}
	for _, g := range groups {
		groupID[strings.ToLower(g.Name)] = g.ID
	}
	tags, err := h.store.ListTags()
	if err != nil {
		return err
	}
	tagID := map[string]int64{}
	for _, t := range tags {
		tagID[strings.ToLower(t.Name)] = t.ID
	}
	for i := range plan.Monitors {
		it := &plan.Monitors[i]
		if it.Action != "create" {
			continue
		}
		m := it.monitor
		if it.Group != "" {
			key := strings.ToLower(it.Group)
			if _, ok := groupID[key]; !ok {
				g := &store.Group{Name: it.Group}
				if err := h.store.CreateGroup(g); err != nil {
					return fmt.Errorf("group %s: %w", it.Group, err)
				}
				groupID[key] = g.ID
			}
			m.GroupID = groupID[key]
		}
		for _, kt := range it.tags {
			name := strings.TrimSpace(kt.Name)
			if name == "" {
				continue
			}
			key := strings.ToLower(name)
			if _, ok := tagID[key]; !ok {
				t := &store.Tag{Name: name, Color: kumaColor(kt.Color)}
				if err := validateTag(t); err != nil {
					t.Color = ""
					if validateTag(t) != nil {
						continue
					}
				}
				if err := h.store.CreateTag(t); err != nil {
					return fmt.Errorf("tag %s: %w", name, err)
				}
				tagID[key] = t.ID
			}
			m.TagIDs = append(m.TagIDs, tagID[key])
		}
		m.TagIDs = cleanIDs(m.TagIDs)
		if m.Type == check.TypePush {
			m.PushToken = newPushToken()
		}
		if err := h.store.CreateMonitor(m); err != nil {
			return fmt.Errorf("monitor %s: %w", m.Name, err)
		}
		if it.Paused {
			_ = h.store.SetMonitorPaused(m.ID, true)
		}
		h.MonitorChanged(m.ID)
		plan.Created++
	}
	h.labelsChanged()
	return nil
}

// importKuma previews (?dry_run=1) or runs the import of an Uptime Kuma backup.
func (h *Hub) importKuma(w http.ResponseWriter, r *http.Request, _ *store.User) {
	var b kumaBackup
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<20)).Decode(&b); err != nil {
		writeErr(w, 400, "not a JSON file")
		return
	}
	if b.MonitorList == nil {
		writeErr(w, 400, "not an Uptime Kuma backup: monitorList is missing")
		return
	}
	h.importMu.Lock()
	defer h.importMu.Unlock()
	plan, err := h.planKuma(&b)
	if err != nil {
		h.internalErr(w, err)
		return
	}
	dry := r.URL.Query().Get("dry_run")
	if dry == "1" || dry == "true" {
		writeJSON(w, 200, plan)
		return
	}
	if err := h.applyKuma(plan); err != nil {
		writeErr(w, 500, fmt.Sprintf("import stopped after %d monitors: %v", plan.Created, err))
		return
	}
	h.log.Info("uptime kuma import", "created", plan.Created)
	writeJSON(w, 200, plan)
}

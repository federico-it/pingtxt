package check

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"
)

// Statuspage checks read the public API of an Atlassian Statuspage page
// (status.example.com/api/v2/summary.json) and turn its state into the
// monitor's: all operational is up, a minor problem is degraded, a major
// or critical one is down. The message names the open incidents.
//
// Job fields used by this type:
//
//	Target     URL of the status page, e.g. https://status.example.com
//	Keyword    comma-separated component ids to follow; empty = the whole page
//	IgnoreTLS  skip certificate verification

// Component states of a Statuspage page.
const (
	spOperational = "operational"
	spDegraded    = "degraded_performance"
	spPartial     = "partial_outage"
	spMajor       = "major_outage"
	spMaintenance = "under_maintenance"
)

// statuspageMaxBody limits the summary read; big pages are a few hundred KB.
const statuspageMaxBody = 4 << 20

// StatuspageSummary is the part of summary.json the check reads.
type StatuspageSummary struct {
	Page struct {
		Name string `json:"name"`
		URL  string `json:"url"`
	} `json:"page"`
	Status struct {
		Indicator   string `json:"indicator"`
		Description string `json:"description"`
	} `json:"status"`
	Components   []StatuspageComponent `json:"components"`
	Incidents    []statuspageIncident  `json:"incidents"`
	Maintenances []statuspageIncident  `json:"scheduled_maintenances"`
}

// StatuspageComponent is one component of a page. Groups hold other
// components and carry the worst state of their members.
type StatuspageComponent struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Status  string `json:"status"`
	Group   bool   `json:"group"`
	GroupID string `json:"group_id"`
}

type statuspageIncident struct {
	Name       string `json:"name"`
	Status     string `json:"status"`
	Impact     string `json:"impact"`
	Components []struct {
		ID string `json:"id"`
	} `json:"components"`
}

// StatuspageIDs splits the Keyword field into component ids.
func StatuspageIDs(s string) []string { return MCPToolNames(s) }

// StatuspageBase gives the address of the page from what the user typed:
// a host, the page URL or one of its API URLs. It keeps the scheme
// (https by default) and drops the API path, query and fragment.
func StatuspageBase(target string) (string, error) {
	t := strings.TrimSpace(target)
	if !strings.Contains(t, "://") {
		t = "https://" + t
	}
	u, err := url.Parse(t)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", errors.New("target must be the URL of a status page, e.g. https://status.example.com")
	}
	p := u.Path
	if i := strings.Index(p, "/api/v2"); i >= 0 {
		p = p[:i]
	}
	if i := strings.Index(p, "/history"); i >= 0 {
		p = p[:i]
	}
	u.Path = strings.TrimRight(p, "/")
	u.RawPath, u.RawQuery, u.Fragment, u.User = "", "", "", nil
	return u.String(), nil
}

// FetchStatuspage downloads and parses summary.json of a page. It fails
// when the answer does not look like a Statuspage summary.
func FetchStatuspage(ctx context.Context, j Job, timeout time.Duration) (*StatuspageSummary, int, error) {
	base, err := StatuspageBase(j.Target)
	if err != nil {
		return nil, 0, err
	}
	tr := &http.Transport{
		Proxy:               http.ProxyFromEnvironment,
		TLSClientConfig:     &tls.Config{InsecureSkipVerify: j.IgnoreTLS}, //nolint:gosec // user opt-in
		TLSHandshakeTimeout: timeout,
		DisableKeepAlives:   true,
		DialContext:         j.dialContext,
	}
	defer tr.CloseIdleConnections()
	client := &http.Client{Transport: tr}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/v2/summary.json", nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, errors.New(cleanErr(err))
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
		return nil, resp.StatusCode, fmt.Errorf("status page API answered HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, statuspageMaxBody))
	if err != nil {
		return nil, resp.StatusCode, errors.New(cleanErr(err))
	}
	var s StatuspageSummary
	if err := json.Unmarshal(body, &s); err != nil || s.Status.Indicator == "" {
		return nil, resp.StatusCode, errors.New("not an Atlassian Statuspage page (no status in /api/v2/summary.json)")
	}
	return &s, resp.StatusCode, nil
}

func runStatuspage(ctx context.Context, j Job, timeout time.Duration) Result {
	start := time.Now()
	s, code, err := FetchStatuspage(ctx, j, timeout)
	lat := ms(time.Since(start))
	if err != nil {
		return Result{LatencyMs: lat, StatusCode: code, Message: err.Error()}
	}
	r := s.Evaluate(StatuspageIDs(j.Keyword))
	r.LatencyMs, r.StatusCode = lat, code
	return r
}

// Evaluate turns the page state into a result. With component ids it
// looks at those components and their incidents only; otherwise at the
// overall indicator of the page.
func (s *StatuspageSummary) Evaluate(ids []string) Result {
	if len(ids) == 0 {
		return s.evalPage()
	}
	return s.evalComponents(ids)
}

func (s *StatuspageSummary) evalPage() Result {
	r := Result{OK: true}
	switch s.Status.Indicator {
	case "none":
	case "minor":
		r.Degraded = true
	case "major", "critical":
		r.OK = false
	default: // "maintenance" and unknown values
	}
	parts := []string{s.Status.Description}
	if names := incidentNames(s.Incidents, nil); names != "" {
		parts = append(parts, names)
	}
	if names := maintenanceNames(s.Maintenances, nil); names != "" {
		parts = append(parts, names)
	}
	if r.OK && !r.Degraded && len(parts) == 1 {
		return r // nothing to say: all operational
	}
	r.Message = strings.Join(parts, " · ")
	return r
}

func (s *StatuspageSummary) evalComponents(ids []string) Result {
	byID := make(map[string]StatuspageComponent, len(s.Components))
	for _, c := range s.Components {
		byID[c.ID] = c
	}
	r := Result{OK: true}
	var bad, missing []string
	found := 0
	for _, id := range ids {
		c, ok := byID[id]
		if !ok {
			missing = append(missing, id)
			continue
		}
		found++
		switch c.Status {
		case spOperational:
			continue
		case spDegraded, spPartial:
			r.Degraded = true
		case spMajor:
			r.OK = false
		}
		bad = append(bad, c.Name+": "+ComponentStatusText(c.Status))
	}
	if found == 0 {
		return Result{Message: "none of the chosen components is on the page any more: " + strings.Join(missing, ", ")}
	}
	if !r.OK {
		r.Degraded = false
	}
	var parts []string
	if len(bad) > 0 {
		parts = append(parts, strings.Join(bad, ", "))
	}
	if names := incidentNames(s.Incidents, ids); names != "" {
		parts = append(parts, names)
	}
	if names := maintenanceNames(s.Maintenances, ids); names != "" {
		parts = append(parts, names)
	}
	if len(missing) > 0 {
		parts = append(parts, "not on the page any more: "+strings.Join(missing, ", "))
	}
	r.Message = strings.Join(parts, " · ")
	return r
}

// ComponentStatusText gives a component state in words.
func ComponentStatusText(st string) string {
	switch st {
	case spDegraded:
		return "degraded performance"
	case spPartial:
		return "partial outage"
	case spMajor:
		return "major outage"
	case spMaintenance:
		return "under maintenance"
	}
	return strings.ReplaceAll(st, "_", " ")
}

// touches reports whether an incident affects one of the components (all
// of them when ids is empty). An incident without components counts.
func (in statuspageIncident) touches(ids []string) bool {
	if len(ids) == 0 || len(in.Components) == 0 {
		return true
	}
	for _, c := range in.Components {
		if slices.Contains(ids, c.ID) {
			return true
		}
	}
	return false
}

// incidentNames lists the open incidents, e.g. "Incident: Elevated errors
// (identified)".
func incidentNames(list []statuspageIncident, ids []string) string {
	var out []string
	for _, in := range list {
		if in.Status == "resolved" || in.Status == "postmortem" || !in.touches(ids) {
			continue
		}
		out = append(out, fmt.Sprintf("%s (%s)", in.Name, in.Status))
	}
	if len(out) == 0 {
		return ""
	}
	return "Incident: " + strings.Join(out, "; ")
}

// maintenanceNames lists the maintenances in progress.
func maintenanceNames(list []statuspageIncident, ids []string) string {
	var out []string
	for _, in := range list {
		if (in.Status == "in_progress" || in.Status == "verifying") && in.touches(ids) {
			out = append(out, in.Name)
		}
	}
	if len(out) == 0 {
		return ""
	}
	return "Maintenance: " + strings.Join(out, "; ")
}

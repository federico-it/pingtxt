package hub

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/federico-it/pingtxt/internal/check"
	"github.com/federico-it/pingtxt/internal/store"
)

// Automatic diagnostics: the hub asks a location for diagnostics (DNS, TCP,
// TLS, response headers, traceroute) when that location is not failing
// yet, so the first failure carries them. They are stored, linked to the
// incident, summarized in its first alert and shown in the UI. They never
// appear on status pages.

// diagEvery is the shortest time between two diagnostics of the same
// monitor at the same location, so a flapping monitor does not run a
// traceroute every minute.
const diagEvery = 10 * time.Minute

// maxAlertDiags is how many locations a text alert lists.
const maxAlertDiags = 3

// Per-monitor diagnostics switch.
const (
	diagDefault = 0 // follow the instance setting
	diagOn      = 1
	diagOff     = 2
)

// EventDiag is the diagnostics of one location in an alert.
type EventDiag struct {
	Location string      `json:"location"`
	Summary  string      `json:"summary"`
	Diag     *check.Diag `json:"-"` // full data, for the email
}

// diagEnabled reports whether a monitor gets diagnostics.
func diagEnabled(m *store.Monitor, g GeneralSettings) bool {
	switch m.Diagnostics {
	case diagOn:
		return true
	case diagOff:
		return false
	}
	return g.Diagnostics == nil || *g.Diagnostics
}

// wantDiagLocked reports whether the next check of a monitor at a location
// should collect diagnostics if it fails: only when the location is not
// failing already and has not sent diagnostics lately. Call with h.mu held.
func (h *Hub) wantDiagLocked(st *monState, loc string, g GeneralSettings, now time.Time) bool {
	if !diagEnabled(st.m, g) || st.m.Type == check.TypePush {
		return false
	}
	ls := st.locs[loc]
	if ls == nil {
		return true
	}
	return ls.fails == 0 && now.Sub(ls.diagAt) >= diagEvery
}

// storeDiag saves the diagnostics of a result and remembers them for the
// alert of the incident they may start.
func (h *Hub) storeDiag(m *store.Monitor, loc string, r check.Result) {
	data, err := json.Marshal(r.Diag)
	if err != nil {
		return
	}
	d := &store.Diagnostic{MonitorID: m.ID, Location: loc, At: r.Diag.At, Data: data}
	if d.At == 0 {
		d.At = r.At
	}
	if _, err := h.store.InsertDiagnostic(d); err != nil {
		h.log.Error("store diagnostics", "err", err)
		return
	}
	h.mu.Lock()
	if st := h.monitors[m.ID]; st != nil {
		if ls := st.locs[loc]; ls != nil && ls.diag == r.Diag {
			ls.diagID = d.ID
		}
	}
	h.mu.Unlock()
}

// incidentDiagsLocked returns the diagnostics of the failing locations of a
// monitor, with their ids. Call with h.mu held.
func (h *Hub) incidentDiagsLocked(st *monState) ([]EventDiag, []int64) {
	var out []EventDiag
	var ids []int64
	for loc, ls := range st.locs {
		if !ls.down || ls.diag == nil {
			continue
		}
		out = append(out, EventDiag{Location: h.locationNameLocked(loc), Summary: ls.diag.Summary(), Diag: ls.diag})
		if ls.diagID > 0 {
			ids = append(ids, ls.diagID)
		}
	}
	return out, ids
}

// diagURL links the diagnostics of an incident in the hub, or "".
func diagURL(base string, incident int64) string {
	if base == "" || incident <= 0 {
		return ""
	}
	return fmt.Sprintf("%s/incidents/?diag=%d", base, incident)
}

// diagText is the diagnostics part of a text alert.
func (e Event) diagText() string {
	if len(e.Diagnostics) == 0 && e.DiagnosticsURL == "" {
		return ""
	}
	var b strings.Builder
	for i, d := range e.Diagnostics {
		if i == maxAlertDiags {
			fmt.Fprintf(&b, "\n  +%d more locations", len(e.Diagnostics)-maxAlertDiags)
			break
		}
		fmt.Fprintf(&b, "\n  %s: %s", d.Location, d.Summary)
	}
	if e.DiagnosticsURL != "" {
		b.WriteString("\n  diagnostics: " + e.DiagnosticsURL)
	}
	return b.String()
}

// diagMailRows lays out the diagnostics of one location for the email.
func diagMailRows(d *check.Diag) []mailRow {
	if d == nil {
		return nil
	}
	var rows []mailRow
	for _, q := range d.DNS {
		v := strings.Join(q.Answers, ", ")
		tone := ""
		if q.Error != "" {
			v, tone = q.Error, toneDown
		} else if v == "" {
			v = "no records"
		}
		if q.CNAME != "" {
			v += " (CNAME " + q.CNAME + ")"
		}
		key := "dns"
		if q.Resolver != "system" {
			key = "dns @" + q.Resolver
		}
		rows = append(rows, mailRow{Key: key, Value: q.Query + " → " + v, Tone: tone})
	}
	for _, c := range d.TCP {
		if c.OK {
			rows = append(rows, mailRow{Key: "tcp", Value: c.Addr + " open (" + fmtDiagMs(c.Ms) + ")"})
		} else {
			rows = append(rows, mailRow{Key: "tcp", Value: c.Addr + " " + c.Error, Tone: toneDown})
		}
	}
	if t := d.TLS; t != nil {
		v := strings.TrimSpace(t.Version + " " + t.Subject)
		if t.Issuer != "" {
			v += " · issuer " + t.Issuer
		}
		if t.NotAfter > 0 {
			v += " · until " + time.Unix(t.NotAfter, 0).UTC().Format("2006-01-02")
		}
		tone := ""
		if t.Error != "" {
			v, tone = strings.TrimPrefix(v+" · "+t.Error, " · "), toneDown
		}
		rows = append(rows, mailRow{Key: "tls", Value: v, Tone: tone})
	}
	if x := d.HTTP; x != nil {
		v := strings.TrimSpace(x.Proto + " " + strconv.Itoa(x.Status))
		if x.RemoteAddr != "" {
			v += " from " + x.RemoteAddr
		}
		rows = append(rows, mailRow{Key: "http", Value: v})
		for _, hd := range x.Headers {
			switch strings.ToLower(hd.Name) {
			case "server", "content-type", "location", "retry-after", "cf-ray", "x-cache", "via":
				rows = append(rows, mailRow{Key: strings.ToLower(hd.Name), Value: hd.Value})
			}
		}
	}
	if t := d.Trace; t != nil {
		rows = append(rows, mailRow{Key: "traceroute", Value: traceLine(t), Tone: map[bool]string{false: toneWarn, true: ""}[t.Reached || t.Error != ""]})
	}
	if d.Note != "" {
		rows = append(rows, mailRow{Key: "note", Value: d.Note})
	}
	return rows
}

// traceLine sums a traceroute up: the hops, or where it stops.
func traceLine(t *check.DiagTrace) string {
	if t.Error != "" {
		return t.Error
	}
	var hops []string
	for _, h := range t.Hops {
		switch {
		case h.Addr == "":
			hops = append(hops, "*")
		case h.Name != "":
			hops = append(hops, h.Name)
		default:
			hops = append(hops, h.Addr)
		}
	}
	// Long paths: the first and the last hops tell the story.
	if len(hops) > 8 {
		hops = append(append(hops[:3:3], "…"), hops[len(hops)-4:]...)
	}
	s := fmt.Sprintf("%s %d → %s", strings.ToUpper(t.Proto), t.Port, strings.Join(hops, " › "))
	if !t.Reached {
		s += " (destination not reached)"
	}
	return s
}

func fmtDiagMs(v float64) string {
	if v < 10 {
		return strconv.FormatFloat(v, 'f', 1, 64) + " ms"
	}
	return strconv.Itoa(int(v+0.5)) + " ms"
}

// diagnosticView is a stored diagnostic with its location name.
type diagnosticView struct {
	*store.Diagnostic
	LocationName string `json:"location_name"`
}

func (h *Hub) diagViews(list []*store.Diagnostic) []diagnosticView {
	out := make([]diagnosticView, len(list))
	h.mu.Lock()
	defer h.mu.Unlock()
	for i, d := range list {
		out[i] = diagnosticView{Diagnostic: d, LocationName: h.locationNameLocked(d.Location)}
	}
	return out
}

// GET /api/incidents/{id}/diagnostics
func (h *Hub) incidentDiagnostics(w http.ResponseWriter, r *http.Request, _ *store.User) {
	id, ok := pathID(r)
	if !ok {
		writeErr(w, 400, "bad id")
		return
	}
	list, err := h.store.IncidentDiagnostics(id)
	if err != nil {
		h.internalErr(w, err)
		return
	}
	writeJSON(w, 200, h.diagViews(list))
}

// GET /api/monitors/{id}/diagnostics?since=&limit=
func (h *Hub) monitorDiagnostics(w http.ResponseWriter, r *http.Request, _ *store.User) {
	id, ok := pathID(r)
	if !ok {
		writeErr(w, 400, "bad id")
		return
	}
	since, _ := strconv.ParseInt(r.URL.Query().Get("since"), 10, 64)
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	list, err := h.store.MonitorDiagnostics(id, since, limit)
	if err != nil {
		h.internalErr(w, err)
		return
	}
	writeJSON(w, 200, h.diagViews(list))
}

// markDiagnosed sets Diagnostics on the incidents that have some.
func (h *Hub) markDiagnosed(list []*store.Incident) {
	ids := make([]int64, len(list))
	for i, in := range list {
		ids[i] = in.ID
	}
	has, err := h.store.DiagnosedIncidents(ids)
	if err != nil {
		h.log.Error("diagnosed incidents", "err", err)
		return
	}
	for _, in := range list {
		in.Diagnostics = has[in.ID]
	}
}

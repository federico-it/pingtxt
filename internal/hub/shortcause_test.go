package hub

import (
	"testing"

	"github.com/federico-it/pingtxt/internal/store"
)

func TestShortCause(t *testing.T) {
	sp := "Casa · Corridonia, Hostdzire · Francoforte, hub: Claude Cowork: degraded performance, Claude API (api.anthropic.com): partial outage, claude.ai: major outage, Claude Code: partial outage · Incident: Elevated errors on claude.ai, the Claude API, and Claude Code (investigating); Elevated errors affecting the Microsoft 365 connector (identified)"
	for _, c := range []struct {
		cause, typ string
		n          int
		want       string
	}{
		{sp, "statuspage", 7, "claude.ai: major outage +3 · Elevated errors on claude.ai, the Claude API, and Claude Code +1"},
		{"a, b: Partial System Outage · Incident: X (monitoring)", "statuspage", 2, "Partial System Outage · X"},
		{"a, b: Claude API: partial outage · Maintenance: DB upgrade; Other", "statuspage", 2, "Claude API: partial outage · Maintenance: DB upgrade +1"},
		{"a, b: context deadline exceeded", "statuspage", 2, "context deadline exceeded"},
		{"a, b: HTTP 503", "http", 2, "HTTP 503"},
		{"a: HTTP 503", "http", 2, "a: HTTP 503"},
		{"a: HTTP 503; b: timeout", "http", 2, "a: HTTP 503; b: timeout"},
		{"HTTP 503", "http", 2, "HTTP 503"},
		{"", "http", 2, ""},
	} {
		if got := shortCause(c.cause, c.typ, c.n); got != c.want {
			t.Errorf("shortCause(%q, %s, %d)\n got %q\nwant %q", c.cause, c.typ, c.n, got, c.want)
		}
	}
}

func TestLiveActivityStatuspage(t *testing.T) {
	open := []*store.Incident{
		{MonitorID: 1, MonitorName: "Claude", Cause: "a, b: claude.ai: major outage, Claude Code: partial outage"},
		{MonitorID: 2, MonitorName: "api", Cause: "a, b: HTTP 503"},
	}
	mons := map[int64]*store.Monitor{
		1: {ID: 1, Type: "statuspage", Locations: []string{"a", "b", "c"}},
		2: {ID: 2, Type: "http", Locations: []string{"a", "b"}},
	}
	st := buildLAState(open, mons)
	if o := st.Outages[0]; o.Cause != "claude.ai: major outage +1" || !o.External {
		t.Fatalf("%+v", o)
	}
	if o := st.Outages[1]; o.Cause != "HTTP 503" || o.External {
		t.Fatalf("%+v", o)
	}
	if _, body := laAlert(open[:1], mons); body != "claude.ai: major outage +1" {
		t.Fatalf("%q", body)
	}
	m := appMessage(Event{MonitorID: 1, Monitor: "Claude", Target: "https://status.claude.com", Status: StatusDown, Cause: open[0].Cause,
		ShortCause: monitorShortCause(open[0].Cause, mons[1])})
	if m.Body != "https://status.claude.com\nclaude.ai: major outage +1" {
		t.Fatalf("%q", m.Body)
	}
}

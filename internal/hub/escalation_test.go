package hub

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/federico-it/pingtxt/internal/store"
)

// alertSink is a webhook that records the events each channel receives.
type alertSink struct {
	mu  sync.Mutex
	got map[string][]Event // by channel name (?ch=)
	srv *httptest.Server
}

func newAlertSink(t *testing.T) *alertSink {
	s := &alertSink{got: map[string][]Event{}}
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var e Event
		_ = json.NewDecoder(r.Body).Decode(&e)
		s.mu.Lock()
		s.got[r.URL.Query().Get("ch")] = append(s.got[r.URL.Query().Get("ch")], e)
		s.mu.Unlock()
	}))
	t.Cleanup(s.srv.Close)
	return s
}

// wait returns what each channel received once total events have arrived.
func (s *alertSink) wait(t *testing.T, total int) map[string][]Event {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		s.mu.Lock()
		n := 0
		for _, l := range s.got {
			n += len(l)
		}
		if n >= total {
			out := map[string][]Event{}
			for k, l := range s.got {
				out[k] = append([]Event(nil), l...)
			}
			s.mu.Unlock()
			// Nothing more must arrive.
			time.Sleep(100 * time.Millisecond)
			s.mu.Lock()
			m := 0
			for _, l := range s.got {
				m += len(l)
			}
			s.mu.Unlock()
			if m != n {
				t.Fatalf("want %d alerts, got %d", n, m)
			}
			return out
		}
		s.mu.Unlock()
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %d alerts, got %d", total, n)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func (s *alertSink) channel(t *testing.T, st *store.Store, name string, delay, repeat int) {
	t.Helper()
	n := &store.Notifier{Name: name, Type: "webhook", Enabled: true, DelayMin: delay, RepeatMin: repeat,
		Config: map[string]string{"url": s.srv.URL + "/?ch=" + name}}
	if err := validateNotifier(n); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateNotifier(n); err != nil {
		t.Fatal(err)
	}
}

func summary(got map[string][]Event) string {
	var out []string
	for ch, l := range got {
		for _, e := range l {
			s := ch + ":" + e.Status
			if e.Reminder > 0 {
				s += "#" + strings.Repeat("r", e.Reminder)
			}
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return strings.Join(out, " ")
}

func TestEscalationAndReminders(t *testing.T) {
	h, st := newTestHub(t)
	sink := newAlertSink(t)
	sink.channel(t, st, "now", 0, 0)
	sink.channel(t, st, "boss", 10, 0)   // escalation after 10 minutes
	sink.channel(t, st, "nag", 0, 5)     // reminder every 5 minutes
	sink.channel(t, st, "later", 60, 15) // never reached in this test
	m := &store.Monitor{Name: "api", Type: "http", Target: "http://x", IntervalSec: 60, Retries: 1, Locations: []string{LocalLocation}}
	if err := st.CreateMonitor(m); err != nil {
		t.Fatal(err)
	}
	h.MonitorChanged(m.ID)

	start := time.Now().Add(-11 * time.Minute)
	r := result(m.ID, false)
	r.At = start.UnixMilli()
	h.HandleResult(LocalLocation, r)
	if got := summary(sink.wait(t, 2)); got != "nag:down now:down" {
		t.Fatalf("first alerts: %s", got)
	}

	// 11 minutes later: the escalation contact hears about it, nag gets a reminder.
	h.escalate(time.Now())
	got := sink.wait(t, 4)
	if s := summary(got); s != "boss:down nag:down nag:down#r now:down" {
		t.Fatalf("after 11 minutes: %s", s)
	}
	if e := got["boss"][0]; e.DownSince != r.At || !strings.Contains(e.Text(), "down for 11m") {
		t.Fatalf("escalated alert: %+v %q", e, e.Text())
	}
	if e := got["nag"][1]; e.Title() != "pingtxt: api STILL DOWN" || !strings.HasPrefix(e.Text(), "[STILL DOWN] api") {
		t.Fatalf("reminder: %q %q", e.Title(), e.Text())
	}
	h.escalate(time.Now()) // nothing new yet
	sink.wait(t, 4)

	// A restart keeps track of what was sent.
	h2 := New(Config{LocalName: "hub"}, st, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := h2.reloadAll(); err != nil {
		t.Fatal(err)
	}
	h2.escalate(time.Now())
	sink.wait(t, 4)
	h2.escalate(time.Now().Add(5 * time.Minute))
	if s := summary(sink.wait(t, 5)); s != "boss:down nag:down nag:down#r nag:down#rr now:down" {
		t.Fatalf("second reminder: %s", s)
	}

	// Maintenance holds reminders back.
	w := &store.Maintenance{Name: "all", Kind: store.MaintDaily, AllMonitors: true, StartTime: "00:00", DurationMin: 24 * 60, Timezone: "UTC", Enabled: true}
	if err := st.CreateMaintenance(w); err != nil {
		t.Fatal(err)
	}
	h2.afterMaintChange()
	h2.escalate(time.Now().Add(20 * time.Minute))
	sink.wait(t, 5)
	if err := st.DeleteMaintenance(w.ID); err != nil {
		t.Fatal(err)
	}
	h2.afterMaintChange()

	// Recovery goes to the channels that were alerted, not to "later".
	h2.HandleResult(LocalLocation, result(m.ID, true))
	got = sink.wait(t, 8)
	if s := summary(got); s != "boss:down boss:up nag:down nag:down#r nag:down#rr nag:up now:down now:up" {
		t.Fatalf("recovery: %s", s)
	}
	if e := got["now"][1]; e.DownSince != r.At || !strings.Contains(e.Text(), "after 11m down") {
		t.Fatalf("recovery event: %+v %q", e, e.Text())
	}
}

func TestEscalationValidation(t *testing.T) {
	base := func(delay, repeat int) *store.Notifier {
		return &store.Notifier{Name: "x", Type: "webhook", DelayMin: delay, RepeatMin: repeat, Config: map[string]string{"url": "https://example.com"}}
	}
	for _, c := range []struct {
		delay, repeat int
		ok            bool
	}{{0, 0, true}, {15, 30, true}, {-1, 0, false}, {0, 1, false}, {0, 5, true}, {maxAlertMin + 1, 0, false}, {0, maxAlertMin + 1, false}} {
		if err := validateNotifier(base(c.delay, c.repeat)); (err == nil) != c.ok {
			t.Errorf("delay %d repeat %d: err %v", c.delay, c.repeat, err)
		}
	}
}

func TestFmtDuration(t *testing.T) {
	for d, want := range map[time.Duration]string{
		45 * time.Minute: "45m", 2 * time.Hour: "2h", 125 * time.Minute: "2h 5m", 76 * time.Hour: "3d 4h", 48 * time.Hour: "2d",
	} {
		if got := fmtDuration(d); got != want {
			t.Errorf("%v: got %s want %s", d, got, want)
		}
	}
}

package hub

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/federico-it/pingtxt/internal/store"
)

func TestExpiryAlert(t *testing.T) {
	th := []int{30, 14, 7, 1}
	steps := []struct {
		days      int
		wantEvent string
		wantLevel int
	}{
		{90, "", -1},
		{31, "", -1},
		{30, ExpiryExpiring, 30},
		{20, "", 30},
		{14, ExpiryExpiring, 14},
		{13, "", 14},
		{3, ExpiryExpiring, 7}, // missed a pass: one alert, at the lowest crossed threshold
		{1, ExpiryExpiring, 1},
		{0, "", 1},
		{-1, ExpiryExpired, 0},
		{-5, "", 0},
		{365, ExpiryRenewed, -1},
		{360, "", -1},
	}
	level := -1
	for _, s := range steps {
		ev, l := expiryAlert(th, s.days, level)
		if ev != s.wantEvent || l != s.wantLevel {
			t.Fatalf("days=%d alerted=%d: got (%q,%d), want (%q,%d)", s.days, level, ev, l, s.wantEvent, s.wantLevel)
		}
		level = l
	}
	// Renewed, but the new date is still within a threshold: no news, lower urgency.
	if ev, l := expiryAlert(th, 20, 7); ev != "" || l != 30 {
		t.Fatalf("partial renewal: got (%q,%d)", ev, l)
	}
	// Alerts turned off.
	if ev, l := expiryAlert(nil, -3, 7); ev != "" || l != -1 {
		t.Fatalf("off: got (%q,%d)", ev, l)
	}
}

func TestDaysLeft(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	cases := map[time.Duration]int{
		10*24*time.Hour + time.Hour: 10,
		23 * time.Hour:              0,
		-time.Hour:                  -1,
		-49 * time.Hour:             -3,
	}
	for d, want := range cases {
		if got := daysLeft(now.Add(d).UnixMilli(), now); got != want {
			t.Errorf("%v: got %d, want %d", d, got, want)
		}
	}
}

func TestExpiryTargets(t *testing.T) {
	on := func(typ, target string) *store.Monitor {
		return &store.Monitor{Type: typ, Target: target, CertExpiryAlert: true, DomainExpiryAlert: true}
	}
	kinds := func(m *store.Monitor) string {
		var out []string
		for _, t := range expiryTargets(m) {
			out = append(out, t.kind+"="+t.subject)
		}
		return strings.Join(out, ",")
	}
	cases := []struct {
		m    *store.Monitor
		want string
	}{
		{on("http", "https://api.example.co.uk/health"), "cert=api.example.co.uk,domain=example.co.uk"},
		{on("keyword", "https://example.com:8443/"), "cert=example.com:8443,domain=example.com"},
		{on("http", "http://example.com/"), "domain=example.com"},
		{on("tcp", "db.example.it:5432"), "domain=example.it"},
		{on("ping", "1.1.1.1"), ""},
		{on("http", "https://10.0.0.1/"), "cert=10.0.0.1"},
		{&store.Monitor{Type: "http", Target: "https://example.com/"}, ""},
	}
	for _, c := range cases {
		if got := kinds(c.m); got != c.want {
			t.Errorf("%s %s: got %q, want %q", c.m.Type, c.m.Target, got, c.want)
		}
	}
}

// tlsServerExpiringIn starts an HTTPS server whose certificate expires after d.
func tlsServerExpiringIn(t *testing.T, d time.Duration) *httptest.Server {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "127.0.0.1"},
		Issuer:       pkix.Name{CommonName: "test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(d),
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	srv.TLS = &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}}
	srv.StartTLS()
	t.Cleanup(srv.Close)
	return srv
}

func TestCertExpiryNotifiesOnce(t *testing.T) {
	h, st := newTestHub(t)

	var mu sync.Mutex
	var got []Event
	hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var e Event
		_ = json.NewDecoder(r.Body).Decode(&e)
		mu.Lock()
		got = append(got, e)
		mu.Unlock()
	}))
	defer hook.Close()
	if err := st.CreateNotifier(&store.Notifier{Name: "hook", Type: "webhook", Config: map[string]string{"url": hook.URL}, Enabled: true, AllMonitors: true, Events: store.DefaultNotifyEvents}); err != nil {
		t.Fatal(err)
	}

	srv := tlsServerExpiringIn(t, 5*24*time.Hour+time.Hour)
	m := &store.Monitor{Name: "site", Type: "http", Target: srv.URL, IntervalSec: 60, Retries: 1, CertExpiryAlert: true, DomainExpiryAlert: true}
	if err := st.CreateMonitor(m); err != nil {
		t.Fatal(err)
	}
	h.CheckExpiryNow(context.Background(), m.ID)
	h.CheckExpiryNow(context.Background(), m.ID) // same level: no second alert

	checks, err := st.ExpiryChecks(m.ID)
	if err != nil || len(checks) != 1 {
		t.Fatalf("checks: %v %+v", err, checks)
	}
	c := checks[0]
	if c.Kind != store.ExpiryCert || c.Source != "tls" || c.ExpiresAt == 0 || c.AlertedDays != 7 {
		t.Fatalf("check: %+v", c)
	}
	if c.Error == "" {
		t.Fatal("self-signed certificate should be reported as invalid")
	}

	deadline := time.Now().Add(3 * time.Second)
	for {
		mu.Lock()
		n := len(got)
		mu.Unlock()
		if n >= 1 || time.Now().After(deadline) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	time.Sleep(100 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if len(got) != 1 {
		t.Fatalf("want 1 notification, got %d: %+v", len(got), got)
	}
	e := got[0]
	if e.Kind != KindCert || e.Status != ExpiryExpiring || e.DaysLeft == nil || *e.DaysLeft != 5 {
		t.Fatalf("event: %+v", e)
	}
	if !strings.Contains(e.Text(), "expires in 5 days") {
		t.Fatalf("text: %s", e.Text())
	}

	// Turning the alert off removes the check.
	m.CertExpiryAlert = false
	if err := st.UpdateMonitor(m); err != nil {
		t.Fatal(err)
	}
	h.CheckExpiryNow(context.Background(), m.ID)
	if checks, _ := st.ExpiryChecks(m.ID); len(checks) != 0 {
		t.Fatalf("check should be removed, got %+v", checks)
	}
}

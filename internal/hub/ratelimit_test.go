package hub

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"testing/fstest"
	"time"

	"github.com/federico-it/pingtxt/internal/store"
)

func TestFailLimiter(t *testing.T) {
	l := newFailLimiter(3)
	now := time.Now()
	for i := range 3 {
		if _, b := l.blocked("k", now); b {
			t.Fatalf("blocked after %d failures", i)
		}
		l.fail("k", now.Add(time.Duration(i)*time.Minute))
	}
	wait, b := l.blocked("k", now.Add(3*time.Minute))
	if !b || wait != loginWindow-3*time.Minute {
		t.Fatalf("want blocked for 12m, got %v %v", b, wait)
	}
	if _, b := l.blocked("other", now); b {
		t.Fatal("keys must be independent")
	}
	// The oldest failure leaves the window: one more attempt is allowed.
	if _, b := l.blocked("k", now.Add(loginWindow)); b {
		t.Fatal("still blocked after the window")
	}
}

func TestLoginRateLimit(t *testing.T) {
	h, _ := newTestHub(t)
	srv := httptest.NewServer(h.Handler(fstest.MapFS{}))
	defer srv.Close()
	c := newClient(t, srv.URL)
	c.do("POST", "/api/setup", map[string]string{"email": "a@b.co", "password": "password123"})

	v := newClient(t, srv.URL)
	for i := range loginFailsAccount {
		if res := v.do("POST", "/api/auth/login", map[string]string{"email": "A@b.co", "password": "wrong-" + strconv.Itoa(i)}); res.StatusCode != 401 {
			t.Fatalf("attempt %d: %d", i, res.StatusCode)
		}
	}
	// Even the right password is refused now, for this account only.
	res := v.do("POST", "/api/auth/login", map[string]string{"email": "a@b.co", "password": "password123"})
	if res.StatusCode != http.StatusTooManyRequests || res.Header.Get("Retry-After") == "" {
		t.Fatalf("want 429 with Retry-After, got %d", res.StatusCode)
	}
	if res := v.do("POST", "/api/auth/login", map[string]string{"email": "c@b.co", "password": "x"}); res.StatusCode != 401 {
		t.Fatalf("other account: %d", res.StatusCode)
	}
}

func TestSecondFactorRateLimit(t *testing.T) {
	h, _ := newTestHub(t)
	srv := httptest.NewServer(h.Handler(fstest.MapFS{}))
	defer srv.Close()
	c := newClient(t, srv.URL)
	c.do("POST", "/api/setup", map[string]string{"email": "a@b.co", "password": "password123"})
	setup := decode[map[string]string](t, c.do("POST", "/api/auth/2fa/setup", nil))
	if res := c.do("POST", "/api/auth/2fa/enable", map[string]string{"code": currentCode(t, setup["secret"], 0)}); res.StatusCode != 200 {
		t.Fatalf("enable: %d", res.StatusCode)
	}

	// New challenges do not reset the count of wrong codes.
	v := newClient(t, srv.URL)
	wrong := 0
	for wrong < codeFailsUser {
		ch := decode[map[string]any](t, v.do("POST", "/api/auth/login", map[string]string{"email": "a@b.co", "password": "password123"}))["challenge"].(string)
		for range 2 {
			if res := v.do("POST", "/api/auth/login/2fa", map[string]string{"challenge": ch, "code": "999999"}); res.StatusCode != 401 {
				t.Fatalf("wrong code %d: %d", wrong, res.StatusCode)
			}
			wrong++
		}
	}
	ch := decode[map[string]any](t, v.do("POST", "/api/auth/login", map[string]string{"email": "a@b.co", "password": "password123"}))["challenge"].(string)
	if res := v.do("POST", "/api/auth/login/2fa", map[string]string{"challenge": ch, "code": currentCode(t, setup["secret"], 1)}); res.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("want 429, got %d", res.StatusCode)
	}
}

// A stored SMTP password is only reused for the same server: changing the
// host (or port, user, security) of a draft or an update needs the password again.
func TestNotifierSecretBoundToServer(t *testing.T) {
	h, _ := newTestHub(t)
	n := &store.Notifier{Name: "mail", Type: "email", Enabled: true, Config: map[string]string{
		"host": "smtp.example.com", "port": "587", "security": "starttls", "username": "u", "password": "secret-pass",
		"from": "a@example.com", "to": "b@example.com",
	}}
	if err := h.store.CreateNotifier(n); err != nil {
		t.Fatal(err)
	}
	draft := func(host, security string) *store.Notifier {
		d := &store.Notifier{ID: n.ID, Name: "mail", Type: "email", Config: map[string]string{
			"host": host, "port": "587", "security": security, "username": "u", "password": "",
			"from": "a@example.com", "to": "b@example.com",
		}}
		if err := validateNotifier(d); err != nil {
			t.Fatal(err)
		}
		h.keepSecrets(d)
		return d
	}
	if d := draft("smtp.example.com", "starttls"); d.Config["password"] != "secret-pass" {
		t.Fatal("same server: the stored password must be kept")
	}
	if d := draft("evil.example.net", "starttls"); d.Config["password"] != "" {
		t.Fatal("other host: the stored password must not be reused")
	}
	if d := draft("smtp.example.com", "none"); d.Config["password"] != "" {
		t.Fatal("plain text: the stored password must not be reused")
	}
}

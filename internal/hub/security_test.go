package hub

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/federico-it/pingtxt/internal/store"
	"time"
)

func TestTOTPVectors(t *testing.T) {
	// RFC 6238 SHA-1 test vectors, truncated to 6 digits.
	key := []byte("12345678901234567890")
	for ts, want := range map[int64]string{59: "287082", 1111111109: "081804", 1234567890: "005924", 2000000000: "279037"} {
		if got := totpCode(key, ts/totpPeriod); got != want {
			t.Errorf("t=%d: got %s want %s", ts, got, want)
		}
	}
	secret := b32.EncodeToString(key)
	now := time.Unix(1111111109, 0)
	if _, ok := totpMatch(secret, "081804", now); !ok {
		t.Fatal("current code rejected")
	}
	if _, ok := totpMatch(secret, "081804", now.Add(20*time.Second)); !ok {
		t.Fatal("previous step should be accepted for clock drift")
	}
	if _, ok := totpMatch(secret, "081804", now.Add(5*time.Minute)); ok {
		t.Fatal("old code accepted")
	}
}

func decode[T any](t *testing.T, res *http.Response) T {
	t.Helper()
	var v T
	if err := json.NewDecoder(res.Body).Decode(&v); err != nil {
		t.Fatal(err)
	}
	return v
}

func currentCode(t *testing.T, secret string, offset int64) string {
	t.Helper()
	key, err := b32.DecodeString(secret)
	if err != nil {
		t.Fatal(err)
	}
	return totpCode(key, time.Now().Unix()/totpPeriod+offset)
}

func TestTwoFactorLogin(t *testing.T) {
	h, _ := newTestHub(t)
	srv := httptest.NewServer(h.Handler(fstest.MapFS{}))
	defer srv.Close()
	c := newClient(t, srv.URL)
	c.do("POST", "/api/setup", map[string]string{"email": "a@b.co", "password": "password123"})

	setup := decode[map[string]string](t, c.do("POST", "/api/auth/2fa/setup", nil))
	if setup["secret"] == "" || !strings.HasPrefix(setup["uri"], "otpauth://totp/") {
		t.Fatalf("setup: %+v", setup)
	}
	if res := c.do("POST", "/api/auth/2fa/enable", map[string]string{"code": "000000"}); res.StatusCode != 400 {
		t.Fatalf("wrong code accepted: %d", res.StatusCode)
	}
	res := c.do("POST", "/api/auth/2fa/enable", map[string]string{"code": currentCode(t, setup["secret"], -1)})
	if res.StatusCode != 200 {
		t.Fatalf("enable: %d", res.StatusCode)
	}
	codes := decode[map[string][]string](t, res)["recovery_codes"]
	if len(codes) != recoveryCodeCount {
		t.Fatalf("recovery codes: %v", codes)
	}

	// The password alone no longer starts a session.
	v := newClient(t, srv.URL)
	login := decode[map[string]any](t, v.do("POST", "/api/auth/login", map[string]string{"email": "a@b.co", "password": "password123"}))
	ch, _ := login["challenge"].(string)
	if login["two_factor"] != true || ch == "" {
		t.Fatalf("login: %+v", login)
	}
	if res := v.do("GET", "/api/auth/me", nil); res.StatusCode != 401 {
		t.Fatalf("session started before second factor: %d", res.StatusCode)
	}
	// A code already used (the one from enable) is refused: no replay.
	if res := v.do("POST", "/api/auth/login/2fa", map[string]string{"challenge": ch, "code": currentCode(t, setup["secret"], -1)}); res.StatusCode != 401 {
		t.Fatalf("replayed code accepted: %d", res.StatusCode)
	}
	if res := v.do("POST", "/api/auth/login/2fa", map[string]string{"challenge": ch, "code": codes[0]}); res.StatusCode != 200 {
		t.Fatalf("recovery code login: %d", res.StatusCode)
	}
	if res := v.do("GET", "/api/auth/me", nil); res.StatusCode != 200 {
		t.Fatalf("me after 2fa: %d", res.StatusCode)
	}
	// Challenges are single use, recovery codes too.
	if res := v.do("POST", "/api/auth/login/2fa", map[string]string{"challenge": ch, "code": codes[1]}); res.StatusCode != http.StatusGone {
		t.Fatalf("challenge reused: %d", res.StatusCode)
	}
	v2 := newClient(t, srv.URL)
	ch2 := decode[map[string]any](t, v2.do("POST", "/api/auth/login", map[string]string{"email": "a@b.co", "password": "password123"}))["challenge"].(string)
	if res := v2.do("POST", "/api/auth/login/2fa", map[string]string{"challenge": ch2, "code": strings.ToUpper(codes[0])}); res.StatusCode != 401 {
		t.Fatalf("used recovery code accepted: %d", res.StatusCode)
	}
	// Too many attempts burn the challenge.
	for range challengeAttempts {
		v2.do("POST", "/api/auth/login/2fa", map[string]string{"challenge": ch2, "code": "111111"})
	}
	if res := v2.do("POST", "/api/auth/login/2fa", map[string]string{"challenge": ch2, "code": codes[2]}); res.StatusCode != http.StatusGone {
		t.Fatalf("challenge survived brute force: %d", res.StatusCode)
	}

	me := decode[map[string]any](t, v.do("GET", "/api/auth/me", nil))
	if me["recovery_codes_left"].(float64) != recoveryCodeCount-1 || me["user"].(map[string]any)["two_factor"] != true {
		t.Fatalf("me: %+v", me)
	}

	// Turning it off needs a valid code.
	if res := v.do("POST", "/api/auth/2fa/disable", map[string]string{"code": "123"}); res.StatusCode != 400 {
		t.Fatalf("disable without code: %d", res.StatusCode)
	}
	if res := v.do("POST", "/api/auth/2fa/disable", map[string]string{"code": codes[3]}); res.StatusCode != 200 {
		t.Fatalf("disable: %d", res.StatusCode)
	}
	if login := decode[map[string]any](t, newClient(t, srv.URL).do("POST", "/api/auth/login", map[string]string{"email": "a@b.co", "password": "password123"})); login["two_factor"] != nil {
		t.Fatalf("2fa still asked after disable: %+v", login)
	}
}

func TestResetTwoFactor(t *testing.T) {
	h, st := newTestHub(t)
	u, _ := st.CreateUser("a@b.co", "x", store.RoleAdmin)
	st.SetPendingTOTP(u.ID, newTOTPSecret())
	st.EnableTOTP(u.ID, 1, nil)
	if !st.TwoFactorEnabled(u.ID) {
		t.Fatal("not enabled")
	}
	if err := h.ResetTwoFactor("A@B.co"); err != nil || st.TwoFactorEnabled(u.ID) {
		t.Fatalf("reset failed: %v", err)
	}
}

func bearerDo(t *testing.T, base, token, method, path string, body any) *http.Response {
	t.Helper()
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest(method, base+path, strings.NewReader(string(b)))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func TestAPITokens(t *testing.T) {
	h, st := newTestHub(t)
	srv := httptest.NewServer(h.Handler(fstest.MapFS{}))
	defer srv.Close()
	c := newClient(t, srv.URL)
	c.do("POST", "/api/setup", map[string]string{"email": "a@b.co", "password": "password123"})

	if res := c.do("POST", "/api/tokens", map[string]any{"name": "ci", "scope": "admin"}); res.StatusCode != 400 {
		t.Fatalf("bad scope accepted: %d", res.StatusCode)
	}
	type created struct {
		Secret string `json:"secret"`
		Token  struct {
			ID   int64  `json:"id"`
			Hint string `json:"hint"`
		} `json:"token"`
	}
	ro := decode[created](t, c.do("POST", "/api/tokens", map[string]any{"name": "grafana", "scope": "read"}))
	rw := decode[created](t, c.do("POST", "/api/tokens", map[string]any{"name": "terraform", "scope": "write", "expires_in_days": 30}))
	if !strings.HasPrefix(ro.Secret, "ptk_") || !strings.HasSuffix(ro.Secret, ro.Token.Hint) {
		t.Fatalf("token: %+v", ro)
	}

	monitor := map[string]any{"name": "x", "type": "http", "target": "https://example.com"}
	if res := bearerDo(t, srv.URL, ro.Secret, "GET", "/api/monitors", nil); res.StatusCode != 200 {
		t.Fatalf("read token GET: %d", res.StatusCode)
	}
	if res := bearerDo(t, srv.URL, ro.Secret, "POST", "/api/monitors", monitor); res.StatusCode != 403 {
		t.Fatalf("read token POST: %d", res.StatusCode)
	}
	if res := bearerDo(t, srv.URL, rw.Secret, "POST", "/api/monitors", monitor); res.StatusCode != 201 {
		t.Fatalf("write token POST: %d", res.StatusCode)
	}
	// Tokens never reach account endpoints.
	for _, p := range [][2]string{{"GET", "/api/tokens"}, {"POST", "/api/tokens"}, {"GET", "/api/settings"}, {"POST", "/api/auth/2fa/setup"}, {"GET", "/api/users"}} {
		if res := bearerDo(t, srv.URL, rw.Secret, p[0], p[1], map[string]any{"name": "x", "scope": "write"}); res.StatusCode != 403 {
			t.Fatalf("%s %s with token: %d", p[0], p[1], res.StatusCode)
		}
	}
	if res := bearerDo(t, srv.URL, "ptk_nope", "GET", "/api/monitors", nil); res.StatusCode != 401 {
		t.Fatalf("unknown token: %d", res.StatusCode)
	}

	list := decode[[]map[string]any](t, c.do("GET", "/api/tokens", nil))
	if len(list) != 2 || list[0]["expires_at"].(float64) == 0 || list[1]["last_used_at"].(float64) == 0 {
		t.Fatalf("list: %+v", list)
	}
	for _, tok := range list {
		if _, leaked := tok["token_hash"]; leaked {
			t.Fatal("hash exposed")
		}
	}

	// Expired and revoked tokens stop working.
	tok, _ := st.APITokenByHash(HashToken(rw.Secret))
	tok.ExpiresAt = time.Now().Add(-time.Second).UnixMilli()
	st.DeleteAPIToken(tok.UserID, tok.ID)
	st.CreateAPIToken(tok)
	if res := bearerDo(t, srv.URL, rw.Secret, "GET", "/api/monitors", nil); res.StatusCode != 401 {
		t.Fatalf("expired token: %d", res.StatusCode)
	}
	if res := c.do("DELETE", "/api/tokens/"+strconv.FormatInt(ro.Token.ID, 10), nil); res.StatusCode != 200 {
		t.Fatalf("revoke: %d", res.StatusCode)
	}
	if res := bearerDo(t, srv.URL, ro.Secret, "GET", "/api/monitors", nil); res.StatusCode != 401 {
		t.Fatalf("revoked token: %d", res.StatusCode)
	}
}

package hub

import (
	"testing"

	"github.com/federico-it/pingtxt/internal/check"
	"github.com/federico-it/pingtxt/internal/store"
)

func TestValidateNewMonitorTypes(t *testing.T) {
	h, _ := newTestHub(t)
	cases := []struct {
		typ, target string
		ok          bool
	}{
		{check.TypePostgres, "postgres://u:p@db:5432/app", true},
		{check.TypePostgres, "postgresql://db/app", true},
		{check.TypePostgres, "db:5432", false},
		{check.TypeMySQL, "mysql://root:pw@db/app", true},
		{check.TypeMySQL, "postgres://db", false},
		{check.TypeRedis, "rediss://:pw@cache:6380/1", true},
		{check.TypeRedis, "cache:6379", false},
		{check.TypeMongo, "mongodb+srv://u:p@cluster.example.net/app", true},
		{check.TypeMongo, "mongodb://a:27017,b:27017/?replicaSet=rs0", true},
		{check.TypeDocker, "my-app_web.1", true},
		{check.TypeDocker, "/nginx", true},
		{check.TypeDocker, "nginx; rm", false},
		{check.TypeGRPC, "grpc.example.com:443", true},
		{check.TypeGRPC, "grpcs://api:443/my.Service", true},
		{check.TypeGRPC, "grpc://api", false},
		{check.TypeSMTP, "mx.example.com", true},
		{check.TypeSMTP, "smtps://mx.example.com:465", true},
		{check.TypeSMTP, "http://mx.example.com", false},
		{check.TypeUDP, "ntp.example.com:123", true},
		{check.TypeUDP, "[2001:db8::1]:53", true},
		{check.TypeUDP, "ntp.example.com", false},
		{check.TypeWS, "wss://example.com/socket", true},
		{check.TypeWS, "ws://[2001:db8::1]:8080/", true},
		{check.TypeWS, "https://example.com/socket", false},
	}
	for _, c := range cases {
		m := &store.Monitor{Name: "x", Type: c.typ, Target: c.target}
		err := h.validateMonitor(m)
		if (err == nil) != c.ok {
			t.Errorf("%s %q: err = %v, want ok=%v", c.typ, c.target, err, c.ok)
		}
	}
}

func TestValidateMCP(t *testing.T) {
	h, _ := newTestHub(t)
	cases := []struct {
		target, method, keyword, min string
		ok                           bool
		wantMethod, wantKW, wantMin  string
	}{
		{"https://mcp.example.com/mcp", "", "", "", true, "auto", "", ""},
		{"http://localhost:3000/sse", "SSE", " a ,, b ", "0", true, "sse", "a, b", ""},
		{"https://x/mcp", "streamable", "", "5", true, "streamable", "", "5"},
		{"mcp.example.com", "", "", "", false, "", "", ""},
		{"https://x/mcp", "websocket", "", "", false, "", "", ""},
		{"https://x/mcp", "", "", "-1", false, "", "", ""},
		{"https://x/mcp", "", "", "many", false, "", "", ""},
	}
	for _, c := range cases {
		m := &store.Monitor{Name: "x", Type: check.TypeMCP, Target: c.target, Method: c.method, Keyword: c.keyword, ExpectedStatus: c.min}
		err := h.validateMonitor(m)
		if (err == nil) != c.ok {
			t.Errorf("%q: err = %v, want ok=%v", c.target, err, c.ok)
			continue
		}
		if c.ok && (m.Method != c.wantMethod || m.Keyword != c.wantKW || m.ExpectedStatus != c.wantMin) {
			t.Errorf("%q: got %q %q %q", c.target, m.Method, m.Keyword, m.ExpectedStatus)
		}
	}
}

func TestValidateUDP(t *testing.T) {
	h, _ := newTestHub(t)
	m := &store.Monitor{Name: "x", Type: check.TypeUDP, Target: "h:53", Method: "weird", Body: "ping"}
	if err := h.validateMonitor(m); err != nil || m.Method != check.UDPText {
		t.Fatalf("%v %q", err, m.Method)
	}
	m = &store.Monitor{Name: "x", Type: check.TypeUDP, Target: "h:53", Method: check.UDPHex, Body: "0x0g"}
	if err := h.validateMonitor(m); err == nil {
		t.Fatal("bad hex accepted")
	}
}

func TestValidateSteps(t *testing.T) {
	h, _ := newTestHub(t)
	login := check.Step{Method: "post", URL: "https://example.com/login", Extract: []check.Extract{{Var: "token", From: check.FromJSON, Path: "token"}}}
	m := &store.Monitor{Name: "x", Type: check.TypeHTTP, Target: "https://{{host}}/me?t={{token}}", Steps: []check.Step{login}}
	if err := h.validateMonitor(m); err != nil || m.Steps[0].Method != "POST" {
		t.Fatalf("%v %+v", err, m.Steps)
	}
	bad := login
	bad.URL = "example.com/login"
	m.Steps = []check.Step{login, bad}
	if err := h.validateMonitor(m); err == nil || err.Error() != "step 2: url must be an http(s) URL" {
		t.Fatalf("bad step: %v", err)
	}
	m.Steps = make([]check.Step, check.MaxSteps+1)
	if err := h.validateMonitor(m); err == nil {
		t.Fatal("too many steps accepted")
	}
	// Other types drop them.
	m = &store.Monitor{Name: "x", Type: check.TypeTCP, Target: "h:1", Steps: []check.Step{login}}
	if err := h.validateMonitor(m); err != nil || len(m.Steps) != 0 {
		t.Fatalf("%v %+v", err, m.Steps)
	}
}

func TestOldAgentError(t *testing.T) {
	steps := &store.Monitor{Type: check.TypeHTTP, Steps: []check.Step{{URL: "https://x/login"}}}
	if got := oldAgentError(steps, "agent:1", check.Result{OK: true}); got != "this agent cannot run request steps: update it" {
		t.Errorf("steps: %q", got)
	}
	if got := oldAgentError(steps, "agent:1", check.Result{OK: false, Steps: 1}); got != "" {
		t.Errorf("new agent: %q", got)
	}
	if got := oldAgentError(steps, LocalLocation, check.Result{OK: true}); got != "" {
		t.Errorf("local: %q", got)
	}
	udp := &store.Monitor{Type: check.TypeUDP}
	if got := oldAgentError(udp, "agent:1", check.Result{Message: "unknown monitor type: udp"}); got != "this agent cannot run udp checks: update it" {
		t.Errorf("udp: %q", got)
	}
}

func TestEventTargetRedactsPassword(t *testing.T) {
	m := &store.Monitor{Type: check.TypePostgres, Target: "postgres://app:hunter2@db/app"}
	if got := eventTarget(m); got != "postgres://app:xxxxx@db/app" {
		t.Errorf("eventTarget = %q", got)
	}
}

func TestValidateIPv6Targets(t *testing.T) {
	h, _ := newTestHub(t)
	cases := []struct {
		typ, target string
		ok          bool
	}{
		{check.TypeHTTP, "http://[2001:db8::1]:8080/health", true},
		{check.TypeHTTP, "http://2001:db8::1/health", false},
		{check.TypeTCP, "[2001:db8::1]:443", true},
		{check.TypeTCP, "2001:db8::1:443", false},
		{check.TypePing, "2001:db8::1", true},
		{check.TypePing, "[2001:db8::1]", true},
		{check.TypeDNS, "ipv6.example.com", true},
		{check.TypeSMTP, "2001:db8::25", true},
		{check.TypeSMTP, "[2001:db8::25]:587", true},
		{check.TypeGRPC, "[2001:db8::1]:50051", true},
		{check.TypeGRPC, "2001:db8::1:50051", false},
		{check.TypeRedis, "redis://:pw@[2001:db8::1]:6379", true},
		{check.TypeRedis, "redis://2001:db8::1", false},
		{check.TypePostgres, "postgres://u:p@[::1]/app", true},
		{check.TypeMongo, "mongodb://[::1]:27017,[::2]:27017/app", true},
	}
	for _, c := range cases {
		m := &store.Monitor{Name: "x", Type: c.typ, Target: c.target}
		err := h.validateMonitor(m)
		if (err == nil) != c.ok {
			t.Errorf("%s %q: err = %v, want ok=%v", c.typ, c.target, err, c.ok)
		}
	}
	// ping keeps the address without brackets
	m := &store.Monitor{Name: "x", Type: check.TypePing, Target: "[::1]"}
	if err := h.validateMonitor(m); err != nil || m.Target != "::1" {
		t.Fatalf("ping target %q, err %v", m.Target, err)
	}
}

func TestValidateIPFamily(t *testing.T) {
	h, _ := newTestHub(t)
	for fam, ok := range map[int]bool{0: true, 4: true, 6: true, 5: false, -1: false} {
		m := &store.Monitor{Name: "x", Type: check.TypeHTTP, Target: "https://example.com", IPFamily: fam}
		if err := h.validateMonitor(m); (err == nil) != ok {
			t.Errorf("ip_family %d: err = %v", fam, err)
		}
	}
	m := &store.Monitor{Name: "x", Type: check.TypeDocker, Target: "web", IPFamily: 6}
	if err := h.validateMonitor(m); err != nil || m.IPFamily != 0 {
		t.Fatalf("docker keeps ip_family %d, err %v", m.IPFamily, err)
	}
}

func TestRedactSteps(t *testing.T) {
	stored := check.Step{URL: "https://u:pw@x/login", Headers: map[string]string{"X-Key": "k"}, Body: `{"password":"pw"}`}
	m := &store.Monitor{Type: check.TypeHTTP, Steps: []check.Step{stored}}
	redactMonitor(m)
	s := m.Steps[0]
	if s.URL != "https://u:"+redacted+"@x/login" || s.Headers["X-Key"] != redacted || s.Body != redacted {
		t.Fatalf("%+v", s)
	}
	if stored.Headers["X-Key"] != "k" {
		t.Fatal("redaction changed the stored headers")
	}
}

func TestValidateChecks2(t *testing.T) {
	h, _ := newTestHub(t)
	cases := []struct {
		typ, target, want string // want "" = rejected
	}{
		{check.TypeSSH, "ssh://srv.example.com:22", "srv.example.com"},
		{check.TypeSSH, "srv.example.com:2222", "srv.example.com:2222"},
		{check.TypeSSH, "root@srv", ""},
		{check.TypeIMAP, "imaps://u:p@mail.example.com", "imaps://u:p@mail.example.com"},
		{check.TypeIMAP, "pop3://mail.example.com", ""},
		{check.TypePOP3, "mail.example.com:995", "mail.example.com:995"},
		{check.TypeNTP, "pool.ntp.org:123", "pool.ntp.org"},
		{check.TypeNTP, "[2001:db8::1]:1123", "[2001:db8::1]:1123"},
		{check.TypeRBL, "203.0.113.7", "203.0.113.7"},
		{check.TypeRBL, "Example.COM.", "example.com"},
		{check.TypeRBL, "https://example.com", ""},
	}
	for _, c := range cases {
		m := &store.Monitor{Name: "x", Type: c.typ, Target: c.target}
		err := h.validateMonitor(m)
		if c.want == "" {
			if err == nil {
				t.Errorf("%s %q accepted", c.typ, c.target)
			}
			continue
		}
		if err != nil || m.Target != c.want {
			t.Errorf("%s %q: %q %v", c.typ, c.target, m.Target, err)
		}
	}
	m := &store.Monitor{Name: "x", Type: check.TypeNTP, Target: "ntp", ExpectedStatus: "1000"}
	if err := h.validateMonitor(m); err != nil || m.ExpectedStatus != "" {
		t.Errorf("default offset: %q %v", m.ExpectedStatus, err)
	}
	m = &store.Monitor{Name: "x", Type: check.TypeNTP, Target: "ntp", ExpectedStatus: "-5"}
	if err := h.validateMonitor(m); err == nil {
		t.Error("negative offset accepted")
	}
	m = &store.Monitor{Name: "x", Type: check.TypeRBL, Target: "1.2.3.4", Keyword: "zen.spamhaus.org\nbl.spamcop.net,zen.spamhaus.org", CertExpiryAlert: true}
	if err := h.validateMonitor(m); err != nil || m.Keyword != "zen.spamhaus.org, bl.spamcop.net" || m.CertExpiryAlert {
		t.Errorf("rbl lists: %q %v", m.Keyword, err)
	}
}

const testCert = `-----BEGIN CERTIFICATE-----
MIIBhTCCASugAwIBAgIQIRi6zePL6mKjOipn+dNuaTAKBggqhkjOPQQDAjASMRAw
DgYDVQQKEwdBY21lIENvMB4XDTE3MTAyMDE5NDMwNloXDTE4MTAyMDE5NDMwNlow
EjEQMA4GA1UEChMHQWNtZSBDbzBZMBMGByqGSM49AgEGCCqGSM49AwEHA0IABD0d
7VNhbWvZLWPuj/RtHFjvtJBEwOkhbN/BnnE8rnZR8+sbwnc/KhCk3FhnpHZnQz7B
5aETbbIgmuvewdjvSBSjYzBhMA4GA1UdDwEB/wQEAwICpDATBgNVHSUEDDAKBggr
BgEFBQcDATAPBgNVHRMBAf8EBTADAQH/MCkGA1UdEQQiMCCCDmxvY2FsaG9zdDo1
NDUzgg4xMjcuMC4wLjE6NTQ1MzAKBggqhkjOPQQDAgNIADBFAiEA2zpJEPQyz6/l
Wf86aX6PepsntZv2GYlA5UpabfT2EZICICpJ5h/iI+i341gBmLiAFQOyTDT+/wQc
6MF9+Yw1Yy0t
-----END CERTIFICATE-----`

const testKey = `-----BEGIN EC PRIVATE KEY-----
MHcCAQEEIIrYSSNQFaA2Hwf1duRSxKtLYX5CB04fSeQ6tF1aY/PuoAoGCCqGSM49
AwEHoUQDQgAEPR3tU2Fta9ktY+6P9G0cWO+0kETA6SFs38GecTyudlHz6xvCdz8q
EKTcWGekdmdDPsHloRNtsiCa697B2O9IFA==
-----END EC PRIVATE KEY-----`

func TestValidateClientCert(t *testing.T) {
	h, _ := newTestHub(t)
	m := &store.Monitor{Name: "x", Type: check.TypeHTTP, Target: "https://x", ClientCert: testCert, ClientKey: testKey}
	if err := h.validateMonitor(m); err != nil || m.ClientKey == "" {
		t.Fatalf("valid pair: %v", err)
	}
	m = &store.Monitor{Name: "x", Type: check.TypeHTTP, Target: "https://x", ClientCert: testCert}
	if err := h.validateMonitor(m); err == nil {
		t.Error("cert without key accepted")
	}
	m = &store.Monitor{Name: "x", Type: check.TypeTCP, Target: "x:1", ClientCert: testCert, ClientKey: testKey}
	if err := h.validateMonitor(m); err != nil || m.ClientCert != "" || m.ClientKey != "" {
		t.Errorf("tcp keeps the cert: %v", err)
	}
	redactMonitor(&store.Monitor{})
	r := &store.Monitor{ClientCert: testCert, ClientKey: testKey}
	redactMonitor(r)
	if r.ClientKey != redacted || r.ClientCert != testCert {
		t.Error("key not redacted")
	}
	withCert := &store.Monitor{Type: check.TypeHTTP, ClientCert: testCert}
	if got := oldAgentError(withCert, "agent:1", check.Result{OK: true}); got != "this agent cannot send client certificates: update it" {
		t.Errorf("old agent: %q", got)
	}
	if got := oldAgentError(withCert, "agent:1", check.Result{OK: true, ClientCert: true}); got != "" {
		t.Errorf("new agent: %q", got)
	}
}

func TestClientKeyExport(t *testing.T) {
	h, st := newTestHub(t)
	m := &store.Monitor{Name: "mtls", Type: check.TypeHTTP, Target: "https://x", ClientCert: testCert, ClientKey: testKey}
	if err := h.validateMonitor(m); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateMonitor(m); err != nil {
		t.Fatal(err)
	}
	doc, err := h.ExportConfig(false)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Monitors[0].ClientKey != redactedValue || doc.Monitors[0].ClientCert != testCert {
		t.Fatalf("key exported: %q", doc.Monitors[0].ClientKey)
	}
	// Same hub: the stored key is kept.
	if _, err := h.ImportConfig(doc, ImportOptions{}); err != nil {
		t.Fatal(err)
	}
	if got, _ := st.Monitor(m.ID); got.ClientKey != testKey || got.Paused {
		t.Fatalf("key lost on re-import: paused=%v", got.Paused)
	}
	// Another hub: no key, so no certificate, and the monitor is paused.
	h2, st2 := newTestHub(t)
	if _, err := h2.ImportConfig(doc, ImportOptions{}); err != nil {
		t.Fatal(err)
	}
	ms, _ := st2.ListMonitors()
	if len(ms) != 1 || ms[0].ClientCert != "" || ms[0].ClientKey != "" || !ms[0].Paused {
		t.Fatalf("imported: %+v", ms)
	}
}

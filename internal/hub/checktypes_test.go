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

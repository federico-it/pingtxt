package check

import (
	"strings"
	"testing"
)

func TestServerResult(t *testing.T) {
	m := &ServerMetrics{CPUPct: 95, Cores: 4, Load5: 2, MemTotal: 1000, MemUsed: 500, SwapTotal: 100, SwapUsed: 10,
		Disks: []DiskUsage{{Mount: "/", Used: 40, Free: 60}, {Mount: "/data", Used: 93, Free: 7}}}
	r := serverResult(m, ServerLimits{}, nil, 1)
	if !r.OK || r.Message != "cpu 95%, ram 50%, disk /data 93%, load 2.00" || r.Server != m {
		t.Fatalf("no limits: %+v", r)
	}
	r = serverResult(m, ServerLimits{CPUPct: 90, MemPct: 90, DiskPct: 90, Load: 0.4}, nil, 1)
	if r.OK || r.Message != "cpu 95% over 90%, disk /data 93% over 90%, load 2.00 over 0.40 per core (4 cores)" {
		t.Fatalf("limits: %+v", r)
	}
	r = serverResult(m, ServerLimits{}, []string{"/", "/backup"}, 1)
	if r.OK || r.Message != "no filesystem at /backup" {
		t.Fatalf("missing mount: %+v", r)
	}
}

func TestServerMounts(t *testing.T) {
	got, err := ServerMounts("/, /data/,/data")
	if err != nil || strings.Join(got, "|") != "/|/data" {
		t.Fatalf("%v %v", got, err)
	}
	if _, err := ServerMounts("data"); err == nil {
		t.Error("relative path accepted")
	}
	if err := ValidateServerLimits(ServerLimits{CPUPct: 101}); err == nil {
		t.Error("101% accepted")
	}
}

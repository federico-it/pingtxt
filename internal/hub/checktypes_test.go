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

func TestEventTargetRedactsPassword(t *testing.T) {
	m := &store.Monitor{Type: check.TypePostgres, Target: "postgres://app:hunter2@db/app"}
	if got := eventTarget(m); got != "postgres://app:xxxxx@db/app" {
		t.Errorf("eventTarget = %q", got)
	}
}

package check

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const healthBody = `{
  "status": "ready",
  "checks": {
    "database": {"status": "ok", "latencyMs": 0},
    "redis": {"status": "ok", "configured": true, "latencyMs": 3, "backend": "redis"},
    "worker": {"status": "ok", "expected": true, "ageMs": 23759, "failed": 0}
  },
  "regions": ["eu", "us"],
  "items": [{"id": 1}, {"id": 2}],
  "a.b": {"c": null},
  "timestamp": "2026-10-31T01:06:47.833Z"
}`

func TestAssertions(t *testing.T) {
	pass := []Assertion{
		{Path: "status", Op: OpEquals, Value: "ready"},
		{Path: "$.checks.redis.status", Op: OpEquals, Value: "ok"},
		{Path: "checks.redis.configured", Op: OpEquals, Value: "true"},
		{Path: "checks.redis.latencyMs", Op: OpEquals, Value: "3.0"},
		{Path: "checks.worker.ageMs", Op: OpLess, Value: "60000"},
		{Path: "checks.worker.failed", Op: OpLessEq, Value: "0"},
		{Path: "checks.redis.latencyMs", Op: OpGreater, Value: "2"},
		{Path: "checks.worker.ageMs", Op: OpGreaterEq, Value: "23759"},
		{Path: "status", Op: OpNotEquals, Value: "degraded"},
		{Path: "checks.redis.backend", Op: OpContains, Value: "red"},
		{Path: "regions", Op: OpContains, Value: "eu"},
		{Path: "regions", Op: OpNotContains, Value: "asia"},
		{Path: "checks", Op: OpContains, Value: "worker"},
		{Path: "checks.database", Op: OpExists},
		{Path: "checks.cache", Op: OpNotExists},
		{Path: "items[1].id", Op: OpEquals, Value: "2"},
		{Path: "items.0.id", Op: OpEquals, Value: "1"},
		{Path: "items[-1].id", Op: OpEquals, Value: "2"},
		{Path: `["a.b"].c`, Op: OpEquals, Value: "null"},
		{Path: "timestamp", Op: OpMatches, Value: `^2026-\d\d-`},
		{Path: "checks.database", Op: OpEquals, Value: `{"latencyMs":0,"status":"ok"}`},
		{Path: "", Op: OpExists},
	}
	for _, a := range pass {
		if err := ValidateAssertion(&a); err != nil {
			t.Fatalf("%+v: invalid: %v", a, err)
		}
		if n, msg := CheckAssertions([]byte(healthBody), []Assertion{a}); n != 1 || msg != "" {
			t.Errorf("%+v: %s", a, msg)
		}
	}

	fail := map[string]Assertion{
		`status: expected "ok", got "ready"`:                      {Path: "status", Op: OpEquals, Value: "ok"},
		"checks.cache.status: not found":                          {Path: "checks.cache.status", Op: OpEquals, Value: "ok"},
		"checks.worker.ageMs: expected < 1000, got 23759":         {Path: "checks.worker.ageMs", Op: OpLess, Value: "1000"},
		`status: expected a number, got "ready"`:                  {Path: "status", Op: OpGreater, Value: "1"},
		`regions: should not contain "eu", got ["eu","us"]`:       {Path: "regions", Op: OpNotContains, Value: "eu"},
		"checks.database: should not exist, got {":                {Path: "checks.database", Op: OpNotExists},
		`status: expected anything but "ready"`:                   {Path: "status", Op: OpNotEquals, Value: "ready"},
		`timestamp: "2026-10-31T01:06:47.833Z" does not match /^`: {Path: "timestamp", Op: OpMatches, Value: `^2025`},
		"items[5]: not found":                                     {Path: "items[5]", Op: OpExists},
	}
	for want, a := range fail {
		_, msg := CheckAssertions([]byte(healthBody), []Assertion{a})
		if !strings.HasPrefix(msg, want) {
			t.Errorf("%+v: got %q, want %q", a, msg, want)
		}
	}

	if n, msg := CheckAssertions([]byte("<html>"), pass[:1]); n != 0 || msg != "response is not valid JSON" {
		t.Errorf("not json: %d %q", n, msg)
	}
	// Rules run in order and stop at the first failure.
	rules := []Assertion{pass[0], {Path: "status", Op: OpEquals, Value: "x"}, pass[1]}
	if n, _ := CheckAssertions([]byte(healthBody), rules); n != 1 {
		t.Errorf("passed = %d, want 1", n)
	}
}

func TestValidateAssertion(t *testing.T) {
	bad := []Assertion{
		{Path: "a", Op: "like"},
		{Path: "a", Op: OpGreater, Value: "ten"},
		{Path: "a", Op: OpMatches, Value: "("},
		{Path: "a..b", Op: OpExists},
		{Path: "a[x]", Op: OpExists},
		{Path: "a[0", Op: OpExists},
		{Path: `a["b]`, Op: OpExists},
		{Path: "a[0]b", Op: OpExists},
	}
	for _, a := range bad {
		if err := ValidateAssertion(&a); err == nil {
			t.Errorf("%+v: expected an error", a)
		}
	}
	a := Assertion{Path: " status ", Op: OpExists, Value: "x"}
	if err := ValidateAssertion(&a); err != nil || a.Path != "status" || a.Value != "" {
		t.Errorf("normalize: %+v %v", a, err)
	}
}

func TestHTTPAssertions(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(healthBody))
	}))
	defer srv.Close()
	ctx := context.Background()
	ok := []Assertion{{Path: "status", Op: OpEquals, Value: "ready"}, {Path: "checks.redis.status", Op: OpEquals, Value: "ok"}}
	r := Run(ctx, Job{Type: TypeHTTP, Target: srv.URL, Assertions: ok})
	if !r.OK || r.Asserted != 2 {
		t.Fatalf("expected up with 2 rules checked: %+v", r)
	}
	r = Run(ctx, Job{Type: TypeHTTP, Target: srv.URL, Assertions: append(ok, Assertion{Path: "checks.worker.status", Op: OpEquals, Value: "down"})})
	if r.OK || r.Message != `checks.worker.status: expected "down", got "ok"` || r.StatusCode != 200 {
		t.Fatalf("expected a failed rule: %+v", r)
	}
	// Keyword monitors check both.
	r = Run(ctx, Job{Type: TypeKeyword, Target: srv.URL, Keyword: "worker", Assertions: ok})
	if !r.OK {
		t.Fatalf("keyword + rules: %+v", r)
	}
}

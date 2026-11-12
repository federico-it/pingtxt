package check

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"
)

func TestFindings(t *testing.T) {
	now := time.Date(2026, 11, 12, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		name string
		d    Diag
		code string // the first finding
	}{
		{"nxdomain", Diag{Host: "x.invalid", DNS: []DiagDNS{{Resolver: "system", Query: "A/AAAA x.invalid", Error: "no such host"}}}, "dns_nxdomain"},
		{"refused", Diag{Port: "443", TCP: []DiagConn{{Addr: "1.2.3.4:443", Error: "connect: connection refused"}}}, "tcp_refused"},
		{"timeout", Diag{Port: "443", TCP: []DiagConn{{Addr: "1.2.3.4:443", Error: "i/o timeout"}},
			Trace: &DiagTrace{Hops: []DiagHop{{TTL: 1, Addr: "10.0.0.1"}, {TTL: 2}}}}, "tcp_timeout"},
		{"ipv6 only broken", Diag{Port: "443", TCP: []DiagConn{{Addr: "1.2.3.4:443", OK: true}, {Addr: "[2001:db8::1]:443", Error: "i/o timeout"}}}, "tcp_family"},
		{"expired", Diag{Port: "443", TCP: []DiagConn{{Addr: "1.2.3.4:443", OK: true}},
			TLS: &DiagTLS{NotAfter: now.Add(-48 * time.Hour).Unix(), Error: "x509: certificate has expired or is not yet valid"}}, "tls_expired"},
		{"wrong name", Diag{Host: "a.example", Port: "443", TCP: []DiagConn{{Addr: "1.2.3.4:443", OK: true}},
			TLS: &DiagTLS{NotAfter: now.Add(900 * time.Hour).Unix(), SANs: []string{"b.example"}, Error: "x509: certificate is valid for b.example, not a.example"}}, "tls_name"},
		{"cloudflare", Diag{Port: "443", TCP: []DiagConn{{Addr: "1.2.3.4:443", OK: true}},
			HTTP: &DiagHTTP{Status: 522, Headers: []DiagHeader{{Name: "Server", Value: "cloudflare"}}}}, "http_cloudflare"},
		{"gateway", Diag{Port: "443", TCP: []DiagConn{{Addr: "1.2.3.4:443", OK: true}}, HTTP: &DiagHTTP{Status: 502}}, "http_gateway"},
		{"service", Diag{Port: "5432", TCP: []DiagConn{{Addr: "1.2.3.4:5432", OK: true}}}, "network_ok"},
		{"ping filtered", Diag{Type: TypePing, Trace: &DiagTrace{Reached: true, Hops: []DiagHop{{TTL: 1, Addr: "1.2.3.4"}}}}, "ping_filtered"},
		{"ping route", Diag{Type: TypePing, Trace: &DiagTrace{Hops: []DiagHop{{TTL: 1, Addr: "10.0.0.1"}, {TTL: 2, Addr: "10.0.0.2", Name: "edge"}, {TTL: 3}}}}, "trace_stops"},
		{"smtp", Diag{Type: TypeSMTP, Port: "25", Banner: "421 too busy", TCP: []DiagConn{{Addr: "1.2.3.4:25", OK: true}}}, "smtp_banner"},
		{"dns mismatch", Diag{Type: TypeDNS, DNS: []DiagDNS{
			{Resolver: "9.9.9.9:53", Query: "A example.com", Answers: []string{"1.1.1.1"}},
			{Resolver: "system", Query: "A example.com", Answers: []string{"2.2.2.2"}},
			{Resolver: "system", Query: "NS example.com", Answers: []string{"ns1."}},
		}}, "dns_mismatch"},
		{"dns server down", Diag{Type: TypeDNS, DNS: []DiagDNS{
			{Resolver: "9.9.9.9:53", Query: "A example.com", Error: "i/o timeout"},
			{Resolver: "system", Query: "A example.com", Answers: []string{"2.2.2.2"}},
		}}, "dns_record_error"},
		{"docker note", Diag{Note: "no network diagnostics"}, "note"},
	}
	for _, c := range cases {
		f := c.d.findingsAt(now)
		if len(f) == 0 || f[0].Code != c.code {
			t.Errorf("%s: got %+v, want %s first", c.name, f, c.code)
			continue
		}
		if f[0].Text == "" || strings.Contains(f[0].Text, "%") {
			t.Errorf("%s: text %q", c.name, f[0].Text)
		}
	}

	// The route explains a timeout.
	d := Diag{Port: "443", TCP: []DiagConn{{Addr: "1.2.3.4:443", Error: "i/o timeout"}},
		Trace: &DiagTrace{Hops: []DiagHop{{TTL: 1, Addr: "10.0.0.1"}, {TTL: 2}}}}
	f := d.findingsAt(now)
	if len(f) != 2 || f[1].Code != "trace_stops" || f[1].Params["hop"] != "1" {
		t.Fatalf("timeout findings: %+v", f)
	}
	// A certificate close to expiry is only a warning.
	d = Diag{Port: "443", TCP: []DiagConn{{Addr: "1.2.3.4:443", OK: true}}, TLS: &DiagTLS{NotAfter: now.Add(72 * time.Hour).Unix()}}
	f = d.findingsAt(now)
	if f[0].Code != "tls_expiring" || f[0].Params["days"] != "3" {
		t.Fatalf("expiring: %+v", f)
	}
	// The summary leads with the diagnosis.
	d = Diag{Port: "443", TCP: []DiagConn{{Addr: "1.2.3.4:443", Error: "connection refused"}}}
	if s := d.Summary(); !strings.HasPrefix(s, "Port 443 refused") {
		t.Fatalf("summary: %q", s)
	}
}

func TestDiagnoseSMTPBanner(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			_, _ = c.Write([]byte("421 4.3.2 too many connections\r\n"))
			c.Close()
		}
	}()
	r := Run(context.Background(), Job{Type: TypeSMTP, Target: "smtp://" + ln.Addr().String(), TimeoutMs: 2000, Diagnose: true})
	if r.OK || r.Diag == nil {
		t.Fatalf("result: %+v", r)
	}
	if r.Diag.Banner != "421 4.3.2 too many connections" || r.Diag.Type != TypeSMTP || r.Diag.Check == "" {
		t.Fatalf("diag: %+v", r.Diag)
	}
	if f := r.Diag.Findings(); f[0].Code != "smtp_banner" {
		t.Fatalf("findings: %+v", f)
	}
}

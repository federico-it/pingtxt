package check

import "testing"

func TestNetFor(t *testing.T) {
	cases := []struct {
		fam      int
		in, want string
	}{
		{IPAny, "tcp", "tcp"},
		{IPv4, "tcp", "tcp4"},
		{IPv6, "tcp", "tcp6"},
		{IPv6, "udp", "udp6"},
		{IPv4, "ip", "ip4"},
		{IPv6, "tcp4", "tcp4"}, // already limited by the caller
		{IPv6, "unix", "unix"},
	}
	for _, c := range cases {
		if got := (Job{IPFamily: c.fam}).netFor(c.in); got != c.want {
			t.Errorf("netFor(%d, %q) = %q, want %q", c.fam, c.in, got, c.want)
		}
	}
}

func TestIPv6Targets(t *testing.T) {
	if got := StripBrackets("[2001:db8::1]"); got != "2001:db8::1" {
		t.Errorf("StripBrackets: %q", got)
	}
	if got := StripBrackets("example.com"); got != "example.com" {
		t.Errorf("StripBrackets: %q", got)
	}
	for in, want := range map[string]string{
		"2001:db8::1": "[2001:db8::1]",
		"::1":         "[::1]",
		"192.0.2.1":   "192.0.2.1",
		"mx.example":  "mx.example",
	} {
		if got := BracketIPv6(in); got != want {
			t.Errorf("BracketIPv6(%q) = %q, want %q", in, got, want)
		}
	}
	smtp := map[string][2]string{
		"2001:db8::25":           {"2001:db8::25", "25"},
		"[2001:db8::25]:587":     {"2001:db8::25", "587"},
		"smtps://[2001:db8::25]": {"2001:db8::25", "465"},
	}
	for in, want := range smtp {
		host, port, _, err := parseSMTPTarget(in)
		if err != nil || host != want[0] || port != want[1] {
			t.Errorf("parseSMTPTarget(%q) = %q %q %v, want %v", in, host, port, err, want)
		}
	}
}

func TestResultRepeatsIPFamily(t *testing.T) {
	r := Run(t.Context(), Job{Type: "nope", IPFamily: IPv6})
	if r.IPFamily != IPv6 {
		t.Fatalf("IPFamily = %d", r.IPFamily)
	}
}

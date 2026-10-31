package check

import (
	"context"
	"net"
	"strings"
)

// IP families a monitor can be limited to. 0 uses IPv4 or IPv6, whichever
// connects (Go tries both on dual-stack hosts).
const (
	IPAny = 0
	IPv4  = 4
	IPv6  = 6
	// IPBoth checks over IPv4 and IPv6 at once: the check passes only when
	// both work.
	IPBoth = 46
)

// runBoth runs the job over IPv4 and IPv6 in parallel and merges the two
// results: it fails when either fails, and reports the slower latency.
func runBoth(ctx context.Context, j Job, run func(context.Context, Job) Result) Result {
	j4, j6 := j, j
	j4.IPFamily, j6.IPFamily = IPv4, IPv6
	ch := make(chan Result, 1)
	go func() { ch <- run(ctx, j6) }()
	r4 := run(ctx, j4)
	r6 := <-ch
	r := r4
	r.OK = r4.OK && r6.OK
	r.LatencyMs = max(r4.LatencyMs, r6.LatencyMs)
	r.Asserted = min(r4.Asserted, r6.Asserted)
	switch {
	case !r4.OK && !r6.OK:
		r.Message = "IPv4: " + r4.Message + "; IPv6: " + r6.Message
	case !r6.OK:
		r.Message = "IPv6: " + r6.Message
		r.StatusCode = r6.StatusCode
	case !r4.OK:
		r.Message = "IPv4: " + r4.Message
	}
	if r6.LatencyMs > r4.LatencyMs && r6.Timing != nil {
		r.Timing = r6.Timing
	}
	r.Both = true
	return r
}

// netFor limits a network name ("tcp", "udp", "ip") to the job's IP family.
func (j Job) netFor(network string) string {
	if strings.HasSuffix(network, "4") || strings.HasSuffix(network, "6") || network == "unix" {
		return network
	}
	switch j.IPFamily {
	case IPv4:
		return network + "4"
	case IPv6:
		return network + "6"
	}
	return network
}

// dialContext dials addr over the job's IP family.
func (j Job) dialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	var d net.Dialer
	return d.DialContext(ctx, j.netFor(network), addr)
}

// StripBrackets turns "[2001:db8::1]" into "2001:db8::1", for targets that
// take a bare host (ping, dns).
func StripBrackets(host string) string {
	if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
		return host[1 : len(host)-1]
	}
	return host
}

// BracketIPv6 puts a bare IPv6 address in brackets so it can be used in a
// URL or followed by a port; anything else is returned as is.
func BracketIPv6(host string) string {
	if ip := net.ParseIP(host); ip != nil && strings.Contains(host, ":") {
		return "[" + host + "]"
	}
	return host
}

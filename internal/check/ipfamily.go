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
)

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

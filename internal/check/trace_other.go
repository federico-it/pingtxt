//go:build !linux

package check

import (
	"context"
	"net"
)

// Traceroute is only implemented on Linux for now.
func Traceroute(_ context.Context, dest net.IP, port, _ int) *DiagTrace {
	t := &DiagTrace{Proto: "tcp", Dest: dest.String(), Port: port}
	if port <= 0 {
		t.Proto, t.Port = "udp", 33434
	}
	t.Error = errTraceUnsupported.Error()
	return t
}

package check

import (
	"context"
	"net"
	"os"
	"strings"
	"sync/atomic"
	"time"
)

// IPv6 connectivity of the host running the checks (the hub or an agent).
// A container without IPv6 fails every IPv6 check with "network is
// unreachable", and a network that drops IPv6 traffic makes them time out:
// IPv6-only monitors skip such locations instead.
const (
	IPv6Unknown int32 = iota // not probed yet
	IPv6Yes
	IPv6No
)

// ipv6Probes are well-known anycast resolvers reachable over IPv6
// (Cloudflare, Google, Quad9). One answering TCP connection is enough.
var ipv6Probes = []string{
	"[2606:4700:4700::1111]:443",
	"[2001:4860:4860::8888]:443",
	"[2620:fe::fe]:443",
}

// ProbeIPv6 reports whether this host can reach the Internet over IPv6. The
// PINGTXT_IPV6 variable overrides the probe: "on" or "off" (for networks
// that block the probe addresses), anything else probes.
func ProbeIPv6(ctx context.Context) bool {
	switch strings.ToLower(os.Getenv("PINGTXT_IPV6")) {
	case "on", "1", "true", "yes":
		return true
	case "off", "0", "false", "no":
		return false
	}
	return probeIPv6(ctx, ipv6Probes, 5*time.Second)
}

func probeIPv6(ctx context.Context, addrs []string, timeout time.Duration) bool {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	ok := make(chan bool, len(addrs))
	for _, a := range addrs {
		go func() {
			var d net.Dialer
			c, err := d.DialContext(ctx, "tcp6", a)
			if err == nil {
				c.Close()
			}
			ok <- err == nil
		}()
	}
	for range addrs {
		if <-ok {
			return true
		}
	}
	return false
}

// IPv6Watcher probes IPv6 connectivity at start and then periodically.
type IPv6Watcher struct {
	state atomic.Int32
	// OnChange, if set, is called after the state changes.
	OnChange func(state int32)
}

// State returns IPv6Unknown, IPv6Yes or IPv6No.
func (w *IPv6Watcher) State() int32 { return w.state.Load() }

// Set stores a state (for tests and overrides).
func (w *IPv6Watcher) Set(s int32) {
	if w.state.Swap(s) != s && w.OnChange != nil {
		w.OnChange(s)
	}
}

// Run probes every interval until ctx is done.
func (w *IPv6Watcher) Run(ctx context.Context, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	fails := 0
	for {
		up := ProbeIPv6(ctx)
		if ctx.Err() != nil {
			return
		}
		if up {
			fails = 0
			w.Set(IPv6Yes)
		} else if fails++; fails >= 2 || w.State() != IPv6Yes {
			// A single failed probe does not turn IPv6 off once it worked.
			w.Set(IPv6No)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

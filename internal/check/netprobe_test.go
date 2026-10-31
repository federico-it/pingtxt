package check

import (
	"context"
	"net"
	"testing"
	"time"
)

func TestProbeIPv6(t *testing.T) {
	if probeIPv6(context.Background(), []string{"127.0.0.1:1"}, 2*time.Second) {
		t.Error("an IPv4 address is not IPv6")
	}
	ln, err := net.Listen("tcp6", "[::1]:0")
	if err != nil {
		t.Skip("no IPv6 loopback:", err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()
	closed, _ := net.Listen("tcp6", "[::1]:0")
	dead := closed.Addr().String()
	closed.Close()

	ctx := context.Background()
	if !probeIPv6(ctx, []string{dead, ln.Addr().String()}, 2*time.Second) {
		t.Error("one answering address is enough")
	}
	if probeIPv6(ctx, []string{dead}, 2*time.Second) {
		t.Error("no answering address must report no IPv6")
	}
}

func TestProbeIPv6Override(t *testing.T) {
	t.Setenv("PINGTXT_IPV6", "on")
	if !ProbeIPv6(context.Background()) {
		t.Error("on")
	}
	t.Setenv("PINGTXT_IPV6", "off")
	if ProbeIPv6(context.Background()) {
		t.Error("off")
	}
}

func TestIPv6Watcher(t *testing.T) {
	var w IPv6Watcher
	var got []int32
	w.OnChange = func(s int32) { got = append(got, s) }
	w.Set(IPv6Yes)
	w.Set(IPv6Yes)
	w.Set(IPv6No)
	if len(got) != 2 || got[0] != IPv6Yes || got[1] != IPv6No || w.State() != IPv6No {
		t.Fatalf("changes %v, state %d", got, w.State())
	}
}

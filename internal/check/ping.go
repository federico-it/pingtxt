package check

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync/atomic"
	"syscall"
	"time"

	probing "github.com/prometheus-community/pro-bing"
)

// Ping modes: raw ICMP sockets need CAP_NET_RAW (root, setcap or
// AmbientCapabilities); unprivileged ICMP ("ping sockets") needs the
// process group in net.ipv4.ping_group_range, which Docker allows by
// default inside containers. The containers run as a non-root user, so the
// raw socket fails there and the unprivileged one works.
const (
	pingAuto int32 = iota
	pingRaw
	pingUnprivileged
)

// pingMode is the mode that worked last. In auto mode the raw socket is
// tried first and the unprivileged socket is used when it is not allowed.
var pingMode atomic.Int32

func init() { pingMode.Store(configuredPingMode()) }

// configuredPingMode reads PINGTXT_UNPRIVILEGED_PING: 1 or true forces
// unprivileged ICMP, 0 or false forces raw sockets, unset tries both.
func configuredPingMode() int32 {
	switch os.Getenv("PINGTXT_UNPRIVILEGED_PING") {
	case "":
		return pingAuto
	case "0", "false", "no":
		return pingRaw
	default:
		return pingUnprivileged
	}
}

func runPing(ctx context.Context, j Job, timeout time.Duration) Result {
	mode := pingMode.Load()
	if mode == pingAuto {
		r, err := ping(ctx, j, timeout, true)
		if err == nil || !permissionError(err) {
			if err == nil {
				pingMode.CompareAndSwap(pingAuto, pingRaw)
			}
			return pingResult(r, err)
		}
		r, err = ping(ctx, j, timeout, false)
		if err == nil {
			pingMode.CompareAndSwap(pingAuto, pingUnprivileged)
		} else if permissionError(err) {
			return Result{Message: "ping not allowed: needs CAP_NET_RAW or net.ipv4.ping_group_range (" + cleanErr(err) + ")"}
		}
		return pingResult(r, err)
	}
	return pingResult(ping(ctx, j, timeout, mode == pingRaw))
}

func ping(ctx context.Context, j Job, timeout time.Duration, privileged bool) (*probing.Statistics, error) {
	p := probing.New(StripBrackets(j.Target))
	p.SetNetwork(j.netFor("ip"))
	if err := p.Resolve(); err != nil {
		return nil, err
	}
	p.Count = 3
	p.Interval = 300 * time.Millisecond
	p.Timeout = timeout
	p.SetPrivileged(privileged)
	if err := p.RunWithContext(ctx); err != nil {
		return nil, err
	}
	return p.Statistics(), nil
}

func pingResult(s *probing.Statistics, err error) Result {
	if err != nil {
		return Result{Message: cleanErr(err)}
	}
	if s.PacketsRecv == 0 {
		return Result{Message: fmt.Sprintf("100%% packet loss (%s)", s.IPAddr)}
	}
	return Result{
		OK:        true,
		LatencyMs: ms(s.AvgRtt),
		Message:   fmt.Sprintf("%d/%d packets, %.0f%% loss", s.PacketsRecv, s.PacketsSent, s.PacketLoss),
	}
}

// permissionError reports whether opening the ICMP socket was refused.
func permissionError(err error) bool {
	return errors.Is(err, syscall.EPERM) || errors.Is(err, syscall.EACCES) || errors.Is(err, os.ErrPermission)
}

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
			return pingResult(j, r, err)
		}
		r, err = ping(ctx, j, timeout, false)
		if err == nil {
			pingMode.CompareAndSwap(pingAuto, pingUnprivileged)
		} else if permissionError(err) {
			return Result{Message: "ping not allowed: needs CAP_NET_RAW or net.ipv4.ping_group_range (" + cleanErr(err) + ")"}
		}
		return pingResult(j, r, err)
	}
	r, err := ping(ctx, j, timeout, mode == pingRaw)
	return pingResult(j, r, err)
}

func ping(ctx context.Context, j Job, timeout time.Duration, privileged bool) (*probing.Statistics, error) {
	p := probing.New(StripBrackets(j.Target))
	p.SetNetwork(j.netFor("ip"))
	if err := p.Resolve(); err != nil {
		return nil, err
	}
	p.Count = j.pingCount()
	p.Interval = PingInterval
	p.Timeout = timeout
	p.SetPrivileged(privileged)
	if err := p.RunWithContext(ctx); err != nil {
		return nil, err
	}
	return p.Statistics(), nil
}

// Ping count limits and the gap between echo requests.
const (
	DefaultPingCount = 3
	MaxPingCount     = 20
	PingInterval     = 300 * time.Millisecond
)

// pingCount is the number of echo requests to send for j.
func (j Job) pingCount() int {
	if j.PingCount <= 0 {
		return DefaultPingCount
	}
	return min(j.PingCount, MaxPingCount)
}

// MinPingTimeout is the shortest timeout that leaves room for count echo
// requests: the gaps between them plus 500 ms for the last reply.
func MinPingTimeout(count int) time.Duration {
	return time.Duration(max(count-1, 0))*PingInterval + 500*time.Millisecond
}

func pingResult(j Job, s *probing.Statistics, err error) Result {
	if err != nil {
		return Result{Message: cleanErr(err)}
	}
	if s.PacketsRecv == 0 {
		return Result{Message: fmt.Sprintf("100%% packet loss (%s)", s.IPAddr), PingSent: s.PacketsSent}
	}
	r := Result{
		OK:        true,
		LatencyMs: ms(s.AvgRtt),
		Message:   fmt.Sprintf("%d/%d packets, %.0f%% loss", s.PacketsRecv, s.PacketsSent, s.PacketLoss),
		PingSent:  s.PacketsSent,
	}
	if j.PingLossPct > 0 && s.PacketLoss >= float64(j.PingLossPct) {
		r.OK = false
		r.Message += fmt.Sprintf(" (limit %d%%)", j.PingLossPct)
	}
	return r
}

// permissionError reports whether opening the ICMP socket was refused.
func permissionError(err error) bool {
	return errors.Is(err, syscall.EPERM) || errors.Is(err, syscall.EACCES) || errors.Is(err, os.ErrPermission)
}

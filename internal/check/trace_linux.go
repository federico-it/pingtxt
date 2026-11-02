//go:build linux

package check

import (
	"context"
	"encoding/binary"
	"errors"
	"net"
	"time"

	"golang.org/x/sys/unix"
)

// Traceroute sends one probe per TTL, all at once, and reads the ICMP
// errors from each socket's error queue (IP_RECVERR). This needs no
// privileges, like tracepath: it works for the non-root containers too.
// With a port the probes are TCP SYNs to it (they pass firewalls that let
// the service through); without one they are UDP datagrams to port 33434.
func Traceroute(ctx context.Context, dest net.IP, port, maxHops int) *DiagTrace {
	t := &DiagTrace{Proto: "tcp", Dest: dest.String(), Port: port}
	if port <= 0 {
		t.Proto, t.Port = "udp", 33434
	}
	v4 := dest.To4() != nil
	family, level, ttlOpt, errOpt := unix.AF_INET6, unix.IPPROTO_IPV6, unix.IPV6_UNICAST_HOPS, unix.IPV6_RECVERR
	if v4 {
		family, level, ttlOpt, errOpt = unix.AF_INET, unix.IPPROTO_IP, unix.IP_TTL, unix.IP_RECVERR
	}
	var sa unix.Sockaddr
	if v4 {
		a := &unix.SockaddrInet4{Port: t.Port}
		copy(a.Addr[:], dest.To4())
		sa = a
	} else {
		a := &unix.SockaddrInet6{Port: t.Port}
		copy(a.Addr[:], dest.To16())
		sa = a
	}
	typ := unix.SOCK_STREAM
	if t.Proto == "udp" {
		typ = unix.SOCK_DGRAM
	}

	type probe struct {
		fd   int
		ttl  int
		sent time.Time
		done bool
	}
	var probes []*probe
	defer func() {
		for _, p := range probes {
			unix.Close(p.fd)
		}
	}()
	for ttl := 1; ttl <= maxHops; ttl++ {
		fd, err := unix.Socket(family, typ|unix.SOCK_NONBLOCK|unix.SOCK_CLOEXEC, 0)
		if err != nil {
			t.Error = "socket: " + err.Error()
			return t
		}
		probes = append(probes, &probe{fd: fd, ttl: ttl})
		if err := unix.SetsockoptInt(fd, level, ttlOpt, ttl); err != nil {
			t.Error = "set TTL: " + err.Error()
			return t
		}
		if err := unix.SetsockoptInt(fd, level, errOpt, 1); err != nil {
			t.Error = "IP_RECVERR: " + err.Error()
			return t
		}
		p := probes[len(probes)-1]
		p.sent = time.Now()
		err = unix.Connect(fd, sa)
		if t.Proto == "udp" && err == nil {
			_, err = unix.Write(fd, []byte("pingtxt traceroute"))
		}
		if err != nil && !errors.Is(err, unix.EINPROGRESS) && !errors.Is(err, unix.ECONNREFUSED) {
			if errors.Is(err, unix.ENETUNREACH) || errors.Is(err, unix.EHOSTUNREACH) {
				t.Error = err.Error()
				return t
			}
		}
	}

	hops := make([]DiagHop, maxHops)
	for i := range hops {
		hops[i].TTL = i + 1
	}
	reachedAt := 0 // lowest TTL that reached the destination
	deadline := time.Now().Add(diagHopWait)
	if dl, ok := ctx.Deadline(); ok && dl.Before(deadline) {
		deadline = dl
	}
	buf := make([]byte, 512)
	oob := make([]byte, 512)
	for {
		wait := time.Until(deadline)
		if wait <= 0 || ctx.Err() != nil {
			break
		}
		var fds []unix.PollFd
		var open []*probe
		for _, p := range probes {
			// Stop waiting for hops past the destination.
			if p.done || (reachedAt > 0 && p.ttl > reachedAt) {
				continue
			}
			events := int16(unix.POLLERR)
			if t.Proto == "tcp" {
				events |= unix.POLLOUT
			}
			fds = append(fds, unix.PollFd{Fd: int32(p.fd), Events: events})
			open = append(open, p)
		}
		if len(fds) == 0 {
			break
		}
		n, err := unix.Poll(fds, int(min(wait, 200*time.Millisecond)/time.Millisecond)+1)
		if err != nil && !errors.Is(err, unix.EINTR) {
			t.Error = "poll: " + err.Error()
			break
		}
		if n <= 0 {
			continue
		}
		for i, f := range fds {
			if f.Revents == 0 {
				continue
			}
			p := open[i]
			rtt := ms(time.Since(p.sent))
			hop := &hops[p.ttl-1]
			// ICMP errors (time exceeded, unreachable) wait in the error queue.
			if _, oobn, _, _, err := unix.Recvmsg(p.fd, buf, oob, unix.MSG_ERRQUEUE); err == nil {
				from, icmpType, icmpCode, ok := parseRecvErr(oob[:oobn], v4)
				if !ok {
					continue
				}
				p.done = true
				hop.Addr, hop.RTTMs = from.String(), rtt
				timeExceeded := (v4 && icmpType == 11) || (!v4 && icmpType == 3)
				if !timeExceeded && from.Equal(dest) {
					if reachedAt == 0 || p.ttl < reachedAt {
						reachedAt = p.ttl
					}
				}
				_ = icmpCode
				continue
			}
			// No ICMP error: for TCP the connection either opened or was
			// refused, and both mean the probe reached the destination.
			if t.Proto == "tcp" && f.Revents&(unix.POLLOUT|unix.POLLERR|unix.POLLHUP) != 0 {
				soErr, _ := unix.GetsockoptInt(p.fd, unix.SOL_SOCKET, unix.SO_ERROR)
				if soErr == 0 || soErr == int(unix.ECONNREFUSED) || soErr == int(unix.ECONNRESET) {
					p.done = true
					hop.Addr, hop.RTTMs = dest.String(), rtt
					if reachedAt == 0 || p.ttl < reachedAt {
						reachedAt = p.ttl
					}
				} else {
					// Some other error (e.g. a local one): nothing to learn.
					p.done = true
				}
			}
		}
		if reachedAt > 0 {
			// Every hop below the destination has had its chance once all
			// lower probes are answered or the wait is over.
			all := true
			for _, p := range probes {
				if p.ttl < reachedAt && !p.done {
					all = false
				}
			}
			if all {
				break
			}
		}
	}
	if reachedAt > 0 {
		t.Reached = true
		hops = hops[:reachedAt]
	} else {
		// Drop the trailing hops that never answered, keeping one to show
		// where the path goes silent.
		last := 0
		for i, h := range hops {
			if h.Addr != "" {
				last = i + 1
			}
		}
		hops = hops[:min(last+1, len(hops))]
	}
	t.Hops = hops
	return t
}

// parseRecvErr reads the IP_RECVERR control message: a sock_extended_err
// followed by the address of the node that sent the ICMP error.
func parseRecvErr(oob []byte, v4 bool) (net.IP, uint8, uint8, bool) {
	msgs, err := unix.ParseSocketControlMessage(oob)
	if err != nil {
		return nil, 0, 0, false
	}
	for _, m := range msgs {
		if v4 && !(m.Header.Level == unix.IPPROTO_IP && m.Header.Type == unix.IP_RECVERR) {
			continue
		}
		if !v4 && !(m.Header.Level == unix.IPPROTO_IPV6 && m.Header.Type == unix.IPV6_RECVERR) {
			continue
		}
		// struct sock_extended_err: u32 errno, u8 origin, u8 type, u8 code,
		// u8 pad, u32 info, u32 data (16 bytes), then the offender.
		d := m.Data
		if len(d) < 16 {
			continue
		}
		origin, typ, code := d[4], d[5], d[6]
		if origin != unix.SO_EE_ORIGIN_ICMP && origin != unix.SO_EE_ORIGIN_ICMP6 {
			continue
		}
		off := d[16:]
		if len(off) < 2 {
			continue
		}
		fam := binary.NativeEndian.Uint16(off[:2])
		switch {
		case fam == unix.AF_INET && len(off) >= 8:
			return net.IP(append([]byte(nil), off[4:8]...)), typ, code, true
		case fam == unix.AF_INET6 && len(off) >= 24:
			return net.IP(append([]byte(nil), off[8:24]...)), typ, code, true
		}
	}
	return nil, 0, 0, false
}

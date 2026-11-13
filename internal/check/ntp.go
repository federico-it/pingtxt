package check

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"net"
	"strconv"
	"strings"
	"time"
)

// DefaultNTPMaxOffsetMs is the largest clock offset an ntp check accepts
// when the monitor sets none.
const DefaultNTPMaxOffsetMs = 1000

// ntpEpoch is 1900-01-01, the zero of NTP timestamps.
var ntpEpoch = time.Date(1900, 1, 1, 0, 0, 0, 0, time.UTC)

// NTPTarget turns "host" or "host:port" into host:port (port 123 by default).
func NTPTarget(target string) (string, error) {
	t := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(target), "ntp://"))
	if t == "" || strings.ContainsAny(t, "/ @") {
		return "", errors.New("target must be host or host:port")
	}
	if h, p, err := net.SplitHostPort(t); err == nil {
		if h == "" || p == "" {
			return "", errors.New("target must be host or host:port")
		}
		return t, nil
	}
	if strings.Count(t, ":") == 1 {
		return "", errors.New("target must be host or host:port")
	}
	return net.JoinHostPort(StripBrackets(t), "123"), nil
}

// NTPMaxOffset reads the offset limit (ms) stored in ExpectedStatus.
func NTPMaxOffset(s string) (int, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return DefaultNTPMaxOffsetMs, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 || n > 3_600_000 {
		return 0, errors.New("the maximum offset must be 1 to 3600000 ms")
	}
	return n, nil
}

// runNTP asks an NTP server for the time (SNTP, RFC 4330). The check fails
// when the server does not answer, is not synchronized (stratum 0 or 16,
// leap indicator 3) or its clock is further than the maximum offset
// (ExpectedStatus, ms) from the clock of the location running the check.
func runNTP(ctx context.Context, j Job) Result {
	addr, err := NTPTarget(j.Target)
	if err != nil {
		return Result{Message: err.Error()}
	}
	maxOff, err := NTPMaxOffset(j.ExpectedStatus)
	if err != nil {
		return Result{Message: err.Error()}
	}
	start := time.Now()
	conn, err := j.dialContext(ctx, "udp", addr)
	if err != nil {
		return Result{LatencyMs: ms(time.Since(start)), Message: cleanErr(err)}
	}
	defer func() { _ = conn.Close() }()
	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	}
	req := make([]byte, 48)
	req[0] = 0<<6 | 4<<3 | 3 // no leap warning, version 4, client
	t1 := time.Now()
	binary.BigEndian.PutUint64(req[40:], toNTP(t1))
	if _, err := conn.Write(req); err != nil {
		return Result{LatencyMs: ms(time.Since(start)), Message: udpErr(err)}
	}
	buf := make([]byte, 512)
	for {
		n, err := conn.Read(buf)
		t4 := time.Now()
		if err != nil {
			return Result{LatencyMs: ms(time.Since(start)), Message: udpErr(err)}
		}
		// Ignore stray packets that do not answer our request.
		if n < 48 || binary.BigEndian.Uint64(buf[24:]) != binary.BigEndian.Uint64(req[40:]) {
			continue
		}
		return ntpResult(buf[:n], t1, t4, maxOff)
	}
}

func ntpResult(p []byte, t1, t4 time.Time, maxOffMs int) Result {
	lat := ms(t4.Sub(t1))
	leap, mode, stratum := p[0]>>6, p[0]&7, p[1]
	if mode != 4 && mode != 5 {
		return Result{LatencyMs: lat, Message: fmt.Sprintf("not an ntp server reply (mode %d)", mode)}
	}
	if stratum == 0 {
		code := strings.TrimRight(string(p[12:16]), "\x00")
		return Result{LatencyMs: lat, Message: fmt.Sprintf("server refused the request (kiss code %q)", code)}
	}
	if stratum >= 16 || leap == 3 {
		return Result{LatencyMs: lat, Message: "server clock not synchronized"}
	}
	t2 := fromNTP(binary.BigEndian.Uint64(p[32:]))
	t3 := fromNTP(binary.BigEndian.Uint64(p[40:]))
	offset := (t2.Sub(t1) + t3.Sub(t4)) / 2
	delay := t4.Sub(t1) - t3.Sub(t2)
	if delay < 0 {
		delay = 0
	}
	offMs := float64(offset.Microseconds()) / 1000
	msg := fmt.Sprintf("stratum %d, offset %+.1f ms", stratum, offMs)
	if math.Abs(offMs) > float64(maxOffMs) {
		return Result{LatencyMs: ms(delay), Message: fmt.Sprintf("%s, more than %d ms", msg, maxOffMs)}
	}
	return Result{OK: true, LatencyMs: ms(delay), Message: msg}
}

func toNTP(t time.Time) uint64 {
	d := t.Sub(ntpEpoch)
	sec := uint64(d / time.Second)
	frac := uint64(d%time.Second) << 32 / uint64(time.Second)
	return sec<<32 | frac
}

func fromNTP(v uint64) time.Time {
	sec, frac := v>>32, v&0xffffffff
	// Era 1 starts in 2036: timestamps with the top bit clear are after it.
	base := ntpEpoch
	if sec&0x80000000 == 0 {
		base = base.Add(time.Duration(1<<32) * time.Second)
	}
	return base.Add(time.Duration(sec)*time.Second + time.Duration(frac*uint64(time.Second)>>32))
}

package check

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"
)

// UDP payload encodings, stored in Job.Method.
const (
	UDPText = "text" // the payload and the expected reply are text ("" too)
	UDPHex  = "hex"  // both are hex bytes, e.g. "ff 00 1a"
)

// DecodeUDP turns a payload or expected reply into bytes. Hex accepts
// spaces, colons and an optional 0x prefix.
func DecodeUDP(s, encoding string) ([]byte, error) {
	if encoding != UDPHex {
		return []byte(s), nil
	}
	s = strings.TrimPrefix(strings.TrimSpace(s), "0x")
	s = strings.NewReplacer(" ", "", ":", "", "\n", "", "\r", "", "\t", "").Replace(s)
	b, err := hex.DecodeString(s)
	if err != nil {
		return nil, errors.New("invalid hex")
	}
	return b, nil
}

// runUDP sends the payload (Body) to host:port and waits for a reply. The
// check passes when a reply arrives and, if Keyword is set, contains it.
// UDP has no handshake, so a service that never answers cannot be told
// from one that is down: a reply is always required. "Port unreachable"
// from the host fails the check at once.
func runUDP(ctx context.Context, j Job) Result {
	payload, err := DecodeUDP(j.Body, j.Method)
	if err != nil {
		return Result{Message: "payload: " + err.Error()}
	}
	want, err := DecodeUDP(j.Keyword, j.Method)
	if err != nil {
		return Result{Message: "expected reply: " + err.Error()}
	}
	start := time.Now()
	conn, err := j.dialContext(ctx, "udp", j.Target)
	if err != nil {
		return Result{LatencyMs: ms(time.Since(start)), Message: cleanErr(err)}
	}
	defer conn.Close()
	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	}
	if _, err := conn.Write(payload); err != nil {
		return Result{LatencyMs: ms(time.Since(start)), Message: udpErr(err)}
	}
	buf := make([]byte, 64<<10)
	n, err := conn.Read(buf)
	lat := ms(time.Since(start))
	if err != nil {
		return Result{LatencyMs: lat, Message: udpErr(err)}
	}
	reply := buf[:n]
	if len(want) > 0 && !bytes.Contains(reply, want) {
		return Result{LatencyMs: lat, Message: fmt.Sprintf("reply does not contain %s: got %s", showUDP(want, j.Method), showUDP(reply, j.Method))}
	}
	return Result{OK: true, LatencyMs: lat, Message: fmt.Sprintf("reply %d bytes", n)}
}

func udpErr(err error) string {
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return "no reply (timeout)"
	}
	if strings.Contains(err.Error(), "connection refused") {
		return "port unreachable"
	}
	return cleanErr(err)
}

func showUDP(b []byte, encoding string) string {
	if len(b) > 40 {
		b = b[:40]
	}
	if encoding == UDPHex {
		return hex.EncodeToString(b)
	}
	return fmt.Sprintf("%q", b)
}

package check

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// runRedis sends AUTH (when the URL has credentials) and PING.
//
//	redis://[user:pass@]host[:6379][/db]   rediss:// for TLS
func runRedis(ctx context.Context, j Job) Result {
	u, err := url.Parse(j.Target)
	if err != nil || (u.Scheme != "redis" && u.Scheme != "rediss") || u.Host == "" {
		return Result{Message: "target must be a redis:// or rediss:// URL"}
	}
	addr := u.Host
	if u.Port() == "" {
		addr = net.JoinHostPort(u.Hostname(), "6379")
	}
	start := time.Now()
	conn, certExpiry, err := dial(ctx, j, addr, u.Scheme == "rediss", u.Hostname(), j.IgnoreTLS)
	if err != nil {
		return Result{LatencyMs: ms(time.Since(start)), Message: cleanErr(err)}
	}
	defer conn.Close()
	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	}
	rd := bufio.NewReader(conn)
	cmd := func(args ...string) (string, error) {
		var b strings.Builder
		fmt.Fprintf(&b, "*%d\r\n", len(args))
		for _, a := range args {
			fmt.Fprintf(&b, "$%d\r\n%s\r\n", len(a), a)
		}
		if _, err := conn.Write([]byte(b.String())); err != nil {
			return "", err
		}
		return readRESP(rd)
	}
	fail := func(err error) Result {
		return Result{LatencyMs: ms(time.Since(start)), CertExpiry: certExpiry, Message: cleanErr(err)}
	}
	if pass, ok := u.User.Password(); ok {
		args := []string{"AUTH", pass}
		if user := u.User.Username(); user != "" && user != "default" {
			args = []string{"AUTH", user, pass}
		}
		if _, err := cmd(args...); err != nil {
			return fail(err)
		}
	}
	if db := strings.Trim(u.Path, "/"); db != "" && db != "0" {
		if _, err := cmd("SELECT", db); err != nil {
			return fail(err)
		}
	}
	pong, err := cmd("PING")
	if err != nil {
		return fail(err)
	}
	_, _ = cmd("QUIT")
	return Result{OK: pong == "PONG", LatencyMs: ms(time.Since(start)), CertExpiry: certExpiry, Message: pong}
}

// readRESP reads one simple reply; errors from the server become Go errors.
func readRESP(rd *bufio.Reader) (string, error) {
	line, err := rd.ReadString('\n')
	if err != nil {
		return "", err
	}
	line = strings.TrimRight(line, "\r\n")
	if line == "" {
		return "", errors.New("empty reply")
	}
	switch line[0] {
	case '+', ':':
		return line[1:], nil
	case '-':
		return "", errors.New(line[1:])
	case '$':
		n, err := strconv.Atoi(line[1:])
		if err != nil || n < 0 {
			return "", err
		}
		buf := make([]byte, n+2)
		if _, err := io.ReadFull(rd, buf); err != nil {
			return "", err
		}
		return string(buf[:n]), nil
	}
	return "", fmt.Errorf("unexpected reply %q", truncate(line, 40))
}

// dial opens a TCP connection, optionally wrapped in TLS, and returns the
// leaf certificate expiry (unix seconds) when TLS is used.
func dial(ctx context.Context, j Job, addr string, useTLS bool, serverName string, insecure bool) (net.Conn, int64, error) {
	conn, err := j.dialContext(ctx, "tcp", addr)
	if err != nil || !useTLS {
		return conn, 0, err
	}
	return startTLS(ctx, conn, serverName, insecure)
}

func startTLS(ctx context.Context, conn net.Conn, serverName string, insecure bool) (net.Conn, int64, error) {
	tc := tls.Client(conn, &tls.Config{ServerName: serverName, InsecureSkipVerify: insecure}) //nolint:gosec // user opt-in
	if err := tc.HandshakeContext(ctx); err != nil {
		_ = conn.Close()
		return nil, 0, err
	}
	var exp int64
	if certs := tc.ConnectionState().PeerCertificates; len(certs) > 0 {
		exp = certs[0].NotAfter.Unix()
	}
	return tc, exp, nil
}

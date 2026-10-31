package check

import (
	"context"
	"errors"
	"net"
	"net/textproto"
	"net/url"
	"strings"
	"time"
)

// runSMTP reads the greeting, sends EHLO and upgrades with STARTTLS when the
// server offers it (port 25/587). smtps:// uses implicit TLS (port 465).
//
//	smtp://host[:25]   smtps://host[:465]   host:port
func runSMTP(ctx context.Context, j Job) Result {
	host, port, implicitTLS, err := parseSMTPTarget(j.Target)
	if err != nil {
		return Result{Message: err.Error()}
	}
	addr := net.JoinHostPort(host, port)
	start := time.Now()
	conn, certExpiry, err := dial(ctx, j, addr, implicitTLS, host, j.IgnoreTLS)
	if err != nil {
		return Result{LatencyMs: ms(time.Since(start)), Message: cleanErr(err)}
	}
	defer func() { _ = conn.Close() }()
	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	}
	fail := func(err error) Result {
		if ctx.Err() != nil {
			err = context.DeadlineExceeded
		}
		return Result{LatencyMs: ms(time.Since(start)), CertExpiry: certExpiry, Message: cleanErr(err)}
	}
	tp := textproto.NewConn(conn)
	_, greeting, err := tp.ReadResponse(220)
	if err != nil {
		return fail(err)
	}
	ehlo := func() (string, error) {
		if err := tp.PrintfLine("EHLO pingtxt.local"); err != nil {
			return "", err
		}
		_, msg, err := tp.ReadResponse(250)
		return msg, err
	}
	exts, err := ehlo()
	if err != nil {
		return fail(err)
	}
	secured := implicitTLS
	if !implicitTLS && hasExt(exts, "STARTTLS") {
		if err := tp.PrintfLine("STARTTLS"); err != nil {
			return fail(err)
		}
		if _, _, err := tp.ReadResponse(220); err != nil {
			return fail(errors.New("starttls: " + err.Error()))
		}
		tc, exp, err := startTLS(ctx, conn, host, j.IgnoreTLS)
		if err != nil {
			return fail(errors.New("starttls: " + cleanErr(err)))
		}
		conn, certExpiry, secured = tc, exp, true
		tp = textproto.NewConn(conn)
		if _, err := ehlo(); err != nil {
			return fail(err)
		}
	}
	_ = tp.PrintfLine("QUIT")
	msg := strings.SplitN(greeting, "\n", 2)[0]
	if secured {
		msg = "tls, " + msg
	} else {
		msg = "no tls, " + msg
	}
	return Result{OK: true, LatencyMs: ms(time.Since(start)), CertExpiry: certExpiry, Message: msg}
}

func hasExt(ehlo, ext string) bool {
	for _, l := range strings.Split(ehlo, "\n") {
		if f := strings.Fields(l); len(f) > 0 && strings.EqualFold(f[0], ext) {
			return true
		}
	}
	return false
}

func parseSMTPTarget(target string) (host, port string, implicitTLS bool, err error) {
	t := strings.TrimSpace(target)
	if !strings.Contains(t, "://") {
		t = "smtp://" + BracketIPv6(t)
	}
	u, perr := url.Parse(t)
	if perr != nil || (u.Scheme != "smtp" && u.Scheme != "smtps") || u.Hostname() == "" {
		return "", "", false, errors.New("target must be smtp://host[:port] or smtps://host[:port]")
	}
	port = u.Port()
	if port == "" {
		port = "25"
		if u.Scheme == "smtps" {
			port = "465"
		}
	}
	return u.Hostname(), port, u.Scheme == "smtps", nil
}

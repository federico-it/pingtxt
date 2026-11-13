package check

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// mailTarget is a parsed imap(s):// or pop3(s):// target.
type mailTarget struct {
	host, port  string
	implicitTLS bool
	user, pass  string
}

// ParseMailboxTarget parses the target of an imap or pop3 monitor:
//
//	imap://[user:pass@]host[:143]   imaps://host[:993]   host[:port]
//	pop3://[user:pass@]host[:110]   pop3s://host[:995]
func ParseMailboxTarget(typ, target string) (mailTarget, error) {
	plain, secure, plainPort, securePort := "imap", "imaps", "143", "993"
	if typ == TypePOP3 {
		plain, secure, plainPort, securePort = "pop3", "pop3s", "110", "995"
	}
	bad := fmt.Errorf("target must be %s://host[:port] or %s://host[:port]", plain, secure)
	t := strings.TrimSpace(target)
	if !strings.Contains(t, "://") {
		t = plain + "://" + BracketIPv6(t)
	}
	u, err := url.Parse(t)
	if err != nil || (u.Scheme != plain && u.Scheme != secure) || u.Hostname() == "" || strings.Trim(u.Path, "/") != "" {
		return mailTarget{}, bad
	}
	mt := mailTarget{host: u.Hostname(), port: u.Port(), implicitTLS: u.Scheme == secure}
	if mt.port == "" {
		mt.port = plainPort
		if mt.implicitTLS {
			mt.port = securePort
		}
	}
	if u.User != nil {
		mt.user = u.User.Username()
		mt.pass, _ = u.User.Password()
	}
	return mt, nil
}

// runMailbox checks an IMAP or POP3 server: it reads the greeting, upgrades
// to TLS when the server offers it (STARTTLS, STLS) and, when the target has
// a user and password, logs in. It never sends a password without TLS.
func runMailbox(ctx context.Context, j Job) Result {
	mt, err := ParseMailboxTarget(j.Type, j.Target)
	if err != nil {
		return Result{Message: err.Error()}
	}
	start := time.Now()
	conn, certExpiry, err := dial(ctx, j, net.JoinHostPort(mt.host, mt.port), mt.implicitTLS, mt.host, j.IgnoreTLS)
	if err != nil {
		return Result{LatencyMs: ms(time.Since(start)), Message: cleanErr(err)}
	}
	defer func() { _ = conn.Close() }()
	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	}
	s := &mailSession{conn: conn, br: bufio.NewReader(conn), ctx: ctx, mt: mt, j: j, secured: mt.implicitTLS, certExpiry: certExpiry}
	var msg string
	if j.Type == TypePOP3 {
		msg, err = s.pop3()
	} else {
		msg, err = s.imap()
	}
	r := Result{LatencyMs: ms(time.Since(start)), CertExpiry: s.certExpiry}
	if err != nil {
		if ctx.Err() != nil {
			err = context.DeadlineExceeded
		}
		r.Message = cleanErr(err)
		return r
	}
	parts := []string{"no tls"}
	if s.secured {
		parts[0] = "tls"
	}
	if mt.user != "" {
		parts = append(parts, "logged in")
	}
	r.OK = true
	r.Message = strings.Join(append(parts, msg), ", ")
	return r
}

type mailSession struct {
	conn       net.Conn
	br         *bufio.Reader
	ctx        context.Context
	mt         mailTarget
	j          Job
	secured    bool
	certExpiry int64
	tag        int
}

func (s *mailSession) line() (string, error) {
	l, err := s.br.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimRight(l, "\r\n"), nil
}

func (s *mailSession) send(line string) error {
	_, err := s.conn.Write([]byte(line + "\r\n"))
	return err
}

func (s *mailSession) upgrade() error {
	tc, exp, err := startTLS(s.ctx, s.conn, s.mt.host, s.j.IgnoreTLS)
	if err != nil {
		return errors.New("starttls: " + cleanErr(err))
	}
	s.conn, s.certExpiry, s.secured = tc, exp, true
	s.br = bufio.NewReader(tc)
	return nil
}

var errClearPassword = errors.New("the server offers no tls: not sending the password in clear text")

// imap runs: greeting, CAPABILITY, STARTTLS, LOGIN, LOGOUT.
func (s *mailSession) imap() (string, error) {
	greeting, err := s.line()
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(greeting, "* OK") && !strings.HasPrefix(greeting, "* PREAUTH") {
		return "", fmt.Errorf("unexpected greeting %q", truncate(greeting, 80))
	}
	if !s.secured {
		lines, err := s.imapCmd("CAPABILITY")
		if err != nil {
			return "", err
		}
		if strings.Contains(strings.ToUpper(strings.Join(lines, " ")), "STARTTLS") {
			if _, err := s.imapCmd("STARTTLS"); err != nil {
				return "", errors.New("starttls: " + err.Error())
			}
			if err := s.upgrade(); err != nil {
				return "", err
			}
		}
	}
	if s.mt.user != "" {
		if !s.secured {
			return "", errClearPassword
		}
		if _, err := s.imapCmd("LOGIN " + imapQuote(s.mt.user) + " " + imapQuote(s.mt.pass)); err != nil {
			return "", errors.New("login: " + err.Error())
		}
	}
	_, _ = s.imapCmd("LOGOUT")
	return greeting, nil
}

// imapCmd sends a tagged command and reads up to its tagged reply; a reply
// other than OK is an error.
func (s *mailSession) imapCmd(cmd string) ([]string, error) {
	s.tag++
	tag := "p" + strconv.Itoa(s.tag)
	if err := s.send(tag + " " + cmd); err != nil {
		return nil, err
	}
	var lines []string
	for range 1000 {
		l, err := s.line()
		if err != nil {
			return lines, err
		}
		if rest, ok := strings.CutPrefix(l, tag+" "); ok {
			if strings.HasPrefix(strings.ToUpper(rest), "OK") {
				return lines, nil
			}
			return lines, errors.New(truncate(rest, 120))
		}
		lines = append(lines, l)
	}
	return lines, errors.New("reply too long")
}

func imapQuote(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}

// pop3 runs: greeting, CAPA, STLS, USER/PASS, QUIT.
func (s *mailSession) pop3() (string, error) {
	greeting, err := s.line()
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(greeting, "+OK") {
		return "", fmt.Errorf("unexpected greeting %q", truncate(greeting, 80))
	}
	if !s.secured {
		// CAPA is optional (RFC 2449): an -ERR reply just means no STLS.
		if caps, err := s.pop3Multi("CAPA"); err == nil && hasExt(strings.Join(caps, "\n"), "STLS") {
			if _, err := s.pop3Cmd("STLS"); err != nil {
				return "", errors.New("stls: " + err.Error())
			}
			if err := s.upgrade(); err != nil {
				return "", err
			}
		}
	}
	if s.mt.user != "" {
		if !s.secured {
			return "", errClearPassword
		}
		if _, err := s.pop3Cmd("USER " + s.mt.user); err != nil {
			return "", errors.New("login: " + err.Error())
		}
		if _, err := s.pop3Cmd("PASS " + s.mt.pass); err != nil {
			return "", errors.New("login: " + err.Error())
		}
	}
	_, _ = s.pop3Cmd("QUIT")
	return greeting, nil
}

func (s *mailSession) pop3Cmd(cmd string) (string, error) {
	if err := s.send(cmd); err != nil {
		return "", err
	}
	l, err := s.line()
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(l, "+OK") {
		return l, errors.New(truncate(l, 120))
	}
	return l, nil
}

// pop3Multi sends a command with a multi-line reply ending in ".".
func (s *mailSession) pop3Multi(cmd string) ([]string, error) {
	if _, err := s.pop3Cmd(cmd); err != nil {
		return nil, err
	}
	var lines []string
	for range 1000 {
		l, err := s.line()
		if err != nil {
			return lines, err
		}
		if l == "." {
			return lines, nil
		}
		lines = append(lines, l)
	}
	return lines, errors.New("reply too long")
}

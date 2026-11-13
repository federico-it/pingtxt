package check

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

// sshHostKeyAlgos are tried one by one when the host key of the first
// handshake is not the pinned one: a server usually has several keys (ed25519,
// ecdsa, rsa) and the pinned fingerprint can be any of them.
var sshHostKeyAlgos = []string{
	ssh.KeyAlgoED25519, ssh.KeyAlgoECDSA256, ssh.KeyAlgoECDSA384, ssh.KeyAlgoECDSA521, ssh.KeyAlgoRSASHA512,
}

// errGotKey stops the SSH handshake once the host key is known: the check
// never logs in.
var errGotKey = errors.New("got host key")

// SSHTarget turns "host", "host:port" or "ssh://host[:port]" into host:port
// (port 22 by default).
func SSHTarget(target string) (string, error) {
	t := strings.TrimSpace(target)
	t = strings.TrimPrefix(t, "ssh://")
	t = strings.TrimSuffix(t, "/")
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
	return net.JoinHostPort(StripBrackets(t), "22"), nil
}

// runSSH connects to an SSH server, reads its version banner and runs the
// key exchange up to the server's host key, without logging in. Keyword is
// either text the banner must contain or a host key fingerprint
// ("SHA256:..."): with a fingerprint the check fails when no host key of the
// server matches it.
func runSSH(ctx context.Context, j Job) Result {
	addr, err := SSHTarget(j.Target)
	if err != nil {
		return Result{Message: err.Error()}
	}
	want := strings.TrimSpace(j.Keyword)
	pin := strings.HasPrefix(want, "SHA256:")
	start := time.Now()
	banner, key, err := sshHandshake(ctx, j, addr, nil)
	if err != nil {
		return Result{LatencyMs: ms(time.Since(start)), Message: err.Error()}
	}
	lat := ms(time.Since(start))
	if !pin && want != "" && !strings.Contains(banner, want) {
		return Result{LatencyMs: lat, Message: fmt.Sprintf("banner does not contain %q: %s", want, banner)}
	}
	fp := ssh.FingerprintSHA256(key)
	if pin && fp != want {
		// The server picked another of its keys: ask for each type in turn.
		found := false
		for _, algo := range sshHostKeyAlgos {
			if algoMatches(key.Type(), algo) || ctx.Err() != nil {
				continue
			}
			_, k, err := sshHandshake(ctx, j, addr, []string{algo})
			if err != nil {
				continue
			}
			if ssh.FingerprintSHA256(k) == want {
				key, fp, found = k, want, true
				break
			}
		}
		if !found {
			return Result{LatencyMs: lat, Message: fmt.Sprintf("host key changed: %s %s, expected %s", key.Type(), ssh.FingerprintSHA256(key), want)}
		}
	}
	return Result{OK: true, LatencyMs: lat, Message: fmt.Sprintf("%s, %s %s", banner, key.Type(), fp)}
}

func algoMatches(keyType, algo string) bool {
	if keyType == ssh.KeyAlgoRSA {
		return algo == ssh.KeyAlgoRSASHA512 || algo == ssh.KeyAlgoRSASHA256
	}
	return keyType == algo
}

// sshHandshake returns the server banner and the host key it presented.
func sshHandshake(ctx context.Context, j Job, addr string, algos []string) (string, ssh.PublicKey, error) {
	conn, err := j.dialContext(ctx, "tcp", addr)
	if err != nil {
		return "", nil, errors.New(cleanErr(err))
	}
	defer func() { _ = conn.Close() }()
	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	}
	rc := &bannerConn{Conn: conn, br: bufio.NewReader(conn)}
	var key ssh.PublicKey
	cfg := &ssh.ClientConfig{
		User:              "pingtxt",
		ClientVersion:     "SSH-2.0-pingtxt",
		HostKeyAlgorithms: algos,
		HostKeyCallback: func(_ string, _ net.Addr, k ssh.PublicKey) error {
			key = k
			return errGotKey
		},
	}
	_, _, _, err = ssh.NewClientConn(rc, addr, cfg)
	banner := strings.TrimSpace(rc.banner)
	if key != nil {
		return banner, key, nil
	}
	if banner != "" && !strings.HasPrefix(banner, "SSH-") {
		return banner, nil, fmt.Errorf("not an ssh server: %q", truncate(banner, 60))
	}
	if ctx.Err() != nil {
		return banner, nil, errors.New("timeout")
	}
	if banner == "" {
		if err != nil {
			return "", nil, fmt.Errorf("no ssh banner: %s", cleanErr(err))
		}
		return "", nil, errors.New("no ssh banner")
	}
	return banner, nil, fmt.Errorf("key exchange: %s", cleanErr(err))
}

// bannerConn records the server's version line, the first line it sends
// that starts with "SSH-" (servers may send other lines before it).
type bannerConn struct {
	net.Conn
	br     *bufio.Reader
	banner string
	line   []byte
	done   bool
}

func (c *bannerConn) Read(p []byte) (int, error) {
	n, err := c.br.Read(p)
	if !c.done {
		for _, b := range p[:n] {
			if b == '\n' {
				l := strings.TrimSpace(string(c.line))
				c.line = c.line[:0]
				if c.banner == "" || !strings.HasPrefix(c.banner, "SSH-") {
					c.banner = l
				}
				if strings.HasPrefix(l, "SSH-") {
					c.done = true
					break
				}
				continue
			}
			if len(c.line) < 255 {
				c.line = append(c.line, b)
			}
		}
	}
	return n, err
}

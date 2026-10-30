package hub

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/mail"
	"strconv"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/federico-it/pingtxt/internal/store"
)

// fakeSMTP is a minimal SMTP server for tests.
type fakeSMTP struct {
	ln       net.Listener
	tls      *tls.Config
	implicit bool // TLS from the first byte (port 465 style)
	starttls bool // offer STARTTLS
	user     string
	pass     string

	mu    sync.Mutex
	mails []fakeMail
}

type fakeMail struct {
	from, auth string
	to         []string
	data       string
	tls        bool
}

func newFakeSMTP(t *testing.T, implicit, starttls bool) (*fakeSMTP, *tls.Config) {
	t.Helper()
	// Borrow the self-signed certificate of an httptest TLS server.
	hs := httptest.NewTLSServer(http.NotFoundHandler())
	t.Cleanup(hs.Close)
	pool := x509.NewCertPool()
	pool.AddCert(hs.Certificate())

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := &fakeSMTP{ln: ln, tls: &tls.Config{Certificates: hs.TLS.Certificates}, implicit: implicit, starttls: starttls, user: "alice", pass: randomPass()}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go s.serve(c)
		}
	}()
	return s, &tls.Config{RootCAs: pool}
}

// randomPass makes a throwaway credential for the fake server, so no
// password literal lives in the tests.
func randomPass() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func (s *fakeSMTP) port() string {
	return strconv.Itoa(s.ln.Addr().(*net.TCPAddr).Port)
}

func (s *fakeSMTP) serve(c net.Conn) {
	defer c.Close()
	secure := false
	if s.implicit {
		c = tls.Server(c, s.tls)
		secure = true
	}
	r := bufio.NewReader(c)
	say := func(line string) { io.WriteString(c, line+"\r\n") }
	var m fakeMail
	say("220 fake ESMTP")
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		cmd := strings.ToUpper(strings.SplitN(line, " ", 2)[0])
		switch cmd {
		case "EHLO":
			say("250-fake")
			if s.starttls && !secure {
				say("250-STARTTLS")
			}
			say("250 AUTH PLAIN")
		case "STARTTLS":
			say("220 go ahead")
			c = tls.Server(c, s.tls)
			r = bufio.NewReader(c)
			secure = true
		case "AUTH":
			parts := strings.Fields(line)
			raw, _ := base64.StdEncoding.DecodeString(parts[len(parts)-1])
			f := strings.Split(string(raw), "\x00")
			if len(f) != 3 || f[1] != s.user || f[2] != s.pass {
				say("535 bad credentials")
				continue
			}
			m.auth = f[1]
			say("235 ok")
		case "MAIL":
			m.from = strings.Trim(strings.TrimPrefix(line[5:], "FROM:"), "<>")
			say("250 ok")
		case "RCPT":
			m.to = append(m.to, strings.Trim(strings.TrimPrefix(line[5:], "TO:"), "<>"))
			say("250 ok")
		case "DATA":
			say("354 send")
			var b strings.Builder
			for {
				l, err := r.ReadString('\n')
				if err != nil {
					return
				}
				if l == ".\r\n" {
					break
				}
				b.WriteString(l)
			}
			m.data, m.tls = b.String(), secure
			s.mu.Lock()
			s.mails = append(s.mails, m)
			s.mu.Unlock()
			m = fakeMail{}
			say("250 queued")
		case "QUIT":
			say("221 bye")
			return
		default:
			say("250 ok")
		}
	}
}

func (s *fakeSMTP) received() []fakeMail {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]fakeMail(nil), s.mails...)
}

func TestEmailNotifierAPI(t *testing.T) {
	h, _ := newTestHub(t)
	smtpSrv, clientTLS := newFakeSMTP(t, false, true)
	h.notify.tlsConfig = clientTLS
	srv := httptest.NewServer(h.Handler(fstest.MapFS{}))
	defer srv.Close()

	c := newClient(t, srv.URL)
	if res := c.do("POST", "/api/setup", map[string]string{"email": "admin@example.com", "password": "password123"}); res.StatusCode != 201 {
		t.Fatalf("setup: %d", res.StatusCode)
	}
	cfg := map[string]string{
		"host": "127.0.0.1", "port": smtpSrv.port(), "security": "starttls",
		"username": smtpSrv.user, "password": smtpSrv.pass,
		"from": "Pingtxt <alerts@example.com>", "to": "ops@example.com; Bob <bob@example.com>",
	}
	body := map[string]any{"name": "mail", "type": "email", "enabled": true, "config": cfg}

	// Test before saving.
	if res := c.do("POST", "/api/notifiers/test", body); res.StatusCode != 200 {
		b, _ := io.ReadAll(res.Body)
		t.Fatalf("draft test: %d %s", res.StatusCode, b)
	}

	res := c.do("POST", "/api/notifiers", body)
	if res.StatusCode != 201 {
		b, _ := io.ReadAll(res.Body)
		t.Fatalf("create: %d %s", res.StatusCode, b)
	}
	var n store.Notifier
	json.NewDecoder(res.Body).Decode(&n)
	if _, ok := n.Config["password"]; ok || n.Config["password_set"] != "true" {
		t.Fatalf("password must not be returned: %+v", n.Config)
	}

	// Saving with an empty password keeps the stored one.
	cfg["password"] = ""
	if res := c.do("PUT", "/api/notifiers/"+strconv.FormatInt(n.ID, 10), body); res.StatusCode != 200 {
		t.Fatalf("update: %d", res.StatusCode)
	}
	if stored, _ := h.store.Notifier(n.ID); stored.Config["password"] != smtpSrv.pass || stored.Config["password_set"] != "" {
		t.Fatalf("stored config: %+v", stored.Config)
	}
	res = c.do("GET", "/api/notifiers", nil)
	var list []store.Notifier
	json.NewDecoder(res.Body).Decode(&list)
	if len(list) != 1 || list[0].Config["password"] != "" {
		t.Fatalf("list leaks password: %+v", list)
	}

	if res := c.do("POST", "/api/notifiers/"+strconv.FormatInt(n.ID, 10)+"/test", nil); res.StatusCode != 200 {
		b, _ := io.ReadAll(res.Body)
		t.Fatalf("test: %d %s", res.StatusCode, b)
	}

	mails := smtpSrv.received()
	if len(mails) != 2 {
		t.Fatalf("want 2 mails, got %d", len(mails))
	}
	m := mails[1]
	if !m.tls || m.auth != smtpSrv.user || m.from != "alerts@example.com" || strings.Join(m.to, ",") != "ops@example.com,bob@example.com" {
		t.Fatalf("envelope: %+v", m)
	}
	msg, err := mail.ReadMessage(strings.NewReader(m.data))
	if err != nil {
		t.Fatal(err)
	}
	if subj := msg.Header.Get("Subject"); subj != "[test] [pingtxt] example.com is DOWN" {
		t.Fatalf("subject: %q", subj)
	}
	if to := msg.Header.Get("To"); !strings.Contains(to, `"Bob" <bob@example.com>`) {
		t.Fatalf("to: %q", to)
	}
	b, _ := io.ReadAll(msg.Body)
	if !strings.Contains(string(b), "this is a test notification") {
		t.Fatalf("body: %s", b)
	}

	// A wrong password is reported to the caller.
	cfg["password"] = randomPass()
	res = c.do("POST", "/api/notifiers/test", body)
	if res.StatusCode != 502 {
		t.Fatalf("bad password: want 502, got %d", res.StatusCode)
	}
}

func TestEmailImplicitTLS(t *testing.T) {
	smtpSrv, clientTLS := newFakeSMTP(t, true, false)
	n := &Notifier{tlsConfig: clientTLS}
	cfg := map[string]string{"host": "127.0.0.1", "port": smtpSrv.port(), "security": "tls", "from": "a@example.com", "to": "b@example.com"}
	ev := Event{Monitor: "api", Target: "https://api.example.com", Status: StatusUp, At: time.Now().UnixMilli()}
	if err := n.sendEmail(context.Background(), cfg, ev); err != nil {
		t.Fatal(err)
	}
	if m := smtpSrv.received(); len(m) != 1 || !m[0].tls || m[0].auth != "" {
		t.Fatalf("got %+v", m)
	}
}

func TestEmailRequiresStartTLS(t *testing.T) {
	smtpSrv, clientTLS := newFakeSMTP(t, false, false)
	n := &Notifier{tlsConfig: clientTLS}
	cfg := map[string]string{"host": "127.0.0.1", "port": smtpSrv.port(), "security": "starttls", "username": smtpSrv.user, "password": smtpSrv.pass, "from": "a@example.com", "to": "b@example.com"}
	err := n.sendEmail(context.Background(), cfg, Event{Monitor: "x", Status: StatusDown})
	if err == nil || !strings.Contains(err.Error(), "STARTTLS") {
		t.Fatalf("want STARTTLS error, got %v", err)
	}
	if len(smtpSrv.received()) != 0 {
		t.Fatal("mail must not be sent without TLS")
	}
}

func TestEmailConfigValidation(t *testing.T) {
	base := func() map[string]string {
		return map[string]string{"host": "smtp.example.com", "from": "a@example.com", "to": "b@example.com"}
	}
	c := base()
	if _, err := parseEmailConfig(c); err != nil {
		t.Fatal(err)
	}
	if c["security"] != "starttls" || c["port"] != "587" {
		t.Fatalf("defaults: %+v", c)
	}
	bad := []func(map[string]string){
		func(c map[string]string) { c["host"] = "" },
		func(c map[string]string) { c["from"] = "not an address" },
		func(c map[string]string) { c["to"] = "" },
		func(c map[string]string) { c["to"] = "b@example.com\r\nBcc: evil@example.com" },
		func(c map[string]string) { c["port"] = "99999" },
		func(c map[string]string) { c["security"] = "ssl3" },
	}
	for i, f := range bad {
		c := base()
		f(c)
		if _, err := parseEmailConfig(c); err == nil {
			t.Errorf("case %d: want error for %+v", i, c)
		}
	}
}

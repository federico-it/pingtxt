package hub

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/smtp"
	"strconv"
	"strings"
	"time"
)

// Email channel config keys: host, port, security, username, password, from, to.
//
// security is "starttls" (default, usually port 587), "tls" (implicit TLS,
// usually port 465) or "none" (plain SMTP, e.g. a local relay on port 25).
// The password is write-only: the API never returns it.

const emailTimeout = 20 * time.Second

type emailConfig struct {
	Host     string
	Port     int
	Security string
	Username string
	Password string
	From     *mail.Address
	To       []*mail.Address
}

func defaultSMTPPort(security string) int {
	switch security {
	case "tls":
		return 465
	case "none":
		return 25
	}
	return 587
}

// parseEmailConfig validates the channel config and normalizes it in place.
func parseEmailConfig(c map[string]string) (*emailConfig, error) {
	for k, v := range c {
		c[k] = strings.TrimSpace(v)
		if k != "password" && strings.ContainsAny(v, "\r\n") {
			return nil, fmt.Errorf("%s contains a line break", k)
		}
	}
	ec := &emailConfig{Host: c["host"], Security: c["security"], Username: c["username"], Password: c["password"]}
	if ec.Host == "" {
		return nil, errors.New("smtp host is required")
	}
	if ec.Security == "" {
		ec.Security = "starttls"
	}
	if ec.Security != "starttls" && ec.Security != "tls" && ec.Security != "none" {
		return nil, errors.New("security must be starttls, tls or none")
	}
	c["security"] = ec.Security
	if c["port"] == "" {
		ec.Port = defaultSMTPPort(ec.Security)
		c["port"] = strconv.Itoa(ec.Port)
	} else {
		p, err := strconv.Atoi(c["port"])
		if err != nil || p < 1 || p > 65535 {
			return nil, errors.New("invalid smtp port")
		}
		ec.Port = p
	}
	from, err := mail.ParseAddress(c["from"])
	if err != nil {
		return nil, errors.New("a valid from address is required")
	}
	ec.From = from
	list, err := mail.ParseAddressList(strings.ReplaceAll(c["to"], ";", ","))
	if err != nil || len(list) == 0 {
		return nil, errors.New("at least one valid recipient is required")
	}
	ec.To = list
	return ec, nil
}

func emailSubject(e Event) string {
	prefix := ""
	if e.Test {
		prefix = "[test] "
	}
	if e.Kind == KindCert || e.Kind == KindDomain {
		// e.g. "[pingtxt] example.com certificate expiring"
		return prefix + "[pingtxt] " + strings.TrimPrefix(e.Title(), "pingtxt: ")
	}
	return fmt.Sprintf("%s[pingtxt] %s is %s", prefix, e.Monitor, strings.ToUpper(e.Status))
}

func newMessageID(host string) string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return "<" + hex.EncodeToString(b) + "@" + host + ">"
}

// buildEmail renders a plain text, quoted-printable encoded message.
func buildEmail(ec *emailConfig, e Event, now time.Time) []byte {
	to := make([]string, len(ec.To))
	for i, a := range ec.To {
		to[i] = a.String()
	}
	domain := ec.From.Address[strings.LastIndexByte(ec.From.Address, '@')+1:]
	var b bytes.Buffer
	hdr := func(k, v string) { b.WriteString(k + ": " + v + "\r\n") }
	hdr("From", ec.From.String())
	hdr("To", strings.Join(to, ", "))
	hdr("Subject", mime.QEncoding.Encode("utf-8", emailSubject(e)))
	hdr("Date", now.Format(time.RFC1123Z))
	hdr("Message-ID", newMessageID(domain))
	hdr("MIME-Version", "1.0")
	hdr("Content-Type", "text/plain; charset=utf-8")
	hdr("Content-Transfer-Encoding", "quoted-printable")
	hdr("Auto-Submitted", "auto-generated")
	b.WriteString("\r\n")

	body := e.Text()
	if e.Target != "" {
		body += "\n\nmonitor: " + e.Monitor + "\ntarget:  " + e.Target
	}
	body += "\n\n-- \nsent by pingtxt\n"
	qp := quotedprintable.NewWriter(&b)
	_, _ = qp.Write([]byte(strings.ReplaceAll(body, "\n", "\r\n")))
	_ = qp.Close()
	return b.Bytes()
}

// sendEmail delivers the event over SMTP.
func (n *Notifier) sendEmail(ctx context.Context, c map[string]string, e Event) error {
	cfg := make(map[string]string, len(c))
	for k, v := range c {
		cfg[k] = v
	}
	ec, err := parseEmailConfig(cfg)
	if err != nil {
		return err
	}
	return n.deliverEmail(ctx, ec, buildEmail(ec, e, time.Now()))
}

// deliverEmail sends a complete message to the recipients of ec over SMTP.
func (n *Notifier) deliverEmail(ctx context.Context, ec *emailConfig, msg []byte) error {
	ctx, cancel := context.WithTimeout(ctx, emailTimeout)
	defer cancel()

	addr := net.JoinHostPort(ec.Host, strconv.Itoa(ec.Port))
	tlsCfg := &tls.Config{ServerName: ec.Host, MinVersion: tls.VersionTLS12}
	if n.tlsConfig != nil {
		tlsCfg = n.tlsConfig.Clone()
		tlsCfg.ServerName = ec.Host
	}

	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("connect %s: %w", addr, err)
	}
	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	}
	// Close the connection if the context is cancelled mid-conversation.
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()

	if ec.Security == "tls" {
		tc := tls.Client(conn, tlsCfg)
		if err := tc.HandshakeContext(ctx); err != nil {
			conn.Close()
			return fmt.Errorf("tls handshake: %w", err)
		}
		conn = tc
	}
	cl, err := smtp.NewClient(conn, ec.Host)
	if err != nil {
		conn.Close()
		return fmt.Errorf("smtp greeting: %w", err)
	}
	defer cl.Close()

	if err := cl.Hello(heloName); err != nil {
		return fmt.Errorf("smtp hello: %w", err)
	}
	if ec.Security == "starttls" {
		if ok, _ := cl.Extension("STARTTLS"); !ok {
			return errors.New("server does not support STARTTLS (try security \"tls\" or \"none\")")
		}
		if err := cl.StartTLS(tlsCfg); err != nil {
			return fmt.Errorf("starttls: %w", err)
		}
	}
	if ec.Username != "" {
		if ok, _ := cl.Extension("AUTH"); !ok {
			return errors.New("server does not support authentication")
		}
		// PlainAuth refuses to send credentials over an unencrypted
		// connection, except to localhost.
		if err := cl.Auth(smtp.PlainAuth("", ec.Username, ec.Password, ec.Host)); err != nil {
			return fmt.Errorf("smtp auth: %w", err)
		}
	}
	if err := cl.Mail(ec.From.Address); err != nil {
		return fmt.Errorf("mail from: %w", err)
	}
	for _, a := range ec.To {
		if err := cl.Rcpt(a.Address); err != nil {
			return fmt.Errorf("rcpt to %s: %w", a.Address, err)
		}
	}
	w, err := cl.Data()
	if err != nil {
		return fmt.Errorf("data: %w", err)
	}
	if _, err := w.Write(msg); err != nil {
		return fmt.Errorf("data: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("data: %w", err)
	}
	_ = cl.Quit()
	return nil
}

// heloName is sent in EHLO; servers only log it.
const heloName = "pingtxt.localhost"

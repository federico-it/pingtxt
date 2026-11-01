package hub

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/mail"
	"net/smtp"
	"net/textproto"
	"strconv"
	"strings"
	"time"
)

// Email channel config keys: provider, host, port, security, username,
// password, api_key, from, to.
//
// provider is "smtp" (default, stored as no key) or "resend": Resend sends
// over its HTTP API with api_key, and the SMTP keys are dropped.
//
// security is "starttls" (default, usually port 587), "tls" (implicit TLS,
// usually port 465) or "none" (plain SMTP, e.g. a local relay on port 25).
// The password and the api key are write-only: the API never returns them.

const emailTimeout = 20 * time.Second

type emailConfig struct {
	Provider string // "smtp" or "resend"
	APIKey   string // resend only
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
	ec := &emailConfig{Provider: c["provider"]}
	switch ec.Provider {
	case "", "smtp":
		ec.Provider = "smtp"
		delete(c, "provider")
		delete(c, "api_key")
		if err := parseSMTPConfig(c, ec); err != nil {
			return nil, err
		}
	case "resend":
		for _, k := range []string{"host", "port", "security", "username", "password"} {
			delete(c, k)
		}
		ec.APIKey = c["api_key"]
	default:
		return nil, errors.New("provider must be smtp or resend")
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
	if ec.Provider == "resend" && len(list) > resendMaxTo {
		return nil, fmt.Errorf("resend accepts at most %d recipients", resendMaxTo)
	}
	ec.To = list
	return ec, nil
}

func parseSMTPConfig(c map[string]string, ec *emailConfig) error {
	ec.Host, ec.Security, ec.Username, ec.Password = c["host"], c["security"], c["username"], c["password"]
	if ec.Host == "" {
		return errors.New("smtp host is required")
	}
	if ec.Security == "" {
		ec.Security = "starttls"
	}
	if ec.Security != "starttls" && ec.Security != "tls" && ec.Security != "none" {
		return errors.New("security must be starttls, tls or none")
	}
	c["security"] = ec.Security
	if c["port"] == "" {
		ec.Port = defaultSMTPPort(ec.Security)
		c["port"] = strconv.Itoa(ec.Port)
	} else {
		p, err := strconv.Atoi(c["port"])
		if err != nil || p < 1 || p > 65535 {
			return errors.New("invalid smtp port")
		}
		ec.Port = p
	}
	return nil
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
	return fmt.Sprintf("%s[pingtxt] %s is %s", prefix, e.Monitor, e.statusWord())
}

func newMessageID(host string) string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return "<" + hex.EncodeToString(b) + "@" + host + ">"
}

// buildEmail renders the alert email of an event. base is the hub address
// (publicBase), used to link the monitor; it can be empty.
func buildEmail(ec *emailConfig, e Event, base string, now time.Time) mailMsg {
	to := make([]string, len(ec.To))
	for i, a := range ec.To {
		to[i] = a.String()
	}
	v := eventMailView(e, base)
	return newMail(mailHeaders{to: strings.Join(to, ", "), subject: emailSubject(e)}, v.renderText(""), v.renderHTML(nil), now)
}

// eventMailView lays out a status or expiry event.
func eventMailView(e Event, base string) mailView {
	v := mailView{Preheader: e.Text()}
	if i := strings.IndexByte(v.Preheader, '\n'); i > 0 {
		v.Preheader = v.Preheader[:i]
	}
	if e.Kind == KindCert || e.Kind == KindDomain {
		expiryMailView(&v, e)
	} else {
		statusMailView(&v, e)
	}
	if e.Test {
		v.Command = "pingtxt notify --test"
		v.Footer = append(v.Footer, "This is a test notification: nothing is down.")
	} else if u := monitorURL(base, e.MonitorID); u != "" {
		v.Action = &mailLink{Label: "open monitor", Href: u}
	}
	if base != "" {
		v.FootLinks = append(v.FootLinks, mailLink{Label: strings.TrimPrefix(strings.TrimPrefix(base, "https://"), "http://"), Href: base})
	}
	return v
}

func statusMailView(v *mailView, e Event) {
	v.Command = "pingtxt status " + e.Monitor
	v.Title = e.Monitor
	v.Badge = e.statusWord()
	v.Note = e.Cause
	v.Rows = append(v.Rows, mailRow{Key: "monitor", Value: e.Monitor}, mailRow{Key: "target", Value: e.Target})
	if e.Status == StatusDown {
		v.Tone = toneDown
		since := e.At
		if e.DownSince > 0 {
			since = e.DownSince
		}
		v.Rows = append(v.Rows, mailRow{Key: "down since", Value: mailTime(since)})
		if d := e.downFor(); d != "" {
			v.Rows = append(v.Rows, mailRow{Key: "down for", Value: d, Tone: toneDown})
		}
		if e.Reminder > 0 {
			v.Rows = append(v.Rows, mailRow{Key: "reminder", Value: fmt.Sprintf("#%d", e.Reminder)})
		}
		return
	}
	v.Tone = toneUp
	v.Rows = append(v.Rows, mailRow{Key: "up since", Value: mailTime(e.At)})
	if d := e.downFor(); d != "" {
		v.Rows = append(v.Rows, mailRow{Key: "was down", Value: d})
	}
}

func expiryMailView(v *mailView, e Event) {
	tag, what := "CERT", "TLS certificate"
	v.Command = "pingtxt cert " + e.Subject
	if e.Kind == KindDomain {
		tag, what = "DOMAIN", "domain registration"
		v.Command = "pingtxt whois " + e.Subject
	}
	date := time.UnixMilli(e.ExpiresAt).UTC().Format("2006-01-02")
	days := 0
	if e.DaysLeft != nil {
		days = *e.DaysLeft
	}
	v.Title = e.Subject
	v.Rows = []mailRow{{Key: "expires", Value: date}}
	switch e.Status {
	case ExpiryExpired:
		v.Tone, v.Badge = toneDown, tag+" EXPIRED"
		v.Lead = fmt.Sprintf("The %s of %s expired on %s.", what, e.Subject, date)
		v.Rows[0].Tone = toneDown
	case ExpiryRenewed:
		v.Tone, v.Badge = toneUp, tag+" OK"
		v.Lead = fmt.Sprintf("The %s of %s was renewed.", what, e.Subject)
		v.Rows[0].Key = "valid until"
	default:
		v.Tone, v.Badge = toneWarn, tag
		v.Lead = fmt.Sprintf("The %s of %s expires in %s.", what, e.Subject, plural(days, "day"))
		v.Rows = append(v.Rows, mailRow{Key: "days left", Value: fmt.Sprint(days), Tone: toneWarn})
	}
	v.Rows = append(v.Rows, mailRow{Key: "monitor", Value: e.Monitor}, mailRow{Key: "target", Value: e.Target})
}

// sendEmail delivers the event over SMTP or Resend.
func (n *Notifier) sendEmail(ctx context.Context, c map[string]string, e Event) error {
	cfg := make(map[string]string, len(c))
	for k, v := range c {
		cfg[k] = v
	}
	ec, err := parseEmailConfig(cfg)
	if err != nil {
		return err
	}
	base := ""
	if n.publicURL != nil {
		base = publicBase(n.publicURL())
	}
	return n.deliverEmail(ctx, ec, buildEmail(ec, e, base, time.Now()))
}

// deliverEmail sends a message to the recipients of ec.
func (n *Notifier) deliverEmail(ctx context.Context, ec *emailConfig, msg mailMsg) error {
	to := make([]string, len(ec.To))
	for i, a := range ec.To {
		to[i] = a.Address
	}
	ctx, cancel := context.WithTimeout(ctx, emailTimeout)
	defer cancel()
	return n.deliver(ctx, ec, []outMail{{to: to, msg: msg}}, nil)
}

// outMail is one message for deliver: its envelope recipients and content.
type outMail struct {
	to  []string
	msg mailMsg
}

// deliver sends the messages in order, over one SMTP connection or through
// the Resend API. A message refused by the server is reported to failed
// (when set) and the others are still sent; any other error stops the
// delivery.
func (n *Notifier) deliver(ctx context.Context, ec *emailConfig, msgs []outMail, failed func(i int, err error)) error {
	if ec.Provider == "resend" {
		return n.deliverResend(ctx, ec, msgs, failed)
	}
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
	for i, m := range msgs {
		if err := sendOne(cl, ec, m); err != nil {
			var perr *textproto.Error
			if failed == nil || !errors.As(err, &perr) {
				return err
			}
			// The server refused this message (e.g. a bad recipient):
			// reset the transaction and go on with the next one.
			failed(i, err)
			if err := cl.Reset(); err != nil {
				return fmt.Errorf("rset: %w", err)
			}
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
	}
	_ = cl.Quit()
	return nil
}

func sendOne(cl *smtp.Client, ec *emailConfig, m outMail) error {
	if err := cl.Mail(ec.From.Address); err != nil {
		return fmt.Errorf("mail from: %w", err)
	}
	for _, a := range m.to {
		if err := cl.Rcpt(a); err != nil {
			return fmt.Errorf("rcpt to %s: %w", a, err)
		}
	}
	w, err := cl.Data()
	if err != nil {
		return fmt.Errorf("data: %w", err)
	}
	if _, err := w.Write(writeMail(ec, m.msg)); err != nil {
		return fmt.Errorf("data: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("data: %w", err)
	}
	return nil
}

// heloName is sent in EHLO; servers only log it.
const heloName = "pingtxt.localhost"

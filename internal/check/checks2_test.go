package check

import (
	"bufio"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/binary"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/net/dns/dnsmessage"
)

func sshServer(t *testing.T, banner string, keys ...ssh.Signer) string {
	t.Helper()
	cfg := &ssh.ServerConfig{NoClientAuth: true, ServerVersion: banner}
	for _, k := range keys {
		cfg.AddHostKey(k)
	}
	return listen(t, func(c net.Conn) {
		_ = c.SetDeadline(time.Now().Add(3 * time.Second))
		_, _, _, _ = ssh.NewServerConn(c, cfg)
	})
}

func TestSSH(t *testing.T) {
	_, edPriv, _ := ed25519.GenerateKey(rand.Reader)
	ed, _ := ssh.NewSignerFromKey(edPriv)
	ecPriv, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	ec, _ := ssh.NewSignerFromKey(ecPriv)
	addr := sshServer(t, "SSH-2.0-OpenSSH_9.6 Test", ed, ec)

	r := run(t, Job{Type: TypeSSH, Target: addr})
	if !r.OK || !strings.HasPrefix(r.Message, "SSH-2.0-OpenSSH_9.6 Test, ") || !strings.Contains(r.Message, "SHA256:") {
		t.Fatalf("banner: %+v", r)
	}
	if r = run(t, Job{Type: TypeSSH, Target: "ssh://" + addr, Keyword: "OpenSSH_9"}); !r.OK {
		t.Errorf("keyword: %+v", r)
	}
	if r = run(t, Job{Type: TypeSSH, Target: addr, Keyword: "dropbear"}); r.OK || !strings.Contains(r.Message, "banner does not contain") {
		t.Errorf("wrong keyword: %+v", r)
	}
	// Both host keys match, whichever the server picks first.
	for _, k := range []ssh.Signer{ed, ec} {
		fp := ssh.FingerprintSHA256(k.PublicKey())
		if r = run(t, Job{Type: TypeSSH, Target: addr, Keyword: fp}); !r.OK || !strings.Contains(r.Message, fp) {
			t.Errorf("pinned %s: %+v", k.PublicKey().Type(), r)
		}
	}
	if r = run(t, Job{Type: TypeSSH, Target: addr, Keyword: "SHA256:nope"}); r.OK || !strings.HasPrefix(r.Message, "host key changed") {
		t.Errorf("wrong key: %+v", r)
	}
	notSSH := listen(t, func(c net.Conn) { _, _ = c.Write([]byte("220 smtp ready\r\n")); time.Sleep(time.Second) })
	if r = run(t, Job{Type: TypeSSH, Target: notSSH, TimeoutMs: 500}); r.OK || !strings.Contains(r.Message, "not an ssh server") {
		t.Errorf("not ssh: %+v", r)
	}
}

func TestSSHTarget(t *testing.T) {
	ok := map[string]string{"h": "h:22", "h:2222": "h:2222", "ssh://h": "h:22", "2001:db8::1": "[2001:db8::1]:22", "[2001:db8::1]:22": "[2001:db8::1]:22"}
	for in, want := range ok {
		if got, err := SSHTarget(in); err != nil || got != want {
			t.Errorf("%q: %q %v", in, got, err)
		}
	}
	for _, in := range []string{"", "user@h", "h/x", ":22"} {
		if _, err := SSHTarget(in); err == nil {
			t.Errorf("%q accepted", in)
		}
	}
}

// mailServer is a fake IMAP or POP3 server with STARTTLS/STLS and one user.
func mailServer(t *testing.T, pop bool, startTLS bool) string {
	cert := httptest.NewUnstartedServer(nil)
	cert.StartTLS()
	t.Cleanup(cert.Close)
	tlsCert := cert.TLS.Certificates[0]
	return listen(t, func(c net.Conn) {
		rd := bufio.NewReader(c)
		w := func(s string) { _, _ = c.Write([]byte(s + "\r\n")) }
		secure := false
		if pop {
			w("+OK POP3 ready")
		} else {
			w("* OK IMAP4rev1 ready")
		}
		for {
			line, err := rd.ReadString('\n')
			if err != nil {
				return
			}
			f := strings.Fields(line)
			if pop {
				switch strings.ToUpper(f[0]) {
				case "CAPA":
					w("+OK")
					if startTLS && !secure {
						w("STLS")
					}
					w("USER")
					w(".")
				case "STLS":
					w("+OK begin tls")
					tc := tls.Server(c, &tls.Config{Certificates: []tls.Certificate{tlsCert}})
					if tc.Handshake() != nil {
						return
					}
					c, rd, secure = tc, bufio.NewReader(tc), true
				case "USER":
					w("+OK")
				case "PASS":
					if f[1] == "pw" {
						w("+OK logged in")
					} else {
						w("-ERR [AUTH] invalid password")
					}
				case "QUIT":
					w("+OK bye")
					return
				}
				continue
			}
			tag, cmd := f[0], strings.ToUpper(f[1])
			switch cmd {
			case "CAPABILITY":
				if startTLS && !secure {
					w("* CAPABILITY IMAP4rev1 STARTTLS LOGINDISABLED")
				} else {
					w("* CAPABILITY IMAP4rev1 AUTH=PLAIN")
				}
				w(tag + " OK done")
			case "STARTTLS":
				w(tag + " OK begin tls")
				tc := tls.Server(c, &tls.Config{Certificates: []tls.Certificate{tlsCert}})
				if tc.Handshake() != nil {
					return
				}
				c, rd, secure = tc, bufio.NewReader(tc), true
			case "LOGIN":
				if f[3] == `"pw"` {
					w(tag + " OK logged in")
				} else {
					w(tag + " NO [AUTHENTICATIONFAILED] invalid")
				}
			case "LOGOUT":
				w("* BYE")
				w(tag + " OK bye")
				return
			}
		}
	})
}

func TestIMAP(t *testing.T) {
	addr := mailServer(t, false, true)
	r := run(t, Job{Type: TypeIMAP, Target: addr, IgnoreTLS: true})
	if !r.OK || r.Message != "tls, * OK IMAP4rev1 ready" || r.CertExpiry == 0 {
		t.Fatalf("starttls: %+v", r)
	}
	if r = run(t, Job{Type: TypeIMAP, Target: "imap://u:pw@" + addr, IgnoreTLS: true}); !r.OK || !strings.HasPrefix(r.Message, "tls, logged in") {
		t.Errorf("login: %+v", r)
	}
	if r = run(t, Job{Type: TypeIMAP, Target: "imap://u:bad@" + addr, IgnoreTLS: true}); r.OK || !strings.Contains(r.Message, "login: NO [AUTHENTICATIONFAILED]") {
		t.Errorf("bad login: %+v", r)
	}
	if r = run(t, Job{Type: TypeIMAP, Target: addr}); r.OK || !strings.Contains(r.Message, "starttls") {
		t.Errorf("untrusted cert accepted: %+v", r)
	}
	plain := mailServer(t, false, false)
	if r = run(t, Job{Type: TypeIMAP, Target: plain}); !r.OK || !strings.HasPrefix(r.Message, "no tls") {
		t.Errorf("plain: %+v", r)
	}
	if r = run(t, Job{Type: TypeIMAP, Target: "imap://u:pw@" + plain}); r.OK || r.Message != errClearPassword.Error() {
		t.Errorf("clear password: %+v", r)
	}
}

func TestPOP3(t *testing.T) {
	addr := mailServer(t, true, true)
	r := run(t, Job{Type: TypePOP3, Target: "pop3://u:pw@" + addr, IgnoreTLS: true})
	if !r.OK || r.Message != "tls, logged in, +OK POP3 ready" {
		t.Fatalf("login: %+v", r)
	}
	if r = run(t, Job{Type: TypePOP3, Target: "pop3://u:x@" + addr, IgnoreTLS: true}); r.OK || !strings.Contains(r.Message, "invalid password") {
		t.Errorf("bad login: %+v", r)
	}
	if r = run(t, Job{Type: TypePOP3, Target: mailServer(t, true, false)}); !r.OK || r.Message != "no tls, +OK POP3 ready" {
		t.Errorf("plain: %+v", r)
	}
}

func TestParseMailboxTarget(t *testing.T) {
	mt, err := ParseMailboxTarget(TypeIMAP, "imaps://me%40x.com:p%3Aw@mail.x.com")
	if err != nil || mt.port != "993" || !mt.implicitTLS || mt.user != "me@x.com" || mt.pass != "p:w" {
		t.Fatalf("%+v %v", mt, err)
	}
	if mt, _ := ParseMailboxTarget(TypePOP3, "mail.x.com"); mt.port != "110" || mt.implicitTLS {
		t.Errorf("pop3 default: %+v", mt)
	}
	if _, err := ParseMailboxTarget(TypePOP3, "imap://mail.x.com"); err == nil {
		t.Error("imap scheme on pop3 accepted")
	}
}

func ntpServer(t *testing.T, reply func(req []byte) []byte) string {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pc.Close() })
	go func() {
		buf := make([]byte, 512)
		for {
			n, from, err := pc.ReadFrom(buf)
			if err != nil {
				return
			}
			if p := reply(buf[:n]); p != nil {
				_, _ = pc.WriteTo(p, from)
			}
		}
	}()
	return pc.LocalAddr().String()
}

func ntpReply(req []byte, stratum byte, skew time.Duration) []byte {
	p := make([]byte, 48)
	p[0] = 4<<3 | 4
	p[1] = stratum
	copy(p[24:32], req[40:48])
	now := toNTP(time.Now().Add(skew))
	binary.BigEndian.PutUint64(p[32:], now)
	binary.BigEndian.PutUint64(p[40:], now)
	return p
}

func TestNTP(t *testing.T) {
	good := ntpServer(t, func(req []byte) []byte { return ntpReply(req, 2, 0) })
	r := run(t, Job{Type: TypeNTP, Target: good})
	if !r.OK || !strings.HasPrefix(r.Message, "stratum 2, offset ") {
		t.Fatalf("good: %+v", r)
	}
	skewed := ntpServer(t, func(req []byte) []byte { return ntpReply(req, 1, 5*time.Second) })
	if r = run(t, Job{Type: TypeNTP, Target: skewed}); r.OK || !strings.Contains(r.Message, "more than 1000 ms") {
		t.Errorf("skewed: %+v", r)
	}
	if r = run(t, Job{Type: TypeNTP, Target: skewed, ExpectedStatus: "10000"}); !r.OK {
		t.Errorf("skew within limit: %+v", r)
	}
	unsynced := ntpServer(t, func(req []byte) []byte { return ntpReply(req, 16, 0) })
	if r = run(t, Job{Type: TypeNTP, Target: unsynced}); r.OK || r.Message != "server clock not synchronized" {
		t.Errorf("unsynced: %+v", r)
	}
	kod := ntpServer(t, func(req []byte) []byte { p := ntpReply(req, 0, 0); copy(p[12:], "RATE"); return p })
	if r = run(t, Job{Type: TypeNTP, Target: kod}); r.OK || !strings.Contains(r.Message, `"RATE"`) {
		t.Errorf("kiss of death: %+v", r)
	}
	silent := ntpServer(t, func([]byte) []byte { return nil })
	if r = run(t, Job{Type: TypeNTP, Target: silent, TimeoutMs: 300}); r.OK || r.Message != "no reply (timeout)" {
		t.Errorf("silent: %+v", r)
	}
}

func TestNTPTimestamps(t *testing.T) {
	now := time.Date(2026, 11, 13, 10, 0, 0, 123456789, time.UTC)
	if got := fromNTP(toNTP(now)); got.Sub(now).Abs() > time.Microsecond {
		t.Errorf("round trip: %v", got)
	}
	after2036 := time.Date(2040, 1, 1, 0, 0, 0, 0, time.UTC)
	if got := fromNTP(toNTP(after2036)); !got.Equal(after2036) {
		t.Errorf("era 1: %v", got)
	}
}

// dnsServer answers A and MX queries from the maps; everything else is
// NXDOMAIN.
func dnsServer(t *testing.T, a map[string]string, mx map[string]string) string {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pc.Close() })
	go func() {
		buf := make([]byte, 1500)
		for {
			n, from, err := pc.ReadFrom(buf)
			if err != nil {
				return
			}
			var m dnsmessage.Message
			if m.Unpack(buf[:n]) != nil || len(m.Questions) == 0 {
				continue
			}
			q := m.Questions[0]
			name := strings.TrimSuffix(q.Name.String(), ".")
			resp := dnsmessage.Message{Header: dnsmessage.Header{ID: m.ID, Response: true, Authoritative: true}, Questions: m.Questions}
			hdr := dnsmessage.ResourceHeader{Name: q.Name, Type: q.Type, Class: dnsmessage.ClassINET, TTL: 60}
			switch {
			case q.Type == dnsmessage.TypeA && a[name] != "":
				ip := net.ParseIP(a[name]).To4()
				resp.Answers = append(resp.Answers, dnsmessage.Resource{Header: hdr, Body: &dnsmessage.AResource{A: [4]byte(ip)}})
			case q.Type == dnsmessage.TypeMX && mx[name] != "":
				resp.Answers = append(resp.Answers, dnsmessage.Resource{Header: hdr, Body: &dnsmessage.MXResource{Pref: 10, MX: dnsmessage.MustNewName(mx[name] + ".")}})
			case a[name] != "" || mx[name] != "":
				// Name exists, no record of this type.
			default:
				resp.RCode = dnsmessage.RCodeNameError
			}
			out, _ := resp.Pack()
			_, _ = pc.WriteTo(out, from)
		}
	}()
	return pc.LocalAddr().String()
}

func TestRBL(t *testing.T) {
	srv := dnsServer(t, map[string]string{
		"4.3.2.1.bl.test":         "127.0.0.2",
		"9.9.9.9.bl.test":         "127.255.255.254", // refused
		"mail.example.org":        "1.2.3.4",
		"bad.example.bl.dom.test": "127.0.1.2",
		"clean.example":           "5.6.7.8",
	}, map[string]string{"example.org": "mail.example.org"})
	r := run(t, Job{Type: TypeRBL, Target: "1.2.3.4", Keyword: "bl.test, other.test", DNSServer: srv})
	if r.OK || r.Message != "listed: bl.test (127.0.0.2)" {
		t.Fatalf("listed ip: %+v", r)
	}
	if r = run(t, Job{Type: TypeRBL, Target: "8.8.8.8", Keyword: "bl.test", DNSServer: srv}); !r.OK || r.Message != "not listed on 1 lists" {
		t.Errorf("clean ip: %+v", r)
	}
	if r = run(t, Job{Type: TypeRBL, Target: "9.9.9.9", Keyword: "bl.test", DNSServer: srv}); r.OK || !strings.HasPrefix(r.Message, "no blacklist answered") {
		t.Errorf("refused: %+v", r)
	}
	// Mail domain: its MX address is listed.
	if r = run(t, Job{Type: TypeRBL, Target: "example.org", Keyword: "bl.test", DNSServer: srv}); r.OK || r.Message != "listed: 1.2.3.4 on bl.test (127.0.0.2)" {
		t.Errorf("domain: %+v", r)
	}
	// Without MX the domain's own address is checked.
	if r = run(t, Job{Type: TypeRBL, Target: "clean.example", Keyword: "bl.test", DNSServer: srv}); !r.OK || !strings.Contains(r.Message, "(1 mail server addresses)") {
		t.Errorf("no mx: %+v", r)
	}
}

func TestRBLHelpers(t *testing.T) {
	if got := reverseIP(net.ParseIP("1.2.3.4")); got != "4.3.2.1" {
		t.Error(got)
	}
	if got := reverseIP(net.ParseIP("2001:db8::1")); got != "1.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.8.b.d.0.1.0.0.2" {
		t.Error(got)
	}
	for _, c := range []struct {
		addrs  []string
		listed bool
	}{
		{[]string{"127.0.0.2"}, true},
		{[]string{"127.0.0.1"}, false},
		{[]string{"127.255.255.254"}, false},
		{[]string{"127.0.1.255"}, false},
		{[]string{"10.0.0.1"}, false},
	} {
		if _, got := rblListed(c.addrs); got != c.listed {
			t.Errorf("%v: %v", c.addrs, got)
		}
	}
	if l, err := ParseBlacklists("zen.spamhaus.org,\nbl.spamcop.net zen.spamhaus.org."); err != nil || len(l) != 2 {
		t.Errorf("%v %v", l, err)
	}
	if _, err := ParseBlacklists("not a list!"); err == nil {
		t.Error("bad zone accepted")
	}
	if _, _, err := RBLTarget("https://x.com"); err == nil {
		t.Error("url accepted")
	}
}

func clientCert(t *testing.T) (certPEM, keyPEM string) {
	t.Helper()
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "pingtxt-client"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	kder, _ := x509.MarshalECPrivateKey(key)
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})),
		string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kder}))
}

func TestClientCert(t *testing.T) {
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if len(r.TLS.PeerCertificates) == 0 {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		_, _ = w.Write([]byte("hello " + r.TLS.PeerCertificates[0].Subject.CommonName))
	}))
	srv.TLS = &tls.Config{ClientAuth: tls.RequestClientCert}
	srv.StartTLS()
	defer srv.Close()
	certPEM, keyPEM := clientCert(t)
	r := run(t, Job{Type: TypeHTTP, Target: srv.URL, IgnoreTLS: true})
	if r.OK || r.StatusCode != 403 {
		t.Fatalf("without cert: %+v", r)
	}
	r = run(t, Job{Type: TypeKeyword, Target: srv.URL, IgnoreTLS: true, Keyword: "hello pingtxt-client", ClientCert: certPEM, ClientKey: keyPEM})
	if !r.OK {
		t.Fatalf("with cert: %+v", r)
	}
	r = run(t, Job{Type: TypeHTTP, Target: srv.URL, IgnoreTLS: true, ClientCert: certPEM})
	if r.OK || !strings.HasPrefix(r.Message, "client certificate:") {
		t.Errorf("no key: %+v", r)
	}
	if _, _, err := ParseClientCert(certPEM, "junk"); err == nil {
		t.Error("bad key accepted")
	}
}

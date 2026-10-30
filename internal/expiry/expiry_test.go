package expiry

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestRegistrableDomain(t *testing.T) {
	cases := map[string]string{
		"api.eu.example.co.uk": "example.co.uk",
		"www.example.com":      "example.com",
		"example.it":           "example.it",
		"EXAMPLE.com.":         "example.com",
		"1.2.3.4":              "",
		"::1":                  "",
		"localhost":            "",
		"":                     "",
	}
	for in, want := range cases {
		got, err := RegistrableDomain(in)
		if want == "" {
			if !errors.Is(err, ErrNoDomain) {
				t.Errorf("%q: want ErrNoDomain, got %q %v", in, got, err)
			}
			continue
		}
		if got != want {
			t.Errorf("%q: want %q, got %q (%v)", in, want, got, err)
		}
	}
}

func TestHost(t *testing.T) {
	cases := []struct{ in, host, port string }{
		{"https://Example.com/health", "example.com", ""},
		{"https://example.com:8443/", "example.com", "8443"},
		{"db.example.com:5432", "db.example.com", "5432"},
		{"example.com", "example.com", ""},
		{"[::1]:80", "::1", "80"},
	}
	for _, c := range cases {
		h, p := Host(c.in)
		if h != c.host || p != c.port {
			t.Errorf("%q: got %q %q", c.in, h, p)
		}
	}
}

func TestParseWhois(t *testing.T) {
	cases := []struct {
		name, text, date, registrar string
	}{
		{"com", "   Domain Name: EXAMPLE.COM\n   Registrar: RESERVED-Internet Assigned Numbers Authority\n   Registry Expiry Date: 2026-08-13T04:00:00Z\n", "2026-08-13", "RESERVED-Internet Assigned Numbers Authority"},
		{"it", "Domain:             example.it\nStatus:             ok\nCreated:            1999-01-01 00:00:00\nExpire Date:        2027-03-15\n\nRegistrar\n  Organization:     Aruba s.p.a.\n  Name:             ARUBA-REG\n", "2027-03-15", "ARUBA-REG"},
		{"uk", "    Registrar:\n        Nominet UK [Tag = NOMINET]\n    Relevant dates:\n        Registered on: 26-Aug-1996\n        Expiry date:  12-Oct-2026\n", "2026-10-12", ""},
		{"ru", "domain:        EXAMPLE.RU\npaid-till:     2026-12-01T21:00:00Z\n", "2026-12-01", ""},
	}
	for _, c := range cases {
		info, err := ParseWhois(c.text)
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		if got := info.ExpiresAt.Format("2006-01-02"); got != c.date {
			t.Errorf("%s: date %s, want %s", c.name, got, c.date)
		}
		if c.registrar != "" && info.Registrar != c.registrar {
			t.Errorf("%s: registrar %q, want %q", c.name, info.Registrar, c.registrar)
		}
	}
	if _, err := ParseWhois("Domain Name: example.de\nStatus: connect\n"); !errors.Is(err, ErrNoExpiry) {
		t.Errorf("de: want ErrNoExpiry, got %v", err)
	}
	if _, err := ParseWhois("No match for \"NOPE-XYZ.COM\".\n"); !errors.Is(err, errNotRegistered) {
		t.Errorf("free: want errNotRegistered, got %v", err)
	}
}

// fakeWhois answers WHOIS queries from a map: query -> answer.
func fakeWhois(answers map[string]map[string]string) func(context.Context, string) (net.Conn, error) {
	return func(_ context.Context, addr string) (net.Conn, error) {
		host, _, _ := net.SplitHostPort(addr)
		srv, ok := answers[host]
		if !ok {
			return nil, errors.New("unknown host " + host)
		}
		c1, c2 := net.Pipe()
		go func() {
			defer c2.Close()
			buf := make([]byte, 256)
			n, _ := c2.Read(buf)
			q := strings.TrimSpace(string(buf[:n]))
			_, _ = c2.Write([]byte(srv[q]))
		}()
		return c1, nil
	}
}

func TestLookupRDAPAndWhoisFallback(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/dns.json":
			_ = json.NewEncoder(w).Encode(map[string]any{"services": [][][]string{
				{{"com"}, {srv.URL + "/com/"}},
			}})
		case "/com/domain/example.com":
			_, _ = w.Write([]byte(`{"events":[{"eventAction":"registration","eventDate":"1995-08-14T04:00:00Z"},{"eventAction":"expiration","eventDate":"2026-08-13T04:00:00Z"}],
				"entities":[{"roles":["registrar"],"vcardArray":["vcard",[["version",{},"text","4.0"],["fn",{},"text","Example Registrar, Inc."]]]}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	d := NewDomains(srv.Client())
	d.BootstrapURL = srv.URL + "/dns.json"
	d.WhoisDial = fakeWhois(map[string]map[string]string{
		"whois.iana.org": {"it": "domain: IT\nwhois: whois.nic.it\n", "zz": "domain: ZZ\n"},
		"whois.nic.it":   {"example.it": "Domain: example.it\nExpire Date: 2027-03-15\n"},
	})
	ctx := context.Background()

	info, err := d.Lookup(ctx, "example.com")
	if err != nil {
		t.Fatal(err)
	}
	if info.Source != "rdap" || info.Registrar != "Example Registrar, Inc." || !info.ExpiresAt.Equal(time.Date(2026, 8, 13, 4, 0, 0, 0, time.UTC)) {
		t.Fatalf("rdap: %+v", info)
	}

	if _, err := d.Lookup(ctx, "missing.com"); !errors.Is(err, errNotRegistered) {
		t.Fatalf("missing.com: want errNotRegistered, got %v", err)
	}

	info, err = d.Lookup(ctx, "example.it")
	if err != nil {
		t.Fatal(err)
	}
	if info.Source != "whois" || info.ExpiresAt.Format("2006-01-02") != "2027-03-15" {
		t.Fatalf("whois: %+v", info)
	}

	if _, err := d.Lookup(ctx, "example.zz"); !errors.Is(err, errNoWhois) {
		t.Fatalf("example.zz: want errNoWhois, got %v", err)
	}
}

func TestCert(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	host, port, _ := net.SplitHostPort(u.Host)
	info, err := Cert(context.Background(), host, port, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	want := srv.Certificate().NotAfter
	if !info.NotAfter.Equal(want) {
		t.Fatalf("not after %v, want %v", info.NotAfter, want)
	}
	if info.VerifyErr == "" {
		t.Fatal("self-signed test certificate should not verify")
	}
}

package expiry

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
)

// DomainInfo is the registration data of a domain.
type DomainInfo struct {
	Domain    string
	ExpiresAt time.Time
	Registrar string
	Source    string // "rdap" or "whois"
}

// ErrNoExpiry means the registry answered but does not publish an expiry date
// (for example DENIC for .de).
var ErrNoExpiry = errors.New("the registry does not publish an expiry date")

// DefaultBootstrapURL is IANA's RDAP bootstrap file for domains.
const DefaultBootstrapURL = "https://data.iana.org/rdap/dns.json"

// Domains looks up domain expiry dates. The zero value is not usable: use NewDomains.
type Domains struct {
	Client       *http.Client
	BootstrapURL string
	// WhoisDial opens a connection to a WHOIS server (host:43). Tests replace it.
	WhoisDial func(ctx context.Context, addr string) (net.Conn, error)
	// IANAWhois is queried to find the WHOIS server of a TLD.
	IANAWhois string

	mu        sync.Mutex
	bootstrap map[string][]string // tld -> RDAP base URLs
	bootAt    time.Time
	whoisSrv  map[string]string // tld -> whois server ("" = none)
}

// NewDomains returns a lookup using client for HTTP requests.
func NewDomains(client *http.Client) *Domains {
	return &Domains{
		Client:       client,
		BootstrapURL: DefaultBootstrapURL,
		IANAWhois:    "whois.iana.org",
		WhoisDial: func(ctx context.Context, addr string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "tcp", addr)
		},
		whoisSrv: map[string]string{},
	}
}

// Lookup returns the registration info of a registrable domain.
func (d *Domains) Lookup(ctx context.Context, domain string) (*DomainInfo, error) {
	domain = strings.ToLower(strings.TrimSuffix(domain, "."))
	tld := domain[strings.LastIndexByte(domain, '.')+1:]

	var rdapErr error
	if bases, err := d.rdapBases(ctx, tld); err != nil {
		rdapErr = err
	} else if len(bases) > 0 {
		info, err := d.rdap(ctx, bases, domain)
		if err == nil {
			return info, nil
		}
		rdapErr = err
		if errors.Is(err, errNotRegistered) {
			return nil, err
		}
	}
	info, err := d.whois(ctx, tld, domain)
	if err == nil {
		return info, nil
	}
	if rdapErr != nil && !errors.Is(rdapErr, ErrNoExpiry) && errors.Is(err, errNoWhois) {
		return nil, fmt.Errorf("rdap: %w", rdapErr)
	}
	return nil, err
}

var errNotRegistered = errors.New("domain not found at the registry")

// ---------- RDAP ----------

func (d *Domains) rdapBases(ctx context.Context, tld string) ([]string, error) {
	d.mu.Lock()
	fresh := d.bootstrap != nil && time.Since(d.bootAt) < 24*time.Hour
	if fresh {
		b := d.bootstrap[tld]
		d.mu.Unlock()
		return b, nil
	}
	d.mu.Unlock()

	var file struct {
		Services [][][]string `json:"services"`
	}
	if err := d.getJSON(ctx, d.BootstrapURL, &file); err != nil {
		d.mu.Lock()
		defer d.mu.Unlock()
		if d.bootstrap != nil { // keep using the stale copy
			return d.bootstrap[tld], nil
		}
		return nil, fmt.Errorf("rdap bootstrap: %w", err)
	}
	m := map[string][]string{}
	for _, s := range file.Services {
		if len(s) < 2 {
			continue
		}
		// Prefer https endpoints.
		urls := []string{}
		for _, u := range s[1] {
			if strings.HasPrefix(u, "https://") {
				urls = append(urls, u)
			}
		}
		for _, u := range s[1] {
			if !strings.HasPrefix(u, "https://") {
				urls = append(urls, u)
			}
		}
		for _, t := range s[0] {
			m[strings.ToLower(t)] = urls
		}
	}
	d.mu.Lock()
	d.bootstrap, d.bootAt = m, time.Now()
	d.mu.Unlock()
	return m[tld], nil
}

type rdapDomain struct {
	Events []struct {
		Action string `json:"eventAction"`
		Date   string `json:"eventDate"`
	} `json:"events"`
	Entities []rdapEntity `json:"entities"`
}

type rdapEntity struct {
	Roles    []string          `json:"roles"`
	VCard    []json.RawMessage `json:"vcardArray"`
	Entities []rdapEntity      `json:"entities"`
}

func (d *Domains) rdap(ctx context.Context, bases []string, domain string) (*DomainInfo, error) {
	var last error
	for _, base := range bases {
		var doc rdapDomain
		err := d.getJSON(ctx, strings.TrimRight(base, "/")+"/domain/"+domain, &doc)
		if err != nil {
			last = err
			continue
		}
		return parseRDAP(domain, &doc)
	}
	return nil, last
}

func parseRDAP(domain string, doc *rdapDomain) (*DomainInfo, error) {
	info := &DomainInfo{Domain: domain, Source: "rdap", Registrar: rdapRegistrar(doc.Entities)}
	for _, e := range doc.Events {
		if e.Action != "expiration" {
			continue
		}
		t, err := parseDate(e.Date)
		if err != nil {
			return nil, fmt.Errorf("rdap: bad expiration date %q", e.Date)
		}
		info.ExpiresAt = t
		return info, nil
	}
	return nil, ErrNoExpiry
}

func rdapRegistrar(ents []rdapEntity) string {
	for _, e := range ents {
		for _, r := range e.Roles {
			if r == "registrar" {
				return vcardFN(e.VCard)
			}
		}
	}
	return ""
}

// vcardFN extracts the "fn" property of a jCard: ["vcard", [["fn", {}, "text", "Name"], ...]].
func vcardFN(v []json.RawMessage) string {
	if len(v) < 2 {
		return ""
	}
	var props [][]json.RawMessage
	if json.Unmarshal(v[1], &props) != nil {
		return ""
	}
	for _, p := range props {
		var name, val string
		if len(p) >= 4 && json.Unmarshal(p[0], &name) == nil && name == "fn" && json.Unmarshal(p[3], &val) == nil {
			return val
		}
	}
	return ""
}

func (d *Domains) getJSON(ctx context.Context, u string, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/rdap+json, application/json")
	req.Header.Set("User-Agent", "pingtxt")
	resp, err := d.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return errNotRegistered
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("http %d from %s", resp.StatusCode, req.URL.Host)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(v)
}

// ---------- WHOIS ----------

var errNoWhois = errors.New("no whois server for this tld")

func (d *Domains) whois(ctx context.Context, tld, domain string) (*DomainInfo, error) {
	srv, err := d.whoisServer(ctx, tld)
	if err != nil {
		return nil, err
	}
	text, err := d.whoisQuery(ctx, srv, domain)
	if err != nil {
		return nil, fmt.Errorf("whois %s: %w", srv, err)
	}
	info, err := ParseWhois(text)
	if err != nil {
		return nil, err
	}
	info.Domain = domain
	return info, nil
}

func (d *Domains) whoisServer(ctx context.Context, tld string) (string, error) {
	d.mu.Lock()
	srv, ok := d.whoisSrv[tld]
	d.mu.Unlock()
	if ok {
		if srv == "" {
			return "", errNoWhois
		}
		return srv, nil
	}
	text, err := d.whoisQuery(ctx, d.IANAWhois, tld)
	if err != nil {
		return "", fmt.Errorf("whois %s: %w", d.IANAWhois, err)
	}
	srv = whoisField(text, "whois", "refer")
	d.mu.Lock()
	d.whoisSrv[tld] = srv
	d.mu.Unlock()
	if srv == "" {
		return "", errNoWhois
	}
	return srv, nil
}

func (d *Domains) whoisQuery(ctx context.Context, server, query string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	conn, err := d.WhoisDial(ctx, net.JoinHostPort(server, "43"))
	if err != nil {
		return "", err
	}
	defer conn.Close()
	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	}
	if _, err := io.WriteString(conn, query+"\r\n"); err != nil {
		return "", err
	}
	b, err := io.ReadAll(io.LimitReader(conn, 1<<20))
	if err != nil && len(b) == 0 {
		return "", err
	}
	return string(b), nil
}

func whoisField(text string, keys ...string) string {
	sc := bufio.NewScanner(strings.NewReader(text))
	for sc.Scan() {
		k, v, ok := strings.Cut(sc.Text(), ":")
		if !ok {
			continue
		}
		k = strings.ToLower(strings.TrimSpace(k))
		for _, want := range keys {
			if k == want {
				if v = strings.TrimSpace(v); v != "" {
					return v
				}
			}
		}
	}
	return ""
}

var (
	whoisExpiryRe = regexp.MustCompile(`(?im)^\s*(?:registry expiry date|registrar registration expiration date|expiration date|expiry date|expire date|expires on|expires|expiration time|paid-till|renewal date|valid until|domain expiration date)\s*[:.]+\s*(\S.*?)\s*$`)
	whoisFreeRe   = regexp.MustCompile(`(?im)^\s*(?:no match|not found|no entries found|no data found|status:\s*(?:available|free))\b`)
	whoisRegRe    = regexp.MustCompile(`(?im)^\s*registrar(?: name)?\s*:\s*(\S.*?)\s*$`)
)

// ParseWhois extracts the expiry date from a WHOIS answer.
func ParseWhois(text string) (*DomainInfo, error) {
	if m := whoisExpiryRe.FindStringSubmatch(text); m != nil {
		t, err := parseDate(m[1])
		if err != nil {
			return nil, fmt.Errorf("whois: unrecognised date %q", m[1])
		}
		info := &DomainInfo{ExpiresAt: t, Source: "whois"}
		if r := whoisRegRe.FindStringSubmatch(text); r != nil {
			info.Registrar = r[1]
		} else {
			info.Registrar = registrarBlock(text)
		}
		return info, nil
	}
	if whoisFreeRe.MatchString(text) {
		return nil, errNotRegistered
	}
	return nil, ErrNoExpiry
}

// registrarBlock reads the "Name:" line of an indented "Registrar" block
// (the .it format).
func registrarBlock(text string) string {
	lines := strings.Split(text, "\n")
	for i, l := range lines {
		if strings.TrimSpace(l) != "Registrar" {
			continue
		}
		for _, n := range lines[i+1 : min(i+6, len(lines))] {
			if k, v, ok := strings.Cut(n, ":"); ok && strings.EqualFold(strings.TrimSpace(k), "name") {
				return strings.TrimSpace(v)
			}
		}
	}
	return ""
}

var dateLayouts = []string{
	time.RFC3339Nano,
	"2006-01-02T15:04:05Z0700",
	"2006-01-02T15:04:05",
	"2006-01-02 15:04:05 MST",
	"2006-01-02 15:04:05Z07:00",
	"2006-01-02 15:04:05",
	"2006-01-02",
	"2006.01.02",
	"2006/01/02",
	"02-Jan-2006",
	"02.01.2006",
	"02/01/2006",
	"January 2 2006",
	"Mon Jan 2 15:04:05 MST 2006",
}

func parseDate(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	// Drop trailing comments like "2025-01-01 (YYYY-MM-DD)".
	if i := strings.Index(s, " ("); i > 0 {
		s = s[:i]
	}
	for _, l := range dateLayouts {
		if t, err := time.Parse(l, s); err == nil {
			return t.UTC(), nil
		}
	}
	// "2025-01-01T00:00:00.0Z" and other variants: try the date part.
	if len(s) >= 10 {
		if t, err := time.Parse("2006-01-02", s[:10]); err == nil {
			return t.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("unrecognised date %q", s)
}

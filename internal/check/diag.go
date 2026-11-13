package check

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Diagnostics are collected by the location that ran a failing check, when
// the hub asked for them (Job.Diagnose): DNS, TCP connections, TLS, the
// HTTP response headers and a traceroute. They are attached to the result
// and then to the incident and its alerts.

// DiagBudget is the most time the diagnostics may take on top of the check.
const DiagBudget = 8 * time.Second

// Limits on what the diagnostics keep.
const (
	diagMaxBytes   = 16 << 10 // JSON size of a whole Diag
	diagMaxHeaders = 4 << 10  // response headers, names + values
	diagMaxAddrs   = 4        // TCP connections tried
	diagMaxHops    = 20
	diagHopWait    = 2 * time.Second // how long the traceroute waits for replies
)

// Diag is what a location found out about a failing check.
type Diag struct {
	At   int64  `json:"at"` // unix milliseconds
	Host string `json:"host,omitempty"`
	Port string `json:"port,omitempty"`
	// Type is the monitor type. Agents older than it leave it out and the
	// hub fills it in.
	Type string `json:"type,omitempty"`
	// Check is the error of the failing check, filled in by the hub.
	Check string `json:"check,omitempty"`
	// Banner is the first line the server sent (SMTP greeting).
	Banner string `json:"banner,omitempty"`

	DNS   []DiagDNS  `json:"dns,omitempty"`
	TCP   []DiagConn `json:"tcp,omitempty"`
	TLS   *DiagTLS   `json:"tls,omitempty"`
	HTTP  *DiagHTTP  `json:"http,omitempty"`
	Trace *DiagTrace `json:"trace,omitempty"`

	// Note explains a step that could not run, e.g. a docker monitor.
	Note string  `json:"note,omitempty"`
	Ms   float64 `json:"ms"` // time spent collecting
}

// DiagDNS is one lookup.
type DiagDNS struct {
	Resolver string   `json:"resolver"` // "system" or the monitor's DNS server
	Query    string   `json:"query"`    // e.g. "A example.com"
	Answers  []string `json:"answers,omitempty"`
	CNAME    string   `json:"cname,omitempty"`
	Ms       float64  `json:"ms"`
	Error    string   `json:"error,omitempty"`
}

// DiagConn is one TCP connection attempt.
type DiagConn struct {
	Addr  string  `json:"addr"`
	OK    bool    `json:"ok"`
	Ms    float64 `json:"ms"`
	Error string  `json:"error,omitempty"`
}

// DiagTLS describes the TLS handshake and the server certificate.
type DiagTLS struct {
	Addr     string   `json:"addr"`
	Version  string   `json:"version,omitempty"`
	Subject  string   `json:"subject,omitempty"`
	Issuer   string   `json:"issuer,omitempty"`
	SANs     []string `json:"sans,omitempty"`
	NotAfter int64    `json:"not_after,omitempty"` // unix seconds
	Error    string   `json:"error,omitempty"`
}

// DiagHTTP is the response the check got. The body is never kept, nor the
// request headers (they can hold credentials).
type DiagHTTP struct {
	Status     int          `json:"status"`
	Proto      string       `json:"proto,omitempty"`
	RemoteAddr string       `json:"remote_addr,omitempty"`
	Redirects  []string     `json:"redirects,omitempty"`
	Headers    []DiagHeader `json:"headers,omitempty"`
}

// DiagHeader is one response header, with secrets redacted.
type DiagHeader struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// DiagTrace is a traceroute to the target.
type DiagTrace struct {
	Proto   string    `json:"proto"` // "tcp" or "udp"
	Dest    string    `json:"dest"`
	Port    int       `json:"port"`
	Hops    []DiagHop `json:"hops,omitempty"`
	Reached bool      `json:"reached"`
	Error   string    `json:"error,omitempty"`
}

// DiagHop is one hop; Addr is empty when it did not answer.
type DiagHop struct {
	TTL   int     `json:"ttl"`
	Addr  string  `json:"addr,omitempty"`
	Name  string  `json:"name,omitempty"`
	RTTMs float64 `json:"rtt_ms,omitempty"`
}

// diagTarget is what the diagnostics probe for a job.
type diagTarget struct {
	host    string // name or IP
	port    string // empty: no TCP (ping, dns)
	tls     bool   // TLS right after connecting (https, rediss, smtps...)
	tlsName string
	note    string // nothing to probe, and why
}

func targetFor(j Job) diagTarget {
	switch j.Type {
	case TypeHTTP, TypeKeyword, TypeMCP, TypeWS:
		u, err := url.Parse(j.Target)
		if err != nil || u.Hostname() == "" {
			return diagTarget{note: "invalid URL"}
		}
		secure := u.Scheme == "https" || u.Scheme == "wss"
		t := diagTarget{host: u.Hostname(), port: u.Port(), tls: secure, tlsName: u.Hostname()}
		if t.port == "" {
			t.port = "80"
			if t.tls {
				t.port = "443"
			}
		}
		return t
	case TypeUDP:
		// No TCP connection to try: DNS and the route to the host.
		h, _, err := net.SplitHostPort(j.Target)
		if err != nil {
			return diagTarget{note: "invalid host:port"}
		}
		return diagTarget{host: h}
	case TypeTCP:
		h, p, err := net.SplitHostPort(j.Target)
		if err != nil {
			return diagTarget{note: "invalid host:port"}
		}
		return diagTarget{host: h, port: p}
	case TypePing:
		return diagTarget{host: StripBrackets(strings.TrimSpace(j.Target))}
	case TypeDNS:
		// The DNS step queries the record; the traceroute goes to the
		// monitor's DNS server, if any.
		if j.DNSServer == "" {
			return diagTarget{}
		}
		h, p, err := net.SplitHostPort(j.DNSServer)
		if err != nil {
			h, p = StripBrackets(j.DNSServer), "53"
		}
		return diagTarget{host: h, port: p}
	case TypePostgres, TypeMySQL, TypeRedis:
		u, err := url.Parse(j.Target)
		if err != nil || u.Hostname() == "" {
			return diagTarget{note: "invalid URL"}
		}
		def := map[string]string{TypePostgres: "5432", TypeMySQL: "3306", TypeRedis: "6379"}[j.Type]
		t := diagTarget{host: u.Hostname(), port: u.Port(), tls: u.Scheme == "rediss", tlsName: u.Hostname()}
		if t.port == "" {
			t.port = def
		}
		return t
	case TypeMongo:
		u, err := ParseMongoURL(j.Target)
		if err != nil {
			return diagTarget{note: "invalid URL"}
		}
		if u.Scheme == "mongodb+srv" {
			return diagTarget{host: u.Hostname()}
		}
		host := strings.Split(u.Host, ",")[0]
		h, p, err := net.SplitHostPort(host)
		if err != nil {
			h, p = StripBrackets(host), "27017"
		}
		return diagTarget{host: h, port: p}
	case TypeSMTP:
		h, p, implicit, err := parseSMTPTarget(j.Target)
		if err != nil {
			return diagTarget{note: err.Error()}
		}
		return diagTarget{host: h, port: p, tls: implicit, tlsName: h}
	case TypeGRPC:
		addr, _, secure, err := parseGRPCTarget(j.Target)
		if err != nil {
			return diagTarget{note: err.Error()}
		}
		h, p, _ := net.SplitHostPort(addr)
		return diagTarget{host: h, port: p, tls: secure, tlsName: h}
	case TypeSSH:
		addr, err := SSHTarget(j.Target)
		if err != nil {
			return diagTarget{note: err.Error()}
		}
		h, p, _ := net.SplitHostPort(addr)
		return diagTarget{host: h, port: p}
	case TypeIMAP, TypePOP3:
		mt, err := ParseMailboxTarget(j.Type, j.Target)
		if err != nil {
			return diagTarget{note: err.Error()}
		}
		return diagTarget{host: mt.host, port: mt.port, tls: mt.implicitTLS, tlsName: mt.host}
	case TypeNTP:
		// UDP: DNS and the route to the host.
		addr, err := NTPTarget(j.Target)
		if err != nil {
			return diagTarget{note: err.Error()}
		}
		h, _, _ := net.SplitHostPort(addr)
		return diagTarget{host: h}
	case TypeDocker:
		return diagTarget{note: "docker checks use the local Docker socket: no network diagnostics"}
	}
	return diagTarget{note: "no diagnostics for this monitor type"}
}

// Diagnose collects the diagnostics of a failing check. r is the failed
// result: it carries the HTTP response, if one arrived.
func Diagnose(ctx context.Context, j Job, r Result) *Diag {
	ctx, cancel := context.WithTimeout(ctx, DiagBudget)
	defer cancel()
	start := time.Now()
	d := &Diag{HTTP: r.http, Type: j.Type, Check: truncate(r.Message, 300)}
	t := targetFor(j)
	d.Host, d.Port, d.Note = t.host, t.port, t.note

	// The family to look at: the job's, or the one that failed when the
	// check ran over both.
	fam := j.IPFamily
	if fam == IPBoth {
		fam = IPAny
		if strings.HasPrefix(r.Message, "IPv6:") {
			fam = IPv6
		} else if strings.HasPrefix(r.Message, "IPv4:") {
			fam = IPv4
		}
	}

	if j.Type == TypeDNS {
		d.DNS = diagDNSRecord(ctx, j)
	}
	var ips []net.IP
	if t.host != "" {
		if ip := net.ParseIP(t.host); ip != nil {
			ips = []net.IP{ip}
		} else {
			var dns DiagDNS
			dns, ips = diagResolve(ctx, t.host)
			d.DNS = append(d.DNS, dns)
		}
	}
	ips = filterFamily(ips, fam)
	if len(ips) > diagMaxAddrs {
		ips = ips[:diagMaxAddrs]
	}

	var wg sync.WaitGroup
	if t.port != "" && len(ips) > 0 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			d.TCP = diagConnect(ctx, ips, t.port)
			if j.Type == TypeSMTP || j.Type == TypeSSH || j.Type == TypeIMAP || j.Type == TypePOP3 {
				for _, c := range d.TCP {
					if c.OK {
						d.Banner = diagBanner(ctx, c.Addr, t.tlsName, t.tls)
						break
					}
				}
			}
			if t.tls {
				for _, c := range d.TCP {
					if c.OK {
						d.TLS = diagTLS(ctx, c.Addr, t.tlsName)
						break
					}
				}
			}
		}()
	}
	if len(ips) > 0 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			port, _ := strconv.Atoi(t.port)
			d.Trace = Traceroute(ctx, ips[0], port, diagMaxHops)
			nameHops(ctx, d.Trace)
		}()
	}
	wg.Wait()
	d.Ms = ms(time.Since(start))
	d.At = time.Now().UnixMilli()
	d.shrink()
	return d
}

// diagBanner reads the first line a server sends after connecting, such
// as the SMTP greeting.
func diagBanner(ctx context.Context, addr, name string, useTLS bool) string {
	cctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	var c net.Conn
	var err error
	if useTLS {
		dl := tls.Dialer{Config: &tls.Config{ServerName: name, InsecureSkipVerify: true}}
		c, err = dl.DialContext(cctx, "tcp", addr)
	} else {
		var dl net.Dialer
		c, err = dl.DialContext(cctx, "tcp", addr)
	}
	if err != nil {
		return ""
	}
	defer c.Close()
	if dl, ok := cctx.Deadline(); ok {
		_ = c.SetReadDeadline(dl)
	}
	buf := make([]byte, 512)
	n, _ := c.Read(buf)
	line, _, _ := strings.Cut(string(buf[:n]), "\n")
	return truncate(strings.TrimSpace(strings.ToValidUTF8(line, "")), 200)
}

func filterFamily(ips []net.IP, fam int) []net.IP {
	if fam != IPv4 && fam != IPv6 {
		return ips
	}
	var out []net.IP
	for _, ip := range ips {
		if (ip.To4() != nil) == (fam == IPv4) {
			out = append(out, ip)
		}
	}
	return out
}

// diagResolve looks the host up with the system resolver.
func diagResolve(ctx context.Context, host string) (DiagDNS, []net.IP) {
	d := DiagDNS{Resolver: "system", Query: "A/AAAA " + host}
	start := time.Now()
	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	d.Ms = ms(time.Since(start))
	if err != nil {
		d.Error = cleanErr(err)
		return d, nil
	}
	if c, err := net.DefaultResolver.LookupCNAME(ctx, host); err == nil &&
		!strings.EqualFold(strings.TrimSuffix(c, "."), strings.TrimSuffix(host, ".")) {
		d.CNAME = c
	}
	ips := make([]net.IP, 0, len(addrs))
	for _, a := range addrs {
		ips = append(ips, a.IP)
	}
	// IPv4 first, as most clients would try.
	sort.SliceStable(ips, func(a, b int) bool { return ips[a].To4() != nil && ips[b].To4() == nil })
	for _, ip := range ips {
		d.Answers = append(d.Answers, ip.String())
	}
	return d, ips
}

// diagDNSRecord repeats the query of a dns monitor with its server and the
// system resolver, and looks up the NS records of the name.
func diagDNSRecord(ctx context.Context, j Job) []DiagDNS {
	host := StripBrackets(j.Target)
	if u, err := url.Parse(host); err == nil && u.Host != "" {
		host = u.Hostname()
	}
	record := strings.ToUpper(j.DNSRecord)
	if record == "" {
		record = "A"
	}
	query := func(label string, res *net.Resolver, rec string) DiagDNS {
		d := DiagDNS{Resolver: label, Query: rec + " " + host}
		start := time.Now()
		answers, err := lookupRecord(ctx, res, rec, host)
		d.Ms = ms(time.Since(start))
		d.Answers = answers
		if err != nil {
			d.Error = cleanErr(err)
		}
		return d
	}
	var out []DiagDNS
	if j.DNSServer != "" {
		out = append(out, query(j.DNSServer, j.resolver(), record))
	}
	out = append(out, query("system", net.DefaultResolver, record))
	if record != "NS" {
		out = append(out, query("system", net.DefaultResolver, "NS"))
	}
	return out
}

func diagConnect(ctx context.Context, ips []net.IP, port string) []DiagConn {
	out := make([]DiagConn, len(ips))
	var wg sync.WaitGroup
	for i, ip := range ips {
		wg.Add(1)
		go func() {
			defer wg.Done()
			addr := net.JoinHostPort(ip.String(), port)
			cctx, cancel := context.WithTimeout(ctx, 3*time.Second)
			defer cancel()
			var dl net.Dialer
			start := time.Now()
			c, err := dl.DialContext(cctx, "tcp", addr)
			out[i] = DiagConn{Addr: addr, Ms: ms(time.Since(start))}
			if err != nil {
				out[i].Error = cleanErr(err)
				return
			}
			c.Close()
			out[i].OK = true
		}()
	}
	wg.Wait()
	return out
}

func diagTLS(ctx context.Context, addr, name string) *DiagTLS {
	d := &DiagTLS{Addr: addr}
	cctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	dl := tls.Dialer{Config: &tls.Config{ServerName: name, InsecureSkipVerify: true}}
	c, err := dl.DialContext(cctx, "tcp", addr)
	if err != nil {
		d.Error = cleanErr(err)
		return d
	}
	defer c.Close()
	cs := c.(*tls.Conn).ConnectionState()
	d.Version = tls.VersionName(cs.Version)
	if len(cs.PeerCertificates) == 0 {
		d.Error = "no certificate"
		return d
	}
	leaf := cs.PeerCertificates[0]
	d.Subject = leaf.Subject.CommonName
	d.Issuer = leaf.Issuer.CommonName
	if d.Issuer == "" && len(leaf.Issuer.Organization) > 0 {
		d.Issuer = leaf.Issuer.Organization[0]
	}
	d.SANs = leaf.DNSNames
	if len(d.SANs) > 10 {
		d.SANs = append(d.SANs[:10:10], "…")
	}
	d.NotAfter = leaf.NotAfter.Unix()
	inter := x509.NewCertPool()
	for _, c := range cs.PeerCertificates[1:] {
		inter.AddCert(c)
	}
	if _, err := leaf.Verify(x509.VerifyOptions{DNSName: name, Intermediates: inter}); err != nil {
		d.Error = err.Error()
	}
	return d
}

// nameHops looks up the reverse DNS names of the hops, briefly.
func nameHops(ctx context.Context, t *DiagTrace) {
	if t == nil {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	var wg sync.WaitGroup
	for i := range t.Hops {
		if t.Hops[i].Addr == "" {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			if names, err := net.DefaultResolver.LookupAddr(ctx, t.Hops[i].Addr); err == nil && len(names) > 0 {
				t.Hops[i].Name = strings.TrimSuffix(names[0], ".")
			}
		}()
	}
	wg.Wait()
}

// Response headers whose value is never kept.
var secretHeaders = map[string]bool{
	"set-cookie": true, "cookie": true, "authorization": true, "proxy-authorization": true,
	"proxy-authenticate": true, "www-authenticate": true,
}

func secretHeader(name string) bool {
	n := strings.ToLower(name)
	if secretHeaders[n] {
		return true
	}
	for _, w := range []string{"token", "key", "secret", "session", "csrf", "auth"} {
		if strings.Contains(n, w) {
			return true
		}
	}
	return false
}

// diagHTTP keeps what a failing HTTP check got back.
func diagHTTP(resp *http.Response, remote string, redirects []string) *DiagHTTP {
	d := &DiagHTTP{Status: resp.StatusCode, Proto: resp.Proto, RemoteAddr: remote, Redirects: redirects}
	names := make([]string, 0, len(resp.Header))
	for k := range resp.Header {
		names = append(names, k)
	}
	sort.Strings(names)
	size := 0
	for _, k := range names {
		v := strings.Join(resp.Header[k], ", ")
		if secretHeader(k) {
			v = "[redacted]"
		}
		v = truncate(v, 300)
		size += len(k) + len(v)
		if size > diagMaxHeaders {
			d.Headers = append(d.Headers, DiagHeader{Name: "…", Value: "more headers not kept"})
			break
		}
		d.Headers = append(d.Headers, DiagHeader{Name: k, Value: v})
	}
	return d
}

// redirectURL shows a redirect hop without its query string (it can hold
// tokens) or credentials.
func redirectURL(u *url.URL) string {
	c := *u
	c.User = nil
	c.RawQuery = ""
	c.Fragment = ""
	return c.String()
}

// shrink keeps the JSON under diagMaxBytes, dropping the bulkiest parts.
func (d *Diag) shrink() {
	size := func() int { b, _ := json.Marshal(d); return len(b) }
	if size() <= diagMaxBytes {
		return
	}
	if d.HTTP != nil {
		d.HTTP.Headers = nil
	}
	for i := range d.DNS {
		if len(d.DNS[i].Answers) > 10 {
			d.DNS[i].Answers = d.DNS[i].Answers[:10]
		}
	}
	if size() > diagMaxBytes && d.Trace != nil {
		for i := range d.Trace.Hops {
			d.Trace.Hops[i].Name = ""
		}
	}
}

// Summary is a one-line description of the diagnostics, for alerts.
func (d *Diag) Summary() string {
	if d == nil {
		return ""
	}
	var parts []string
	if f := d.Findings(); len(f) > 0 && f[0].Level != FindOK {
		parts = append(parts, f[0].Text)
	}
	for _, q := range d.DNS {
		if q.Resolver != "system" || !strings.HasPrefix(q.Query, "A/AAAA") {
			continue
		}
		if q.Error != "" {
			parts = append(parts, "DNS: "+q.Error)
		} else if len(q.Answers) > 0 {
			a := q.Answers[0]
			if len(q.Answers) > 1 {
				a += " +" + strconv.Itoa(len(q.Answers)-1)
			}
			parts = append(parts, "DNS "+a+" ("+fmtMs(q.Ms)+")")
		}
	}
	if len(d.TCP) > 0 {
		c := d.TCP[0]
		for _, x := range d.TCP {
			if x.OK {
				c = x
				break
			}
		}
		if c.OK {
			parts = append(parts, "TCP "+d.Port+" ok ("+fmtMs(c.Ms)+")")
		} else {
			parts = append(parts, "TCP "+d.Port+": "+c.Error)
		}
	}
	if d.TLS != nil && d.TLS.Error != "" {
		parts = append(parts, "TLS: "+truncate(d.TLS.Error, 80))
	}
	if d.HTTP != nil {
		h := "HTTP " + strconv.Itoa(d.HTTP.Status)
		for _, x := range d.HTTP.Headers {
			if strings.EqualFold(x.Name, "Server") {
				h += " (" + truncate(x.Value, 30) + ")"
			}
		}
		parts = append(parts, h)
	}
	if t := d.Trace; t != nil {
		switch {
		case t.Error != "":
		case t.Reached:
			parts = append(parts, "traceroute: "+strconv.Itoa(len(t.Hops))+" hops")
		default:
			if last := t.LastHop(); last != nil {
				where := last.Addr
				if last.Name != "" {
					where = last.Name
				}
				parts = append(parts, "traceroute stops at hop "+strconv.Itoa(last.TTL)+" ("+where+")")
			} else {
				parts = append(parts, "traceroute: no hop answered")
			}
		}
	}
	if len(parts) == 0 && d.Note != "" {
		return d.Note
	}
	return strings.Join(parts, " · ")
}

// LastHop is the last hop that answered.
func (t *DiagTrace) LastHop() *DiagHop {
	for i := len(t.Hops) - 1; i >= 0; i-- {
		if t.Hops[i].Addr != "" {
			return &t.Hops[i]
		}
	}
	return nil
}

func fmtMs(v float64) string {
	if v < 10 {
		return strconv.FormatFloat(v, 'f', 1, 64) + " ms"
	}
	return strconv.Itoa(int(v+0.5)) + " ms"
}

var errTraceUnsupported = errors.New("traceroute is not supported on this system")

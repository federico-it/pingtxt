package check

import (
	"net"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Finding is one conclusion drawn from the diagnostics, in plain words:
// what failed and where to look. The hub works them out when it reads the
// diagnostics, so older agents and stored diagnostics get them too.
type Finding struct {
	// Code names the finding (e.g. "tcp_refused"); the UI translates it
	// with Params. Clients must accept codes they do not know and show
	// Text instead.
	Code   string            `json:"code"`
	Step   string            `json:"step"`  // dns, tcp, tls, http, trace, check
	Level  string            `json:"level"` // down, warn or ok
	Params map[string]string `json:"params,omitempty"`
	Text   string            `json:"text"` // English sentence
}

// Finding levels.
const (
	FindDown = "down"
	FindWarn = "warn"
	FindOK   = "ok"
)

// tlsExpiryWarn is how close to expiry a valid certificate gets a warning.
const tlsExpiryWarn = 14 * 24 * time.Hour

// Findings lists what the diagnostics point at, the most telling first.
func (d *Diag) Findings() []Finding {
	if d == nil {
		return nil
	}
	return d.findingsAt(time.Now())
}

func (d *Diag) findingsAt(now time.Time) []Finding {
	var out []Finding
	add := func(code, step, level, text string, kv ...string) {
		f := Finding{Code: code, Step: step, Level: level, Text: text}
		if len(kv) > 1 {
			f.Params = map[string]string{}
			for i := 0; i+1 < len(kv); i += 2 {
				if kv[i+1] != "" {
					f.Params[kv[i]] = kv[i+1]
				}
			}
		}
		out = append(out, f)
	}
	port := d.Port

	// DNS: the lookup of the host, then the record of a dns monitor.
	dnsFailed := false
	for _, q := range d.DNS {
		if q.Resolver != "system" || !strings.HasPrefix(q.Query, "A/AAAA ") {
			continue
		}
		name := strings.TrimPrefix(q.Query, "A/AAAA ")
		switch {
		case q.Error != "" && isNXDomain(q.Error):
			dnsFailed = true
			add("dns_nxdomain", "dns", FindDown, "The name "+name+" does not exist (NXDOMAIN): check the domain and its DNS records", "host", name)
		case q.Error != "":
			dnsFailed = true
			add("dns_error", "dns", FindDown, "The DNS lookup of "+name+" failed: "+q.Error, "host", name, "error", q.Error)
		case len(q.Answers) == 0:
			dnsFailed = true
			add("dns_empty", "dns", FindDown, "The name "+name+" has no IP address", "host", name)
		}
	}
	d.dnsRecordFindings(add)

	// TCP connections.
	tcpFailed, tcpTimeout := false, false
	if len(d.TCP) > 0 {
		var ok, bad []DiagConn
		for _, c := range d.TCP {
			if c.OK {
				ok = append(ok, c)
			} else {
				bad = append(bad, c)
			}
		}
		switch {
		case len(ok) == 0:
			tcpFailed = true
			e := bad[0].Error
			switch connErrKind(e) {
			case "refused":
				add("tcp_refused", "tcp", FindDown, "Port "+port+" refused the connection: nothing is listening there, or the service is stopped", "port", port)
			case "timeout":
				tcpTimeout = true
				add("tcp_timeout", "tcp", FindDown, "No answer on port "+port+": a firewall drops the packets or the host is off", "port", port)
			case "unreachable":
				add("tcp_unreachable", "tcp", FindDown, "The host cannot be reached from this location (no route): "+e, "port", port, "error", e)
			default:
				add("tcp_error", "tcp", FindDown, "The connection to port "+port+" failed: "+e, "port", port, "error", e)
			}
		case len(bad) > 0:
			if fam := onlyFamily(bad); fam != "" && onlyFamily(ok) != fam {
				add("tcp_family", "tcp", FindWarn, "Port "+port+" works over "+otherFamily(fam)+" but not over "+fam+": "+bad[0].Error, "port", port, "family", fam, "error", bad[0].Error)
			} else {
				add("tcp_partial", "tcp", FindWarn, strconv.Itoa(len(bad))+" of "+strconv.Itoa(len(d.TCP))+" addresses do not answer on port "+port+": "+bad[0].Error,
					"port", port, "failed", strconv.Itoa(len(bad)), "total", strconv.Itoa(len(d.TCP)), "addr", bad[0].Addr, "error", bad[0].Error)
			}
		}
	}

	// TLS.
	tlsFailed := false
	if t := d.TLS; t != nil {
		date := ""
		if t.NotAfter > 0 {
			date = time.Unix(t.NotAfter, 0).UTC().Format("2006-01-02")
		}
		e := strings.ToLower(t.Error)
		switch {
		case t.Error == "":
			if t.NotAfter > 0 {
				if left := time.Unix(t.NotAfter, 0).Sub(now); left < tlsExpiryWarn {
					days := strconv.Itoa(int(left.Hours() / 24))
					add("tls_expiring", "tls", FindWarn, "The certificate expires in "+days+" days ("+date+")", "days", days, "date", date)
				}
			}
		case t.NotAfter > 0 && time.Unix(t.NotAfter, 0).Before(now), strings.Contains(e, "expired"):
			tlsFailed = true
			add("tls_expired", "tls", FindDown, "The certificate expired on "+date, "date", date)
		case strings.Contains(e, "valid for") || strings.Contains(e, "not valid for"):
			tlsFailed = true
			sans := strings.Join(firstN(t.SANs, 3), ", ")
			text := "The certificate is not valid for " + d.Host
			if sans != "" {
				text += " (it is for " + sans + ")"
			}
			add("tls_name", "tls", FindDown, text, "host", d.Host, "sans", sans)
		case strings.Contains(e, "unknown authority") || strings.Contains(e, "self-signed") || strings.Contains(e, "self signed"):
			tlsFailed = true
			add("tls_untrusted", "tls", FindDown, "The certificate is not trusted (self-signed or missing intermediate certificate), issued by "+t.Issuer, "issuer", t.Issuer)
		default:
			tlsFailed = true
			add("tls_error", "tls", FindDown, "The TLS handshake failed: "+t.Error, "error", t.Error)
		}
	}

	// The SMTP greeting.
	if b := d.Banner; b != "" && d.Type == TypeSMTP && !strings.HasPrefix(b, "220") {
		add("smtp_banner", "tcp", FindDown, "The mail server greets with an error: "+b, "banner", b)
	}

	// HTTP status.
	httpBad := false
	if x := d.HTTP; x != nil && x.Status > 0 {
		s := strconv.Itoa(x.Status)
		retry := headerValue(x.Headers, "Retry-After")
		server := headerValue(x.Headers, "Server")
		cf := strings.EqualFold(server, "cloudflare") || headerValue(x.Headers, "CF-Ray") != ""
		httpBad = x.Status >= 400
		switch {
		case cf && (x.Status >= 520 && x.Status <= 527 || x.Status == 530):
			add("http_cloudflare", "http", FindDown, "Cloudflare answers "+s+": it cannot reach the origin server behind it, the problem is there", "status", s)
		case x.Status == 502 || x.Status == 504:
			add("http_gateway", "http", FindDown, "A proxy answers "+s+": the application behind it is down or too slow", "status", s, "server", server)
		case x.Status == 503:
			add("http_unavailable", "http", FindDown, "The server answers 503: the service is unavailable or in maintenance", "status", s, "retry", retry)
		case x.Status == 429:
			add("http_ratelimit", "http", FindDown, "The server answers 429: too many requests, the monitor is rate limited", "status", s, "retry", retry)
		case x.Status == 401 || x.Status == 403:
			add("http_denied", "http", FindDown, "The server answers "+s+": access denied, check credentials, headers or IP allowlists", "status", s)
		case x.Status == 404 || x.Status == 410:
			add("http_notfound", "http", FindDown, "The server answers "+s+": the page does not exist (anymore)", "status", s)
		case x.Status >= 500:
			add("http_server", "http", FindDown, "The server answers "+s+": an error in the application", "status", s, "server", server)
		case x.Status >= 400:
			add("http_client", "http", FindDown, "The server answers "+s+": it refuses the request", "status", s)
		default:
			add("http_status", "http", FindOK, "The server answers "+s+": the request goes through", "status", s)
		}
		if f := &out[len(out)-1]; retry != "" && f.Params["retry"] == "" {
			f.Params["retry"] = retry // every add above passes status, so Params is set
		}
	}

	// The route.
	if t := d.Trace; t != nil && t.Error == "" {
		last := t.LastHop()
		switch {
		case t.Reached && !tcpFailed && (d.Type == TypePing || d.Type == TypeUDP):
			if d.Type == TypePing {
				add("ping_filtered", "trace", FindWarn, "The host is reachable on the network but does not answer the ping: ICMP is probably blocked")
			} else {
				add("udp_silent", "trace", FindWarn, "The host is reachable on the network: the UDP service does not answer")
			}
		case !t.Reached && last == nil:
			add("trace_none", "trace", FindWarn, "No hop of the route answered: the network of this location may be filtering traceroute")
		case !t.Reached && (tcpTimeout || d.TCP == nil):
			where := last.Addr
			if last.Name != "" {
				where = last.Name
			}
			h := strconv.Itoa(last.TTL)
			add("trace_stops", "trace", FindWarn, "The route stops after hop "+h+" ("+where+"): packets get lost from there on", "hop", h, "where", where)
		}
	}

	// Everything on the network side works: the service is the problem.
	if !dnsFailed && !tcpFailed && !tlsFailed && !httpBad && len(d.TCP) > 0 {
		add("network_ok", "check", FindOK, "DNS, connection"+map[bool]string{true: " and TLS", false: ""}[d.TLS != nil]+" work: the problem is in the service or in what the check expects")
	}

	if len(out) == 0 && d.Note != "" {
		add("note", "check", FindOK, d.Note)
	}
	rank := map[string]int{FindDown: 0, FindWarn: 1, FindOK: 2}
	sort.SliceStable(out, func(a, b int) bool { return rank[out[a].Level] < rank[out[b].Level] })
	return out
}

// dnsRecordFindings compares the answers of a dns monitor's own server with
// the system resolver.
func (d *Diag) dnsRecordFindings(add func(code, step, level, text string, kv ...string)) {
	var server, system *DiagDNS
	for i := range d.DNS {
		q := &d.DNS[i]
		if strings.HasPrefix(q.Query, "A/AAAA ") {
			continue // the lookup of a host, not the monitor's record
		}
		if q.Resolver != "system" && server == nil {
			server = q
		} else if q.Resolver == "system" && system == nil {
			system = q
		}
	}
	if system == nil {
		return
	}
	ref := system
	if server != nil {
		ref = server
	}
	switch {
	case ref.Error != "" && isNXDomain(ref.Error):
		add("dns_record_nxdomain", "dns", FindDown, "The query "+ref.Query+" says the name does not exist (NXDOMAIN)", "query", ref.Query)
	case ref.Error != "":
		add("dns_record_error", "dns", FindDown, "The query "+ref.Query+" failed: "+ref.Error, "query", ref.Query, "error", ref.Error, "resolver", ref.Resolver)
	case len(ref.Answers) == 0:
		add("dns_record_empty", "dns", FindDown, "The query "+ref.Query+" returns no records", "query", ref.Query)
	}
	if server == nil {
		return
	}
	switch {
	case server.Error != "" && system.Error == "" && len(system.Answers) > 0:
		add("dns_server_only", "dns", FindWarn, "Only the server "+server.Resolver+" fails: the system resolver answers "+strings.Join(firstN(system.Answers, 3), ", "), "server", server.Resolver, "answers", strings.Join(firstN(system.Answers, 3), ", "))
	case server.Error == "" && system.Error == "" && !sameSet(server.Answers, system.Answers):
		add("dns_mismatch", "dns", FindWarn, "The server "+server.Resolver+" and the system resolver give different answers: a change not propagated yet, or a stale cache", "server", server.Resolver)
	}
}

func isNXDomain(e string) bool {
	e = strings.ToLower(e)
	return strings.Contains(e, "no such host") || strings.Contains(e, "nxdomain")
}

// connErrKind sorts a connection error: refused, timeout, unreachable or "".
func connErrKind(e string) string {
	e = strings.ToLower(e)
	switch {
	case strings.Contains(e, "refused"):
		return "refused"
	case strings.Contains(e, "timeout") || strings.Contains(e, "timed out") || strings.Contains(e, "deadline"):
		return "timeout"
	case strings.Contains(e, "unreachable") || strings.Contains(e, "no route"):
		return "unreachable"
	}
	return ""
}

// onlyFamily is "IPv4" or "IPv6" when every connection used that family.
func onlyFamily(list []DiagConn) string {
	fam := ""
	for _, c := range list {
		h, _, err := net.SplitHostPort(c.Addr)
		ip := net.ParseIP(h)
		if err != nil || ip == nil {
			return ""
		}
		f := "IPv6"
		if ip.To4() != nil {
			f = "IPv4"
		}
		if fam != "" && fam != f {
			return ""
		}
		fam = f
	}
	return fam
}

func otherFamily(f string) string {
	if f == "IPv4" {
		return "IPv6"
	}
	return "IPv4"
}

func headerValue(h []DiagHeader, name string) string {
	for _, x := range h {
		if strings.EqualFold(x.Name, name) {
			return x.Value
		}
	}
	return ""
}

func firstN(s []string, n int) []string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

func sameSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	m := map[string]int{}
	for _, x := range a {
		m[strings.ToLower(x)]++
	}
	for _, x := range b {
		m[strings.ToLower(x)]--
	}
	for _, v := range m {
		if v != 0 {
			return false
		}
	}
	return true
}

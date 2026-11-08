// Package check runs a single monitoring probe. The same code is used by the
// hub (for its built-in "local" location) and by remote agents.
package check

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Supported monitor types.
const (
	TypeHTTP     = "http"
	TypeKeyword  = "keyword"
	TypeTCP      = "tcp"
	TypePing     = "ping"
	TypeDNS      = "dns"
	TypePostgres = "postgres"
	TypeMySQL    = "mysql"
	TypeRedis    = "redis"
	TypeMongo    = "mongodb"
	TypeDocker   = "docker"
	TypeGRPC     = "grpc"
	TypeSMTP     = "smtp"
	TypeMCP      = "mcp"
	TypeUDP      = "udp"
	TypeWS       = "websocket"
	// TypePush is a heartbeat monitor: the monitored job calls the hub, so
	// it is never probed and Run does not handle it.
	TypePush = "push"
)

// Job describes one probe to execute.
type Job struct {
	ID             string            `json:"id"`
	MonitorID      int64             `json:"monitor_id"`
	Type           string            `json:"type"`
	Target         string            `json:"target"`
	TimeoutMs      int               `json:"timeout_ms"`
	Method         string            `json:"method,omitempty"`
	ExpectedStatus string            `json:"expected_status,omitempty"`
	Keyword        string            `json:"keyword,omitempty"`
	KeywordInvert  bool              `json:"keyword_invert,omitempty"`
	DNSRecord      string            `json:"dns_record,omitempty"`
	DNSServer      string            `json:"dns_server,omitempty"`
	DNSExpected    string            `json:"dns_expected,omitempty"`
	IgnoreTLS      bool              `json:"ignore_tls,omitempty"`
	Headers        map[string]string `json:"headers,omitempty"`
	Body           string            `json:"body,omitempty"`
	// Assertions are rules on the JSON body of http/keyword responses.
	Assertions []Assertion `json:"assertions,omitempty"`
	// IPFamily limits the check to IPv4 (4) or IPv6 (6), or runs it over
	// both (46); 0 uses either.
	IPFamily int `json:"ip_family,omitempty"`
	// Diagnose asks for diagnostics (DNS, TCP, TLS, HTTP headers,
	// traceroute) if the check fails. Older agents ignore it.
	Diagnose bool `json:"diagnose,omitempty"`
	// PingCount is how many echo requests a ping check sends; 0 means
	// DefaultPingCount. Older agents ignore it and send DefaultPingCount.
	PingCount int `json:"ping_count,omitempty"`
	// PingLossPct fails a ping check when at least this share (percent) of
	// the packets is lost; 0 = only when all are lost.
	PingLossPct int `json:"ping_loss_pct,omitempty"`
	// Steps are HTTP requests made before the http/keyword request, e.g. a
	// login. Older agents ignore them.
	Steps []Step `json:"steps,omitempty"`
}

// Result is the outcome of a Job.
type Result struct {
	JobID      string  `json:"job_id"`
	MonitorID  int64   `json:"monitor_id"`
	OK         bool    `json:"ok"`
	LatencyMs  float64 `json:"latency_ms"`
	StatusCode int     `json:"status_code,omitempty"`
	Message    string  `json:"message,omitempty"`
	CertExpiry int64   `json:"cert_expiry,omitempty"` // unix seconds
	// Asserted is how many response rules passed. The hub uses it to spot
	// agents too old to check them.
	Asserted int `json:"asserted,omitempty"`
	// Timing splits the latency of http/keyword checks into phases. Agents
	// older than this field never send it.
	Timing *Timing `json:"timing,omitempty"`
	// IPFamily repeats the job's IP family, so the hub can tell agents that
	// do not know the setting (and would use either family) from those that do.
	IPFamily int `json:"ip_family,omitempty"`
	// Both is set when the check ran over IPv4 and IPv6 (IPBoth). Agents
	// older than it echo IPBoth but check over either family.
	Both bool  `json:"both,omitempty"`
	At   int64 `json:"at"` // unix milliseconds
	// Diag holds the diagnostics of a failed check, when the job asked for
	// them.
	Diag *Diag `json:"diag,omitempty"`

	http *DiagHTTP // response of a failed HTTP check, for Diagnose
	// PingSent is how many echo requests a ping check sent. Agents older
	// than the packet loss threshold never set it.
	PingSent int `json:"ping_sent,omitempty"`
	// Steps is how many steps of the job ran (the failed one included).
	// Agents older than steps never set it.
	Steps int `json:"steps,omitempty"`
}

// Timing is the time spent in each phase of an HTTP request, in ms. With
// redirects the phases of every hop are added up. Whatever is left of the
// total latency is reading the response body.
type Timing struct {
	DNSMs     float64 `json:"dns_ms"`
	ConnectMs float64 `json:"connect_ms"`
	TLSMs     float64 `json:"tls_ms"`
	// TTFBMs is the wait from the connection being ready (request sent) to
	// the first byte of the response.
	TTFBMs float64 `json:"ttfb_ms"`
}

// Run executes the job and always returns a Result.
func Run(ctx context.Context, j Job) Result {
	r := runJob(ctx, j)
	if j.Diagnose && !r.OK && j.Type != TypePush {
		r.Diag = Diagnose(ctx, j, r)
	}
	r.http = nil
	return r
}

func runJob(ctx context.Context, j Job) Result {
	if j.IPFamily == IPBoth {
		r := runBoth(ctx, j, runJob)
		r.IPFamily = IPBoth
		r.Message = truncate(r.Message, 300)
		return r
	}
	timeout := time.Duration(j.TimeoutMs) * time.Millisecond
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var r Result
	switch j.Type {
	case TypeHTTP, TypeKeyword:
		r = runHTTP(ctx, j, timeout)
	case TypeTCP:
		r = runTCP(ctx, j)
	case TypePing:
		r = runPing(ctx, j, timeout)
	case TypeDNS:
		r = runDNS(ctx, j)
	case TypePostgres, TypeMySQL:
		r = runSQL(ctx, j)
	case TypeRedis:
		r = runRedis(ctx, j)
	case TypeMongo:
		r = runMongo(ctx, j)
	case TypeDocker:
		r = runDocker(ctx, j)
	case TypeGRPC:
		r = runGRPC(ctx, j, timeout)
	case TypeSMTP:
		r = runSMTP(ctx, j)
	case TypeMCP:
		r = runMCP(ctx, j, timeout)
	case TypeUDP:
		r = runUDP(ctx, j)
	case TypeWS:
		r = runWebSocket(ctx, j, timeout)
	default:
		r = Result{Message: "unknown monitor type: " + j.Type}
	}
	r.JobID = j.ID
	r.MonitorID = j.MonitorID
	r.IPFamily = j.IPFamily
	r.At = time.Now().UnixMilli()
	r.Message = truncate(r.Message, 300)
	return r
}

func ms(d time.Duration) float64 { return float64(d.Microseconds()) / 1000 }

const userAgent = "pingtxt/1.0 (+https://github.com/federico-it/pingtxt)"

func runHTTP(ctx context.Context, j Job, timeout time.Duration) Result {
	method := strings.ToUpper(j.Method)
	if method == "" {
		method = http.MethodGet
	}
	tr := &http.Transport{
		Proxy:               http.ProxyFromEnvironment,
		TLSClientConfig:     &tls.Config{InsecureSkipVerify: j.IgnoreTLS},
		DisableKeepAlives:   true,
		TLSHandshakeTimeout: timeout,
		DialContext:         j.dialContext,
	}
	defer tr.CloseIdleConnections()
	var redirects []string
	follow := dropHeadersOnRedirect(j.Headers)
	client := &http.Client{Transport: tr, Timeout: timeout, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if j.Diagnose {
			redirects = append(redirects, redirectURL(req.URL))
		}
		return follow(req, via)
	}}

	pt := &phaseTracer{}
	ct := pt.trace()
	var remote string
	if j.Diagnose {
		got := ct.GotConn
		ct.GotConn = func(info httptrace.GotConnInfo) {
			got(info)
			if info.Conn != nil {
				pt.mu.Lock()
				remote = info.Conn.RemoteAddr().String()
				pt.mu.Unlock()
			}
		}
	}
	ctx = httptrace.WithClientTrace(ctx, ct)
	start := time.Now()

	// Steps (a login...) run first; what they extract fills {{variables}}
	// in the monitor's own request.
	target, headers, reqBody := j.Target, j.Headers, j.Body
	steps := 0
	if len(j.Steps) > 0 {
		client.Jar = newCookieJar()
		vars, n, failed := runSteps(ctx, j, client, pt)
		steps = n
		if failed != nil {
			failed.LatencyMs = ms(time.Since(start))
			failed.Steps = steps
			return *failed
		}
		target, headers, reqBody = expand(target, vars), expandHeaders(headers, vars), expand(reqBody, vars)
	}

	var body io.Reader
	if reqBody != "" {
		body = strings.NewReader(reqBody)
	}
	req, err := http.NewRequestWithContext(ctx, method, target, body)
	if err != nil {
		return Result{Message: err.Error(), Steps: steps}
	}
	req.Header.Set("User-Agent", userAgent)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := client.Do(req)
	if err != nil {
		return Result{LatencyMs: ms(time.Since(start)), Message: cleanErr(err), Steps: steps, Timing: pt.timing()}
	}
	defer resp.Body.Close()

	r := Result{StatusCode: resp.StatusCode, Timing: pt.timing(), Steps: steps}
	if j.Diagnose {
		pt.mu.Lock()
		r.http = diagHTTP(resp, remote, redirects)
		pt.mu.Unlock()
	}
	if resp.TLS != nil && len(resp.TLS.PeerCertificates) > 0 {
		r.CertExpiry = resp.TLS.PeerCertificates[0].NotAfter.Unix()
	}

	var content []byte
	if j.Type == TypeKeyword || len(j.Assertions) > 0 {
		content, _ = io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	} else {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	}
	r.LatencyMs = ms(time.Since(start))

	if !StatusMatches(j.ExpectedStatus, resp.StatusCode) {
		r.Message = fmt.Sprintf("unexpected status %d", resp.StatusCode)
		return r
	}
	if j.Type == TypeKeyword {
		found := strings.Contains(string(content), j.Keyword)
		if found == j.KeywordInvert {
			if j.KeywordInvert {
				r.Message = fmt.Sprintf("keyword %q found", j.Keyword)
			} else {
				r.Message = fmt.Sprintf("keyword %q not found", j.Keyword)
			}
			return r
		}
	}
	if len(j.Assertions) > 0 {
		var msg string
		r.Asserted, msg = CheckAssertions(content, j.Assertions)
		if msg != "" {
			r.Message = msg
			return r
		}
	}
	r.OK = true
	r.Message = resp.Status
	return r
}

// phaseTracer adds up the phases of every request a check makes (one per
// redirect hop). Dial callbacks can run on other goroutines (Happy
// Eyeballs), hence the mutex.
type phaseTracer struct {
	mu                   sync.Mutex
	dnsStart, connStart  time.Time
	tlsStart, connReady  time.Time
	dns, conn, tls, wait time.Duration
	gotByte              bool
}

func (p *phaseTracer) trace() *httptrace.ClientTrace {
	lock := func(f func()) { p.mu.Lock(); f(); p.mu.Unlock() }
	return &httptrace.ClientTrace{
		DNSStart: func(httptrace.DNSStartInfo) { lock(func() { p.dnsStart = time.Now() }) },
		DNSDone: func(httptrace.DNSDoneInfo) {
			lock(func() {
				if !p.dnsStart.IsZero() {
					p.dns += time.Since(p.dnsStart)
					p.dnsStart = time.Time{}
				}
			})
		},
		ConnectStart: func(string, string) {
			lock(func() {
				if p.connStart.IsZero() { // first of parallel dials
					p.connStart = time.Now()
				}
			})
		},
		ConnectDone: func(_, _ string, err error) {
			lock(func() {
				if err == nil && !p.connStart.IsZero() {
					p.conn += time.Since(p.connStart)
					p.connStart = time.Time{}
				}
			})
		},
		TLSHandshakeStart: func() { lock(func() { p.tlsStart = time.Now() }) },
		TLSHandshakeDone: func(tls.ConnectionState, error) {
			lock(func() {
				if !p.tlsStart.IsZero() {
					p.tls += time.Since(p.tlsStart)
					p.tlsStart = time.Time{}
				}
			})
		},
		GotConn: func(httptrace.GotConnInfo) { lock(func() { p.connReady = time.Now() }) },
		GotFirstResponseByte: func() {
			lock(func() {
				if !p.connReady.IsZero() {
					p.wait += time.Since(p.connReady)
					p.connReady = time.Time{}
					p.gotByte = true
				}
			})
		},
	}
}

func (p *phaseTracer) timing() *Timing {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.gotByte {
		return nil
	}
	return &Timing{DNSMs: ms(p.dns), ConnectMs: ms(p.conn), TLSMs: ms(p.tls), TTFBMs: ms(p.wait)}
}

// dropHeadersOnRedirect keeps the monitor's own request headers (API keys,
// tokens) from following a redirect to another host. Go already drops
// Authorization and Cookie there, but not custom headers like X-Api-Key.
func dropHeadersOnRedirect(hdr map[string]string) func(*http.Request, []*http.Request) error {
	return func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return errors.New("stopped after 10 redirects")
		}
		if !strings.EqualFold(req.URL.Host, via[0].URL.Host) {
			for k := range hdr {
				req.Header.Del(k)
			}
		}
		return nil
	}
}

// StatusMatches checks a status code against a spec like "200-299,301".
// An empty spec means 2xx/3xx.
func StatusMatches(spec string, code int) bool {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return code >= 200 && code < 400
	}
	for _, part := range strings.Split(spec, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if lo, hi, ok := strings.Cut(part, "-"); ok {
			l, e1 := strconv.Atoi(strings.TrimSpace(lo))
			h, e2 := strconv.Atoi(strings.TrimSpace(hi))
			if e1 == nil && e2 == nil && code >= l && code <= h {
				return true
			}
			continue
		}
		if strings.HasSuffix(strings.ToLower(part), "xx") && len(part) == 3 {
			if d, err := strconv.Atoi(part[:1]); err == nil && code/100 == d {
				return true
			}
			continue
		}
		if c, err := strconv.Atoi(part); err == nil && c == code {
			return true
		}
	}
	return false
}

func runTCP(ctx context.Context, j Job) Result {
	start := time.Now()
	conn, err := j.dialContext(ctx, "tcp", j.Target)
	lat := ms(time.Since(start))
	if err != nil {
		return Result{LatencyMs: lat, Message: cleanErr(err)}
	}
	_ = conn.Close()
	return Result{OK: true, LatencyMs: lat, Message: "port open"}
}

func runDNS(ctx context.Context, j Job) Result {
	host := StripBrackets(j.Target)
	if u, err := url.Parse(host); err == nil && u.Host != "" {
		host = u.Hostname()
	}
	if !knownRecord(j.DNSRecord) {
		return Result{Message: "unsupported record type " + j.DNSRecord}
	}
	start := time.Now()
	answers, err := lookupRecord(ctx, j.resolver(), j.DNSRecord, host)
	lat := ms(time.Since(start))
	if err != nil {
		return Result{LatencyMs: lat, Message: cleanErr(err)}
	}
	if len(answers) == 0 {
		return Result{LatencyMs: lat, Message: "no records"}
	}
	msg := strings.Join(answers, ", ")
	if j.DNSExpected != "" {
		match := false
		for _, a := range answers {
			if strings.EqualFold(strings.TrimSuffix(a, "."), strings.TrimSuffix(j.DNSExpected, ".")) || strings.Contains(a, j.DNSExpected) {
				match = true
				break
			}
		}
		if !match {
			return Result{LatencyMs: lat, Message: "expected " + j.DNSExpected + ", got " + msg}
		}
	}
	return Result{OK: true, LatencyMs: lat, Message: msg}
}

// resolver is the monitor's DNS server, or the system resolver (reached
// over the job's IP family).
func (j Job) resolver() *net.Resolver {
	if j.DNSServer != "" {
		server := j.DNSServer
		if _, _, err := net.SplitHostPort(server); err != nil {
			server = net.JoinHostPort(StripBrackets(server), "53")
		}
		return &net.Resolver{
			PreferGo: true,
			Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
				return j.dialContext(ctx, network, server)
			},
		}
	}
	if j.IPFamily != IPAny {
		return &net.Resolver{PreferGo: true, Dial: j.dialContext}
	}
	return net.DefaultResolver
}

func knownRecord(rec string) bool {
	switch strings.ToUpper(rec) {
	case "", "A", "AAAA", "CNAME", "MX", "TXT", "NS":
		return true
	}
	return false
}

// lookupRecord queries one record type ("" is A).
func lookupRecord(ctx context.Context, res *net.Resolver, rec, host string) ([]string, error) {
	var answers []string
	var err error
	switch strings.ToUpper(rec) {
	case "", "A", "AAAA":
		var ips []net.IP
		network := "ip4"
		if strings.ToUpper(rec) == "AAAA" {
			network = "ip6"
		}
		ips, err = res.LookupIP(ctx, network, host)
		for _, ip := range ips {
			answers = append(answers, ip.String())
		}
	case "CNAME":
		var c string
		c, err = res.LookupCNAME(ctx, host)
		if c != "" {
			answers = []string{c}
		}
	case "MX":
		var mx []*net.MX
		mx, err = res.LookupMX(ctx, host)
		for _, m := range mx {
			answers = append(answers, m.Host)
		}
	case "TXT":
		answers, err = res.LookupTXT(ctx, host)
	case "NS":
		var ns []*net.NS
		ns, err = res.LookupNS(ctx, host)
		for _, n := range ns {
			answers = append(answers, n.Host)
		}
	default:
		err = errors.New("unsupported record type " + rec)
	}
	return answers, err
}

// RedactTarget hides the password of URL targets (database connection
// strings) for display in notifications and logs.
func RedactTarget(target string) string {
	if !strings.Contains(target, "@") || !strings.Contains(target, "://") {
		return target
	}
	u, err := url.Parse(target)
	if err != nil {
		return target
	}
	if _, ok := u.User.Password(); !ok {
		return target
	}
	u.User = url.UserPassword(u.User.Username(), "xxxxx")
	return u.String()
}

// CleanErr shortens common network errors for display.
func CleanErr(err error) string { return cleanErr(err) }

func cleanErr(err error) string {
	if err == nil {
		return ""
	}
	if ue, ok := err.(*url.Error); ok {
		err = ue.Err
	}
	if err == context.DeadlineExceeded || strings.Contains(err.Error(), "deadline exceeded") || strings.Contains(err.Error(), "Client.Timeout") {
		return "timeout"
	}
	return err.Error()
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

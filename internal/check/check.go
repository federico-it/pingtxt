// Package check runs a single monitoring probe. The same code is used by the
// hub (for its built-in "local" location) and by remote agents.
package check

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	probing "github.com/prometheus-community/pro-bing"
)

// Supported monitor types.
const (
	TypeHTTP    = "http"
	TypeKeyword = "keyword"
	TypeTCP     = "tcp"
	TypePing    = "ping"
	TypeDNS     = "dns"
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
	At         int64   `json:"at"`                    // unix milliseconds
}

// Run executes the job and always returns a Result.
func Run(ctx context.Context, j Job) Result {
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
	default:
		r = Result{Message: "unknown monitor type: " + j.Type}
	}
	r.JobID = j.ID
	r.MonitorID = j.MonitorID
	r.At = time.Now().UnixMilli()
	r.Message = truncate(r.Message, 300)
	return r
}

func ms(d time.Duration) float64 { return float64(d.Microseconds()) / 1000 }

func runHTTP(ctx context.Context, j Job, timeout time.Duration) Result {
	method := strings.ToUpper(j.Method)
	if method == "" {
		method = http.MethodGet
	}
	var body io.Reader
	if j.Body != "" {
		body = strings.NewReader(j.Body)
	}
	req, err := http.NewRequestWithContext(ctx, method, j.Target, body)
	if err != nil {
		return Result{Message: err.Error()}
	}
	req.Header.Set("User-Agent", "pingtxt/1.0 (+https://github.com/federico-it/pingtxt)")
	for k, v := range j.Headers {
		req.Header.Set(k, v)
	}
	tr := &http.Transport{
		Proxy:               http.ProxyFromEnvironment,
		TLSClientConfig:     &tls.Config{InsecureSkipVerify: j.IgnoreTLS},
		DisableKeepAlives:   true,
		TLSHandshakeTimeout: timeout,
	}
	defer tr.CloseIdleConnections()
	client := &http.Client{Transport: tr, Timeout: timeout}

	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return Result{LatencyMs: ms(time.Since(start)), Message: cleanErr(err)}
	}
	defer resp.Body.Close()

	r := Result{StatusCode: resp.StatusCode}
	if resp.TLS != nil && len(resp.TLS.PeerCertificates) > 0 {
		r.CertExpiry = resp.TLS.PeerCertificates[0].NotAfter.Unix()
	}

	var content []byte
	if j.Type == TypeKeyword {
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
	r.OK = true
	r.Message = resp.Status
	return r
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
	var d net.Dialer
	start := time.Now()
	conn, err := d.DialContext(ctx, "tcp", j.Target)
	lat := ms(time.Since(start))
	if err != nil {
		return Result{LatencyMs: lat, Message: cleanErr(err)}
	}
	_ = conn.Close()
	return Result{OK: true, LatencyMs: lat, Message: "port open"}
}

func runPing(ctx context.Context, j Job, timeout time.Duration) Result {
	p, err := probing.NewPinger(j.Target)
	if err != nil {
		return Result{Message: cleanErr(err)}
	}
	p.Count = 3
	p.Interval = 300 * time.Millisecond
	p.Timeout = timeout
	// Raw sockets need CAP_NET_RAW; unprivileged UDP ping needs
	// net.ipv4.ping_group_range. Default to privileged (works in Docker as root).
	p.SetPrivileged(os.Getenv("PINGTXT_UNPRIVILEGED_PING") == "")
	if err := p.RunWithContext(ctx); err != nil {
		return Result{Message: cleanErr(err)}
	}
	s := p.Statistics()
	if s.PacketsRecv == 0 {
		return Result{Message: fmt.Sprintf("100%% packet loss (%s)", s.IPAddr)}
	}
	return Result{
		OK:        true,
		LatencyMs: ms(s.AvgRtt),
		Message:   fmt.Sprintf("%d/%d packets, %.0f%% loss", s.PacketsRecv, s.PacketsSent, s.PacketLoss),
	}
}

func runDNS(ctx context.Context, j Job) Result {
	res := net.DefaultResolver
	if j.DNSServer != "" {
		server := j.DNSServer
		if _, _, err := net.SplitHostPort(server); err != nil {
			server = net.JoinHostPort(server, "53")
		}
		res = &net.Resolver{
			PreferGo: true,
			Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, network, server)
			},
		}
	}
	host := j.Target
	if u, err := url.Parse(host); err == nil && u.Host != "" {
		host = u.Hostname()
	}
	start := time.Now()
	var answers []string
	var err error
	switch strings.ToUpper(j.DNSRecord) {
	case "", "A", "AAAA":
		var ips []net.IP
		network := "ip4"
		if strings.ToUpper(j.DNSRecord) == "AAAA" {
			network = "ip6"
		}
		ips, err = res.LookupIP(ctx, network, host)
		for _, ip := range ips {
			answers = append(answers, ip.String())
		}
	case "CNAME":
		var c string
		c, err = res.LookupCNAME(ctx, host)
		answers = []string{c}
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
		return Result{Message: "unsupported record type " + j.DNSRecord}
	}
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

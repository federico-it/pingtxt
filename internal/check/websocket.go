package check

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/coder/websocket"
)

// runWebSocket opens a WebSocket connection (ws:// or wss://). If Body is
// set it is sent as a text message after connecting; if Body or Keyword is
// set the check waits for the first message from the server, which must
// contain Keyword. Headers go with the handshake; Sec-WebSocket-Protocol
// lists the subprotocols to ask for.
func runWebSocket(ctx context.Context, j Job, timeout time.Duration) Result {
	tr := &http.Transport{
		Proxy:               http.ProxyFromEnvironment,
		TLSClientConfig:     &tls.Config{InsecureSkipVerify: j.IgnoreTLS},
		DisableKeepAlives:   true,
		TLSHandshakeTimeout: timeout,
		DialContext:         j.dialContext,
	}
	defer tr.CloseIdleConnections()
	hdr := http.Header{}
	hdr.Set("User-Agent", userAgent)
	var protocols []string
	for k, v := range j.Headers {
		if strings.EqualFold(k, "Sec-WebSocket-Protocol") {
			for _, p := range strings.Split(v, ",") {
				if p = strings.TrimSpace(p); p != "" {
					protocols = append(protocols, p)
				}
			}
			continue
		}
		hdr.Set(k, v)
	}
	start := time.Now()
	conn, resp, err := websocket.Dial(ctx, j.Target, &websocket.DialOptions{
		HTTPClient:   &http.Client{Transport: tr, CheckRedirect: dropHeadersOnRedirect(j.Headers)},
		HTTPHeader:   hdr,
		Subprotocols: protocols,
	})
	r := Result{}
	if resp != nil {
		r.StatusCode = resp.StatusCode
		if resp.TLS != nil && len(resp.TLS.PeerCertificates) > 0 {
			r.CertExpiry = resp.TLS.PeerCertificates[0].NotAfter.Unix()
		}
	}
	if err != nil {
		r.LatencyMs = ms(time.Since(start))
		r.Message = wsErr(err, resp)
		return r
	}
	defer conn.CloseNow()
	conn.SetReadLimit(1 << 20)
	r.LatencyMs = ms(time.Since(start))
	r.Message = "connected"
	if p := conn.Subprotocol(); p != "" {
		r.Message += " (" + p + ")"
	}
	if j.Body == "" && j.Keyword == "" {
		_ = conn.Close(websocket.StatusNormalClosure, "")
		r.OK = true
		return r
	}
	if j.Body != "" {
		if err := conn.Write(ctx, websocket.MessageText, []byte(j.Body)); err != nil {
			r.LatencyMs = ms(time.Since(start))
			r.Message = "send: " + wsErr(err, nil)
			return r
		}
	}
	_, msg, err := conn.Read(ctx)
	r.LatencyMs = ms(time.Since(start))
	if err != nil {
		r.Message = "no reply: " + wsErr(err, nil)
		return r
	}
	if j.Keyword != "" && !strings.Contains(string(msg), j.Keyword) {
		r.Message = fmt.Sprintf("reply does not contain %q: got %q", j.Keyword, truncate(string(msg), 60))
		return r
	}
	_ = conn.Close(websocket.StatusNormalClosure, "")
	r.OK = true
	r.Message = fmt.Sprintf("reply %d bytes", len(msg))
	return r
}

func wsErr(err error, resp *http.Response) string {
	if resp != nil && resp.StatusCode != http.StatusSwitchingProtocols {
		return fmt.Sprintf("unexpected status %d (no WebSocket upgrade)", resp.StatusCode)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	var ce websocket.CloseError
	if errors.As(err, &ce) {
		if ce.Reason != "" {
			return fmt.Sprintf("closed by the server: %d %s", ce.Code, ce.Reason)
		}
		return fmt.Sprintf("closed by the server: %d", ce.Code)
	}
	if ue, ok := errors.Unwrap(err).(*url.Error); ok {
		return cleanErr(ue)
	}
	msg := cleanErr(err)
	msg = strings.TrimPrefix(msg, "failed to WebSocket dial: ")
	msg = strings.TrimPrefix(msg, "failed to send handshake request: ")
	return msg
}

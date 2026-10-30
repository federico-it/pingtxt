package check

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// runGRPC calls the standard health service (grpc.health.v1.Health/Check)
// over HTTP/2 with net/http, without the grpc-go dependency.
//
//	grpc://host:port[/service]    plaintext (h2c)
//	grpcs://host:port[/service]   TLS
func runGRPC(ctx context.Context, j Job, timeout time.Duration) Result {
	addr, service, secure, err := parseGRPCTarget(j.Target)
	if err != nil {
		return Result{Message: err.Error()}
	}
	// Request: health.v1.HealthCheckRequest{service = 1}, length-prefixed.
	var msg []byte
	if service != "" {
		msg = append([]byte{0x0a}, binary.AppendUvarint(nil, uint64(len(service)))...)
		msg = append(msg, service...)
	}
	frame := make([]byte, 5, 5+len(msg))
	binary.BigEndian.PutUint32(frame[1:], uint32(len(msg)))
	frame = append(frame, msg...)

	tr := &http.Transport{
		TLSClientConfig:     &tls.Config{InsecureSkipVerify: j.IgnoreTLS}, //nolint:gosec // user opt-in
		DisableKeepAlives:   true,
		TLSHandshakeTimeout: timeout,
		Protocols:           new(http.Protocols),
	}
	scheme := "http"
	if secure {
		scheme = "https"
		tr.Protocols.SetHTTP2(true)
	} else {
		tr.Protocols.SetUnencryptedHTTP2(true)
	}
	defer tr.CloseIdleConnections()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, scheme+"://"+addr+"/grpc.health.v1.Health/Check", bytes.NewReader(frame))
	if err != nil {
		return Result{Message: err.Error()}
	}
	req.Header.Set("Content-Type", "application/grpc")
	req.Header.Set("TE", "trailers")
	req.Header.Set("User-Agent", "pingtxt/1.0")
	req.Header.Set("Grpc-Timeout", strconv.FormatInt(max(timeout.Milliseconds(), 1), 10)+"m")

	start := time.Now()
	resp, err := (&http.Client{Transport: tr}).Do(req)
	if err != nil {
		return Result{LatencyMs: ms(time.Since(start)), Message: cleanErr(err)}
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	lat := ms(time.Since(start))
	r := Result{LatencyMs: lat, StatusCode: resp.StatusCode}
	if resp.TLS != nil && len(resp.TLS.PeerCertificates) > 0 {
		r.CertExpiry = resp.TLS.PeerCertificates[0].NotAfter.Unix()
	}
	if err != nil {
		r.Message = cleanErr(err)
		return r
	}
	if resp.StatusCode != http.StatusOK {
		r.Message = fmt.Sprintf("http status %d (not a grpc server?)", resp.StatusCode)
		return r
	}
	// Trailers-only responses carry grpc-status in the headers.
	gs, gm := resp.Trailer.Get("Grpc-Status"), resp.Trailer.Get("Grpc-Message")
	if gs == "" {
		gs, gm = resp.Header.Get("Grpc-Status"), resp.Header.Get("Grpc-Message")
	}
	if gm, _ = url.PathUnescape(gm); gs != "" && gs != "0" {
		r.Message = grpcCodeName(gs)
		switch gs {
		case "5":
			r.Message = "service " + strconv.Quote(service) + " unknown"
		case "12":
			r.Message = "health service not implemented"
		}
		if gm != "" && gs != "12" {
			r.Message += ": " + gm
		}
		return r
	}
	status, err := parseHealthResponse(body)
	if err != nil {
		r.Message = err.Error()
		return r
	}
	r.Message = status
	r.OK = status == "SERVING"
	return r
}

func parseGRPCTarget(target string) (addr, service string, secure bool, err error) {
	t := strings.TrimSpace(target)
	if !strings.Contains(t, "://") {
		t = "grpc://" + t
	}
	u, perr := url.Parse(t)
	if perr != nil || (u.Scheme != "grpc" && u.Scheme != "grpcs") || u.Host == "" || u.Port() == "" {
		return "", "", false, errors.New("target must be grpc://host:port or grpcs://host:port, optionally followed by /service")
	}
	return u.Host, strings.Trim(u.Path, "/"), u.Scheme == "grpcs", nil
}

// parseHealthResponse decodes the length-prefixed HealthCheckResponse.
func parseHealthResponse(b []byte) (string, error) {
	if len(b) < 5 {
		return "", errors.New("empty grpc response")
	}
	if b[0] != 0 {
		return "", errors.New("compressed grpc response not supported")
	}
	n := binary.BigEndian.Uint32(b[1:5])
	if int(n) > len(b)-5 {
		return "", errors.New("truncated grpc response")
	}
	m := b[5 : 5+n]
	status := uint64(0) // proto3 default: UNKNOWN
	for len(m) > 0 {
		key, k := binary.Uvarint(m)
		if k <= 0 {
			return "", errors.New("invalid grpc response")
		}
		m = m[k:]
		switch key & 7 {
		case 0:
			v, k := binary.Uvarint(m)
			if k <= 0 {
				return "", errors.New("invalid grpc response")
			}
			if key>>3 == 1 {
				status = v
			}
			m = m[k:]
		case 2:
			l, k := binary.Uvarint(m)
			if k <= 0 || uint64(len(m)-k) < l {
				return "", errors.New("invalid grpc response")
			}
			m = m[k+int(l):]
		default:
			return "", errors.New("invalid grpc response")
		}
	}
	switch status {
	case 1:
		return "SERVING", nil
	case 2:
		return "NOT_SERVING", nil
	case 3:
		return "SERVICE_UNKNOWN", nil
	}
	return "UNKNOWN", nil
}

func grpcCodeName(code string) string {
	names := []string{"OK", "CANCELLED", "UNKNOWN", "INVALID_ARGUMENT", "DEADLINE_EXCEEDED", "NOT_FOUND",
		"ALREADY_EXISTS", "PERMISSION_DENIED", "RESOURCE_EXHAUSTED", "FAILED_PRECONDITION", "ABORTED",
		"OUT_OF_RANGE", "UNIMPLEMENTED", "INTERNAL", "UNAVAILABLE", "DATA_LOSS", "UNAUTHENTICATED"}
	if c, err := strconv.Atoi(code); err == nil && c >= 0 && c < len(names) {
		return names[c]
	}
	return "grpc status " + code
}

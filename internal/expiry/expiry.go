// Package expiry looks up when TLS certificates and domain registrations
// expire. Domains are looked up with RDAP (IANA bootstrap) and, for registries
// without RDAP or without an expiry date in RDAP (e.g. .it), with WHOIS.
package expiry

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"net/url"
	"strings"
	"time"

	"golang.org/x/net/publicsuffix"
)

// CertInfo describes the leaf certificate served by a host.
type CertInfo struct {
	NotAfter time.Time
	Subject  string
	Issuer   string
	// VerifyErr is set when the chain does not validate for the host name
	// (self-signed, wrong name, expired, unknown CA...).
	VerifyErr string
}

// Cert connects to host:port with TLS and returns the leaf certificate info.
// Verification is done after the handshake so that expired or invalid
// certificates are still reported with their dates.
func Cert(ctx context.Context, host, port string, timeout time.Duration) (*CertInfo, error) {
	if port == "" {
		port = "443"
	}
	d := &tls.Dialer{
		NetDialer: &net.Dialer{Timeout: timeout},
		Config:    &tls.Config{ServerName: host, InsecureSkipVerify: true}, //nolint:gosec // verified below
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	conn, err := d.DialContext(ctx, "tcp", net.JoinHostPort(host, port))
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	state := conn.(*tls.Conn).ConnectionState()
	if len(state.PeerCertificates) == 0 {
		return nil, errors.New("no certificate presented")
	}
	leaf := state.PeerCertificates[0]
	info := &CertInfo{NotAfter: leaf.NotAfter, Subject: leaf.Subject.CommonName, Issuer: issuerName(leaf)}
	inter := x509.NewCertPool()
	for _, c := range state.PeerCertificates[1:] {
		inter.AddCert(c)
	}
	if _, err := leaf.Verify(x509.VerifyOptions{DNSName: host, Intermediates: inter}); err != nil {
		info.VerifyErr = err.Error()
	}
	return info, nil
}

func issuerName(c *x509.Certificate) string {
	if len(c.Issuer.Organization) > 0 {
		if c.Issuer.CommonName != "" && c.Issuer.CommonName != c.Issuer.Organization[0] {
			return c.Issuer.Organization[0] + " (" + c.Issuer.CommonName + ")"
		}
		return c.Issuer.Organization[0]
	}
	return c.Issuer.CommonName
}

// Host extracts the host name (and port, if any) from a monitor target:
// a URL, host:port, or a bare host.
func Host(target string) (host, port string) {
	target = strings.TrimSpace(target)
	if strings.Contains(target, "://") {
		u, err := url.Parse(target)
		if err != nil {
			return "", ""
		}
		return strings.ToLower(u.Hostname()), u.Port()
	}
	if h, p, err := net.SplitHostPort(target); err == nil {
		return strings.ToLower(h), p
	}
	return strings.ToLower(strings.Trim(target, "[]")), ""
}

// ErrNoDomain is returned for targets that have no registrable domain
// (IP addresses, localhost, single-label names).
var ErrNoDomain = errors.New("no registrable domain")

// RegistrableDomain returns the domain a registrant pays for, e.g.
// "api.eu.example.co.uk" -> "example.co.uk".
func RegistrableDomain(host string) (string, error) {
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	if host == "" || net.ParseIP(host) != nil || !strings.Contains(host, ".") {
		return "", ErrNoDomain
	}
	d, err := publicsuffix.EffectiveTLDPlusOne(host)
	if err != nil {
		return "", ErrNoDomain
	}
	return d, nil
}

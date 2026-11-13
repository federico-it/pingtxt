package check

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"strings"
	"time"
)

// ClientCertTypes are the monitor types that can present a client
// certificate (mutual TLS).
var ClientCertTypes = []string{TypeHTTP, TypeKeyword, TypeWS, TypeGRPC, TypeMCP}

// ParseClientCert reads a PEM certificate (with its chain, if any) and its
// PEM private key, and returns the leaf certificate.
func ParseClientCert(certPEM, keyPEM string) (tls.Certificate, *x509.Certificate, error) {
	if strings.TrimSpace(certPEM) == "" || strings.TrimSpace(keyPEM) == "" {
		return tls.Certificate{}, nil, errors.New("client certificate: both the certificate and the private key are needed")
	}
	c, err := tls.X509KeyPair([]byte(certPEM), []byte(keyPEM))
	if err != nil {
		return tls.Certificate{}, nil, errors.New("client certificate: " + strings.TrimPrefix(err.Error(), "tls: "))
	}
	leaf, err := x509.ParseCertificate(c.Certificate[0])
	if err != nil {
		return tls.Certificate{}, nil, errors.New("client certificate: " + err.Error())
	}
	return c, leaf, nil
}

// tlsConfig is the TLS client configuration of URL-based checks: the
// ignore_tls setting and, when set, the client certificate.
func (j Job) tlsConfig() (*tls.Config, error) {
	cfg := &tls.Config{InsecureSkipVerify: j.IgnoreTLS} //nolint:gosec // user opt-in
	if j.ClientCert == "" && j.ClientKey == "" {
		return cfg, nil
	}
	c, leaf, err := ParseClientCert(j.ClientCert, j.ClientKey)
	if err != nil {
		return nil, err
	}
	if time.Now().After(leaf.NotAfter) {
		return nil, errors.New("client certificate expired on " + leaf.NotAfter.UTC().Format("2006-01-02"))
	}
	cfg.Certificates = []tls.Certificate{c}
	return cfg, nil
}

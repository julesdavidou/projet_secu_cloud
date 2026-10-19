// Package transportsecurity configures project TLS without changing legacy HTTP profiles.
package transportsecurity

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net/http"
	"os"
)

func Client() (*tls.Config, error) {
	ca := os.Getenv("TLS_CA_FILE")
	if ca == "" {
		return nil, nil
	}
	raw, e := os.ReadFile(ca)
	if e != nil {
		return nil, e
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(raw) {
		return nil, errors.New("invalid project CA")
	}
	cfg := &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: roots}
	cert, key := os.Getenv("TLS_CERT_FILE"), os.Getenv("TLS_KEY_FILE")
	if cert != "" || key != "" {
		pair, e := tls.LoadX509KeyPair(cert, key)
		if e != nil {
			return nil, e
		}
		cfg.Certificates = []tls.Certificate{pair}
	}
	return cfg, nil
}
func ConfigureHTTP() error {
	cfg, e := Client()
	if e != nil {
		return e
	}
	if cfg == nil {
		return nil
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = cfg
	transport.Proxy = nil
	http.DefaultTransport = transport
	return nil
}

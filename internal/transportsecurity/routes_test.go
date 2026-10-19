package transportsecurity

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestChangingDialAddressKeepsTLSIdentity(t *testing.T) {
	pub, key, e := ed25519.GenerateKey(rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "edge-test"}, DNSNames: []string{"edge.test"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, e := x509.CreateCertificate(rand.Reader, template, template, pub, key)
	if e != nil {
		t.Fatal(e)
	}
	cert, _ := x509.ParseCertificate(der)
	roots := x509.NewCertPool()
	roots.AddCert(cert)
	start := func() *httptest.Server {
		s := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.TLS.ServerName != "edge.test" {
				t.Error("SNI lost", r.TLS.ServerName)
			}
			w.WriteHeader(204)
		}))
		s.TLS = &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}}
		s.StartTLS()
		return s
	}
	transport := &http.Transport{DialContext: dialRoute, DisableKeepAlives: true, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: roots}}
	c := http.Client{Transport: transport, Timeout: 2 * time.Second}
	defer transport.CloseIdleConnections()
	defer SetRoutes(nil)
	for i := 0; i < 2; i++ {
		s := start()
		SetRoutes(map[string]Route{"edge.test:443": {Address: strings.TrimPrefix(s.URL, "https://"), Expires: time.Now().Add(time.Minute)}})
		r, e := c.Get("https://edge.test/")
		if e != nil {
			t.Fatal(e)
		}
		r.Body.Close()
		if r.StatusCode != 204 {
			t.Fatal(r.StatusCode)
		}
		s.Close()
	}
	s := start()
	defer s.Close()
	SetRoutes(map[string]Route{"wrong.test:443": {Address: strings.TrimPrefix(s.URL, "https://"), Expires: time.Now().Add(time.Minute)}})
	if r, e := c.Get("https://wrong.test/"); e == nil {
		r.Body.Close()
		t.Fatal("dial override disabled certificate verification")
	}
	SetRoutes(map[string]Route{"edge.test:443": {Address: strings.TrimPrefix(s.URL, "https://"), Expires: time.Now().Add(-time.Second)}})
	if len(Routes()) != 0 {
		t.Fatal("expired lease retained")
	}
}

// client-portal exposes a browser-only loopback portal. Its upstream always
// uses the project CA and this viewer's mTLS certificate; Java still checks
// each user's session. No CA installation in the Windows trust store needed.
package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"time"
)

func required(name string) string {
	v := os.Getenv(name)
	if v == "" {
		log.Fatal("missing ", name)
	}
	return v
}
func main() {
	roots := x509.NewCertPool()
	raw, e := os.ReadFile(required("TLS_CA_FILE"))
	if e != nil || !roots.AppendCertsFromPEM(raw) {
		log.Fatal("invalid project CA")
	}
	cert, e := tls.LoadX509KeyPair(required("TLS_CERT_FILE"), required("TLS_KEY_FILE"))
	if e != nil {
		log.Fatal(e)
	}
	address := required("GATEWAY_ADDRESS")
	host, port, e := net.SplitHostPort(address)
	if e != nil || net.ParseIP(host) == nil {
		log.Fatal("explicit gateway IP:port required")
	}
	target, _ := url.Parse("https://viewer-gateway:" + port)
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.Transport = &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: roots, Certificates: []tls.Certificate{cert}, ServerName: "viewer-gateway"}, DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
		return (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, network, address)
	}, ResponseHeaderTimeout: 180 * time.Second}
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		http.Error(w, "secure controller connection unavailable", 502)
	}
	// Docker publishes this listener ONLY on host loopback. Check Host/Origin too:
	// a LAN webpage cannot use DNS rebinding to read an authenticated portal.
	handler := localOnly(proxy)
	server := &http.Server{Addr: ":8080", Handler: handler, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second}
	log.Fatal(server.ListenAndServe())
}

func localOnly(proxy http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h, _, _ := net.SplitHostPort(r.Host)
		if h != "localhost" && h != "127.0.0.1" {
			http.Error(w, "local portal required", 403)
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" && origin != "http://"+r.Host {
			http.Error(w, "origin refused", 403)
			return
		}
		r.Header.Del("X-Verified-Identity")
		proxy.ServeHTTP(w, r)
	})
}

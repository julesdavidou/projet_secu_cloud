// tls-run binds a legacy service to loopback and exposes its mTLS front in the
// same container. No shared TLS proxy network can bypass its peer checks.
package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

func env(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if len(os.Args) < 2 {
		log.Fatal("child service required")
	}
	child := exec.CommandContext(ctx, os.Args[1], os.Args[2:]...)
	child.Stdout = os.Stdout
	child.Stderr = os.Stderr
	if e := child.Start(); e != nil {
		log.Fatal(e)
	}
	target, e := url.Parse(env("TLS_UPSTREAM", "http://127.0.0.1:8088"))
	if e != nil {
		log.Fatal(e)
	}
	raw, e := os.ReadFile(env("TLS_CA_FILE", "/run/tls/ca.crt"))
	if e != nil {
		log.Fatal(e)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(raw) {
		log.Fatal("invalid project CA")
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	allowed := strings.Split(env("TLS_ALLOWED_PEERS", ""), ",")
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.TLS == nil || len(r.TLS.PeerCertificates) == 0 {
			http.Error(w, "client certificate required", 401)
			return
		}
		actor := r.TLS.PeerCertificates[0].Subject.CommonName
		ok := false
		for _, a := range allowed {
			if a == actor || (strings.HasSuffix(a, "*") && strings.HasPrefix(actor, strings.TrimSuffix(a, "*"))) {
				ok = true
			}
		}
		if !ok {
			http.Error(w, "peer role forbidden", 403)
			return
		}
		r.Header.Del("X-Verified-Identity")
		r.Header.Set("X-Verified-Identity", actor)
		proxy.ServeHTTP(w, r)
	})
	server := &http.Server{Addr: env("TLS_BIND", ":8443"), Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 180 * time.Second, WriteTimeout: 180 * time.Second, TLSConfig: &tls.Config{MinVersion: tls.VersionTLS13, ClientCAs: roots, ClientAuth: tls.RequireAndVerifyClientCert}}
	go func() {
		if e := child.Wait(); e != nil {
			log.Print("child stopped: ", e)
		}
		cancel()
	}()
	go func() {
		<-ctx.Done()
		shutdown, stop := context.WithTimeout(context.Background(), 3*time.Second)
		defer stop()
		_ = server.Shutdown(shutdown)
	}()
	e = server.ListenAndServeTLS(env("TLS_CERT_FILE", "/run/tls/tls.crt"), env("TLS_KEY_FILE", "/run/tls/tls.key"))
	if e != nil && !errors.Is(e, http.ErrServerClosed) {
		log.Print(e)
		cancel()
		os.Exit(1)
	}
}

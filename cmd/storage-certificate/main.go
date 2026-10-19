// storage-certificate issues only server leaves beneath an explicitly constrained
// per-node CA. This CA cannot issue project client credentials or other DNS zones.
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"flag"
	"log"
	"math/big"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

func main() {
	issuer := flag.String("issuer", "/run/issuer", "constrained server CA")
	name := flag.String("name", "", "storage DNS name")
	out := flag.String("out", "", "private output directory")
	flag.Parse()
	if e := issue(*issuer, *name, *out); e != nil {
		log.Fatal(e)
	}
}
func issue(dir, name, out string) error {
	if out == "" || !regexp.MustCompile(`^tenant-[a-f0-9]{24}\.storage-[a-z0-9-]+\.internal$`).MatchString(name) {
		return errors.New("invalid tenant DNS name")
	}
	issuerPEM, e := os.ReadFile(filepath.Join(dir, "tls.crt"))
	if e != nil {
		return e
	}
	block, _ := pem.Decode(issuerPEM)
	if block == nil {
		return errors.New("invalid issuer")
	}
	parent, e := x509.ParseCertificate(block.Bytes)
	if e != nil {
		return e
	}
	if !parent.IsCA || len(parent.PermittedDNSDomains) != 1 || !strings.HasSuffix(name, parent.PermittedDNSDomains[0]) || len(parent.ExtKeyUsage) != 1 || parent.ExtKeyUsage[0] != x509.ExtKeyUsageServerAuth {
		return errors.New("issuer not constrained to this storage domain")
	}
	raw, e := os.ReadFile(filepath.Join(dir, "tls.key"))
	if e != nil {
		return e
	}
	block, _ = pem.Decode(raw)
	if block == nil {
		return errors.New("invalid issuer key")
	}
	parsed, e := x509.ParsePKCS8PrivateKey(block.Bytes)
	if e != nil {
		return e
	}
	key, ok := parsed.(ed25519.PrivateKey)
	if !ok {
		return errors.New("invalid issuer key type")
	}
	public, ok := parent.PublicKey.(ed25519.PublicKey)
	if !ok || !public.Equal(key.Public()) || time.Now().Before(parent.NotBefore) || time.Now().After(parent.NotAfter) {
		return errors.New("issuer key mismatch or validity expired")
	}
	pub, priv, e := ed25519.GenerateKey(rand.Reader)
	if e != nil {
		return e
	}
	serial, e := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if e != nil {
		return e
	}
	template := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: name}, DNSNames: []string{name}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().AddDate(0, 6, 0), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	if template.NotAfter.After(parent.NotAfter) {
		template.NotAfter = parent.NotAfter
	}
	der, e := x509.CreateCertificate(rand.Reader, template, parent, pub, key)
	if e != nil {
		return e
	}
	private, e := x509.MarshalPKCS8PrivateKey(priv)
	if e != nil {
		return e
	}
	if e = os.MkdirAll(out, 0700); e != nil {
		return e
	}
	chain := append(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), issuerPEM...)
	for file, data := range map[string][]byte{"tls.crt": chain, "tls.key": pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: private})} {
		if e = os.WriteFile(filepath.Join(out, file), data, 0600); e != nil {
			return e
		}
	}
	return nil
}

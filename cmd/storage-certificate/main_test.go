package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStorageIssuerCannotCreateProjectClientsOrOtherDomains(t *testing.T) {
	pub, key, e := ed25519.GenerateKey(rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	issuer := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "test storage issuer"}, IsCA: true, BasicConstraintsValid: true, MaxPathLenZero: true, KeyUsage: x509.KeyUsageCertSign, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, PermittedDNSDomainsCritical: true, PermittedDNSDomains: []string{".storage-a1.internal"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour)}
	der, e := x509.CreateCertificate(rand.Reader, issuer, issuer, pub, key)
	if e != nil {
		t.Fatal(e)
	}
	issuer, e = x509.ParseCertificate(der)
	if e != nil {
		t.Fatal(e)
	}
	private, e := x509.MarshalPKCS8PrivateKey(key)
	if e != nil {
		t.Fatal(e)
	}
	dir := t.TempDir()
	for file, data := range map[string][]byte{"tls.crt": pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), "tls.key": pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: private})} {
		if e = os.WriteFile(filepath.Join(dir, file), data, 0600); e != nil {
			t.Fatal(e)
		}
	}
	name := "tenant-0123456789abcdef01234567.storage-a1.internal"
	out := t.TempDir()
	if e = issue(dir, name, out); e != nil {
		t.Fatal(e)
	}
	raw, e := os.ReadFile(filepath.Join(out, "tls.crt"))
	if e != nil {
		t.Fatal(e)
	}
	block, _ := pem.Decode(raw)
	leaf, e := x509.ParseCertificate(block.Bytes)
	if e != nil {
		t.Fatal(e)
	}
	roots := x509.NewCertPool()
	roots.AddCert(issuer)
	if _, e = leaf.Verify(x509.VerifyOptions{Roots: roots, DNSName: name, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}); e != nil {
		t.Fatal("valid isolated server rejected", e)
	}
	if leaf.NotAfter.After(issuer.NotAfter) {
		t.Fatal("leaf outlives issuer")
	}
	if _, e = leaf.Verify(x509.VerifyOptions{Roots: roots, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}); e == nil {
		t.Fatal("storage key accepted as project client")
	}
	for _, invalid := range []string{"web", "tenant-0123456789abcdef01234567.storage-b1.internal", "../escape"} {
		if e = issue(dir, invalid, t.TempDir()); e == nil {
			t.Fatal("issuer escaped storage DNS domain", invalid)
		}
	}
}

package controllers

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// ProvisionPKI creates project certificates once. Transport keys are unrelated to
// SIS/IBS identity keys, which remain in their respective authority/client volumes.
func ProvisionPKI(cfg Config, out string) error {
	root := filepath.Join(out, "pki")
	uid, _ := strconv.Atoi(Env("HOST_UID", "1000"))
	write := func(path string, raw []byte, mode os.FileMode) error {
		if e := os.MkdirAll(filepath.Dir(path), 0755); e != nil {
			return e
		}
		if e := os.WriteFile(path, raw, mode); e != nil {
			return e
		}
		if os.Geteuid() == 0 {
			return os.Chown(path, uid, 10001)
		}
		return nil
	}
	var ca *x509.Certificate
	var key ed25519.PrivateKey
	caFile, keyFile := filepath.Join(root, "authority", "ca.crt"), filepath.Join(root, "authority", "ca.key")
	certRaw, ce := os.ReadFile(caFile)
	keyRaw, ke := os.ReadFile(keyFile)
	if errors.Is(ce, os.ErrNotExist) && errors.Is(ke, os.ErrNotExist) {
		pub, priv, e := ed25519.GenerateKey(rand.Reader)
		if e != nil {
			return e
		}
		serial, e := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
		if e != nil {
			return e
		}
		ca = &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: "Caribou project transport CA"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().AddDate(3, 0, 0), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageCRLSign}
		der, e := x509.CreateCertificate(rand.Reader, ca, ca, pub, priv)
		if e != nil {
			return e
		}
		certRaw = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
		privRaw, e := x509.MarshalPKCS8PrivateKey(priv)
		if e != nil {
			return e
		}
		keyRaw = pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privRaw})
		if e = write(caFile, certRaw, 0644); e != nil {
			return e
		}
		if e = write(keyFile, keyRaw, 0600); e != nil {
			return e
		}
		key = priv
	} else if ce != nil || ke != nil {
		return errors.New("partial or unreadable CA; refusing key regeneration")
	}
	block, _ := pem.Decode(certRaw)
	if block == nil {
		return errors.New("invalid persisted CA")
	}
	var e error
	ca, e = x509.ParseCertificate(block.Bytes)
	if e != nil {
		return e
	}
	if key == nil {
		block, _ = pem.Decode(keyRaw)
		if block == nil {
			return errors.New("invalid CA key")
		}
		parsed, e := x509.ParsePKCS8PrivateKey(block.Bytes)
		if e != nil {
			return e
		}
		var ok bool
		key, ok = parsed.(ed25519.PrivateKey)
		if !ok {
			return errors.New("invalid CA key type")
		}
	}
	caPublic, ok := ca.PublicKey.(ed25519.PublicKey)
	if !ok || !ca.IsCA || time.Now().After(ca.NotAfter) || !bytes.Equal(caPublic, key.Public().(ed25519.PublicKey)) {
		return errors.New("CA key mismatch")
	}
	actors := map[string][]string{"central": {"central"}, "web": {"web"}, "auth": {"auth"}, "broker": {"broker"}, "admin": {"admin"}, "owner-a": {"owner-a", "localhost"}, "owner-b": {"owner-b", "localhost"}}
	for _, z := range cfg.Zones {
		actors["zone-"+z] = []string{"zone-" + z, "zone-" + lower(z)}
		actors["authority-"+z] = []string{"authority-" + z, "is-" + lower(z)}
	}
	for _, n := range cfg.Nodes {
		u, _ := url.Parse(n.Endpoint)
		actors["edge-"+n.ID] = []string{n.ID, "edge-" + n.ID, u.Hostname()}
		actors["broker-"+n.ID] = []string{"broker-" + n.ID}
		if n.BrokerName != "" {
			actors["broker-"+n.ID] = append(actors["broker-"+n.ID], n.BrokerName)
		}
		actors["reconstructor-"+n.ID] = []string{"reconstructor-" + n.ID}
		if n.ReconstructorName != "" {
			actors["reconstructor-"+n.ID] = append(actors["reconstructor-"+n.ID], n.ReconstructorName)
		}
	}
	for actor, names := range actors {
		dir := filepath.Join(root, actor)
		crt, ck := filepath.Join(dir, "tls.crt"), filepath.Join(dir, "tls.key")
		old, ce := os.ReadFile(crt)
		_, ke := os.Stat(ck)
		if ce == nil && ke == nil {
			b, _ := pem.Decode(old)
			if b == nil {
				return errors.New("invalid persisted certificate")
			}
			cert, e := x509.ParseCertificate(b.Bytes)
			if e != nil || cert.CheckSignatureFrom(ca) != nil || time.Now().After(cert.NotAfter) {
				return errors.New("persisted certificate invalid or expired; explicit rotation required")
			}
			if e = write(filepath.Join(dir, "ca.crt"), certRaw, 0644); e != nil {
				return e
			}
			continue
		}
		if !errors.Is(ce, os.ErrNotExist) || !errors.Is(ke, os.ErrNotExist) {
			return errors.New("partial certificate; refusing replacement")
		}
		pub, priv, e := ed25519.GenerateKey(rand.Reader)
		if e != nil {
			return e
		}
		serial, e := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
		if e != nil {
			return e
		}
		cert := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: actor}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().AddDate(1, 0, 0), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth}, DNSNames: names, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}}
		for _, name := range names {
			if ip := net.ParseIP(name); ip != nil {
				cert.IPAddresses = append(cert.IPAddresses, ip)
			}
		}
		der, e := x509.CreateCertificate(rand.Reader, cert, ca, pub, key)
		if e != nil {
			return e
		}
		privDER, e := x509.MarshalPKCS8PrivateKey(priv)
		if e != nil {
			return e
		}
		if e = write(crt, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0644); e != nil {
			return e
		}
		if e = write(ck, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privDER}), 0640); e != nil {
			return e
		}
		if e = write(filepath.Join(dir, "ca.crt"), certRaw, 0644); e != nil {
			return e
		}
	}
	for _, node := range cfg.Nodes {
		dir := filepath.Join(root, "storage-issuer-"+node.ID)
		crtPath, keyPath := filepath.Join(dir, "tls.crt"), filepath.Join(dir, "tls.key")
		persisted, ce := os.ReadFile(crtPath)
		persistedKey, ke := os.ReadFile(keyPath)
		if ce == nil && ke == nil {
			block, _ := pem.Decode(persisted)
			keyBlock, _ := pem.Decode(persistedKey)
			if block == nil || keyBlock == nil {
				return errors.New("invalid storage issuer files")
			}
			issuer, e := x509.ParseCertificate(block.Bytes)
			if e != nil {
				return e
			}
			parsed, e := x509.ParsePKCS8PrivateKey(keyBlock.Bytes)
			issuerKey, ok := parsed.(ed25519.PrivateKey)
			issuerPublic, publicOK := issuer.PublicKey.(ed25519.PublicKey)
			if e != nil || !ok || !publicOK || !bytes.Equal(issuerPublic, issuerKey.Public().(ed25519.PublicKey)) || issuer.CheckSignatureFrom(ca) != nil || !issuer.IsCA || time.Now().After(issuer.NotAfter) || !issuer.MaxPathLenZero || len(issuer.PermittedDNSDomains) != 1 || issuer.PermittedDNSDomains[0] != ".storage-"+lower(node.ID)+".internal" || len(issuer.ExtKeyUsage) != 1 || issuer.ExtKeyUsage[0] != x509.ExtKeyUsageServerAuth {
				return errors.New("storage issuer invalid, mismatched or expired; explicit recovery required")
			}
			continue
		} else if !errors.Is(ce, os.ErrNotExist) || !errors.Is(ke, os.ErrNotExist) {
			return errors.New("partial storage issuer; refusing replacement")
		}
		pub, priv, e := ed25519.GenerateKey(rand.Reader)
		if e != nil {
			return e
		}
		serial, e := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
		if e != nil {
			return e
		}
		cert := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: "storage-issuer-" + node.ID}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().AddDate(1, 0, 0), IsCA: true, BasicConstraintsValid: true, MaxPathLen: 0, MaxPathLenZero: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, PermittedDNSDomainsCritical: true, PermittedDNSDomains: []string{".storage-" + lower(node.ID) + ".internal"}}
		der, e := x509.CreateCertificate(rand.Reader, cert, ca, pub, key)
		if e != nil {
			return e
		}
		privDER, e := x509.MarshalPKCS8PrivateKey(priv)
		if e != nil {
			return e
		}
		if e = write(filepath.Join(dir, "tls.crt"), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0644); e != nil {
			return e
		}
		if e = write(filepath.Join(dir, "tls.key"), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privDER}), 0640); e != nil {
			return e
		}
		if e = write(filepath.Join(dir, "ca.crt"), certRaw, 0644); e != nil {
			return e
		}
	}

	return nil
}
func lower(s string) string {
	out := []byte(s)
	for i, c := range out {
		if c >= 'A' && c <= 'Z' {
			out[i] = c + 32
		}
	}
	return string(out)
}

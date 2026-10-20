package controllers

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestClientProvisionPreservesAuthorityAndExistingKeys(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{Zones: []string{"A", "B"}}
	if e := ProvisionPKIClients(cfg, dir, []string{"viewer-lab-jules", "owner-lab-jules"}); e != nil {
		t.Fatal(e)
	}
	root := filepath.Join(dir, "pki")
	paths := []string{"authority/ca.key", "authority/ca.crt", "viewer-lab-jules/tls.key", "viewer-lab-jules/tls.crt", "owner-lab-jules/tls.key"}
	before := map[string][]byte{}
	for _, p := range paths {
		raw, e := os.ReadFile(filepath.Join(root, p))
		if e != nil {
			t.Fatal(e)
		}
		before[p] = raw
	}
	if e := ProvisionPKIClients(cfg, dir, []string{"viewer-lab-other"}); e != nil {
		t.Fatal(e)
	}
	for _, p := range paths {
		raw, e := os.ReadFile(filepath.Join(root, p))
		if e != nil || !bytes.Equal(raw, before[p]) {
			t.Fatalf("existing key/cert changed: %s", p)
		}
	}
	for _, id := range []string{"admin", "zone-A", "owner-*", "viewer-lab-", "owner-lab-", "viewer-lab-../x"} {
		if e := ProvisionPKIClients(cfg, dir, []string{id}); e == nil {
			t.Fatalf("accepted privileged/invalid identity %s", id)
		}
	}
}

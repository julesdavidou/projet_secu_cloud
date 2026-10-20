package webapp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSignedPublicationRetryAndRestartBindManifestAndOwner(t *testing.T) {
	dir := t.TempDir()
	if e := os.WriteFile(filepath.Join(dir, "operator.token"), []byte(strings.Repeat("x", 32)), 0600); e != nil {
		t.Fatal(e)
	}
	cfg := Config{AuthURL: "http://auth.invalid", SourceNodeID: "A1", DataDir: filepath.Join(dir, "data"), SecretsDir: dir}
	s, e := New(cfg)
	if e != nil {
		t.Fatal(e)
	}
	m := Movie{ID: "v-retry", Owner: "client-alice", PackageHash: strings.Repeat("a", 64)}
	if e = s.acceptSignedMovie(m); e != nil {
		t.Fatal(e)
	}
	s, e = New(cfg)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.acceptSignedMovie(m); e != nil {
		t.Fatal("same signed retry refused after restart", e)
	}
	m.PackageHash = strings.Repeat("b", 64)
	if s.acceptSignedMovie(m) == nil {
		t.Fatal("changed manifest accepted as retry")
	}
	m.PackageHash = strings.Repeat("a", 64)
	m.Owner = "client-bob"
	if s.acceptSignedMovie(m) == nil {
		t.Fatal("another owner replaced publication")
	}
}

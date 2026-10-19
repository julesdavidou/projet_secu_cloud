package storage

import (
	"crypto/sha256"
	"fmt"
	"os"
	"strings"
	"testing"
)

func TestVerifiedWriteRejectsBadUnitsBeforePublish(t *testing.T) {
	s, e := New(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	digest := fmt.Sprintf("%x", sha256.Sum256([]byte("valid")))
	for _, body := range []string{"vali", "valid-extra", "wrong"} {
		if _, e = s.PutVerified("project", "unit", strings.NewReader(body), 5, digest); e == nil {
			t.Fatal("accepted corrupt unit")
		}
		if _, e = s.Path("project", "unit"); e == nil {
			t.Fatal("corrupt final object visible")
		}
	}
	if _, e = s.PutVerified("project", "unit", strings.NewReader("valid"), 5, digest); e != nil {
		t.Fatal(e)
	}
	path, _ := s.Path("project", "unit")
	raw, _ := os.ReadFile(path)
	if string(raw) != "valid" {
		t.Fatal("stored bytes")
	}
}

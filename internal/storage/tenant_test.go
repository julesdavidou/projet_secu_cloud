package storage

import (
	"errors"
	"strings"
	"testing"
)

func TestTenantProcessCannotAccessAnotherNamespace(t *testing.T) {
	s, e := NewTenant(t.TempDir(), "client-a")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Put("client-a", "one", strings.NewReader("own bytes")); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Put("client-b", "one", strings.NewReader("other")); !errors.Is(e, ErrInvalidID) {
		t.Fatal("cross-owner write accepted", e)
	}
	if _, e = s.Path("client-b", "one"); !errors.Is(e, ErrInvalidID) {
		t.Fatal("cross-owner read accepted", e)
	}
	if e = s.Delete("client-b", "one"); !errors.Is(e, ErrInvalidID) {
		t.Fatal("cross-owner delete accepted", e)
	}
}

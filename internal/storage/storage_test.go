package storage

import (
	"errors"
	"os"
	"strings"
	"testing"
)

func TestPutAndRead(t *testing.T) {

	store, err := New(
		t.TempDir(),
	)

	if err != nil {
		t.Fatal(err)
	}

	_, err = store.Put(
		"alice",
		"segment-001.m4s",
		strings.NewReader("hello"),
	)

	if err != nil {
		t.Fatal(err)
	}

	path, err := store.Path(
		"alice",
		"segment-001.m4s",
	)

	if err != nil {
		t.Fatal(err)
	}

	content, err := os.ReadFile(path)

	if err != nil {
		t.Fatal(err)
	}

	if string(content) != "hello" {
		t.Fatalf(
			"unexpected content: %s",
			content,
		)
	}
}

func TestRejectInvalidTenant(t *testing.T) {

	store, err := New(
		t.TempDir(),
	)

	if err != nil {
		t.Fatal(err)
	}

	_, err = store.Put(
		"../alice",
		"segment.m4s",
		strings.NewReader("hello"),
	)

	if !errors.Is(
		err,
		ErrInvalidID,
	) {
		t.Fatal(
			"path traversal should be rejected",
		)
	}
}

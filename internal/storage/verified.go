package storage

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
)

var ErrIntegrity = errors.New("size or SHA256 mismatch")

// PutVerified validates and fsyncs an unpredictable temporary file before rename.
// Success additionally requires fsync of the directory chain. A post-rename
// sync error may leave an orphan file, but must never acknowledge completion.
func (s *Store) PutVerified(tenant, object string, reader io.Reader, expected int64, digest string) (int64, error) {
	if expected <= 0 || expected > 1<<30 {
		return 0, ErrIntegrity
	}
	hash, e := hex.DecodeString(digest)
	if e != nil || len(hash) != sha256.Size {
		return 0, ErrIntegrity
	}
	path, e := s.objectPath(tenant, object)
	if e != nil {
		return 0, e
	}
	dir := filepath.Dir(path)
	if e = os.MkdirAll(dir, 0750); e != nil {
		return 0, e
	}
	f, e := os.CreateTemp(dir, ".unit-*")
	if e != nil {
		return 0, e
	}
	temp := f.Name()
	defer os.Remove(temp)
	h := sha256.New()
	size, e := io.Copy(io.MultiWriter(f, h), io.LimitReader(reader, expected+1))
	if e == nil && (size != expected || hex.EncodeToString(h.Sum(nil)) != digest) {
		e = ErrIntegrity
	}
	if e == nil {
		e = f.Sync()
	}
	closeErr := f.Close()
	if e != nil {
		return size, e
	}
	if closeErr != nil {
		return size, closeErr
	}
	if e = os.Rename(temp, path); e != nil {
		return size, e
	}
	root, e := filepath.Abs(s.root)
	if e != nil {
		return size, e
	}
	current, e := filepath.Abs(dir)
	if e != nil {
		return size, e
	}
	for {
		d, err := os.Open(current)
		if err != nil {
			return size, err
		}
		err = d.Sync()
		closeErr := d.Close()
		if err != nil {
			return size, err
		}
		if closeErr != nil {
			return size, closeErr
		}
		if current == root {
			break
		}
		parent := filepath.Dir(current)
		if parent == current {
			return size, errors.New("storage sync escaped root")
		}
		current = parent
	}
	return size, nil
}

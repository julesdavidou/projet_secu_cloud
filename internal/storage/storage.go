package storage

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
)

var (
	ErrInvalidID = errors.New("invalid identifier")
	ErrNotFound  = errors.New("object not found")
)

// Protection contre le path traversal
var safeID = regexp.MustCompile(
	`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,127}$`,
)

type Store struct {
	root string
}

func New(root string) (*Store, error) {
	err := os.MkdirAll(
		filepath.Join(root, "tenants"),
		0750,
	)

	if err != nil {
		return nil, err
	}

	return &Store{
		root: root,
	}, nil
}

func validID(value string) bool {
	return safeID.MatchString(value)
}

func (s *Store) objectPath(
	tenant string,
	object string,
) (string, error) {

	if !validID(tenant) || !validID(object) {
		return "", ErrInvalidID
	}

	return filepath.Join(
		s.root,
		"tenants",
		tenant,
		"objects",
		object,
	), nil
}

func (s *Store) Put(
	tenant string,
	object string,
	reader io.Reader,
) (int64, error) {

	path, err := s.objectPath(tenant, object)

	if err != nil {
		return 0, err
	}

	err = os.MkdirAll(
		filepath.Dir(path),
		0750,
	)

	if err != nil {
		return 0, err
	}

	tempPath := path + ".part"

	file, err := os.OpenFile(
		tempPath,
		os.O_CREATE|os.O_TRUNC|os.O_WRONLY,
		0640,
	)

	if err != nil {
		return 0, err
	}

	size, copyErr := io.Copy(file, reader)
	closeErr := file.Close()

	if copyErr != nil {
		os.Remove(tempPath)
		return size, copyErr
	}

	if closeErr != nil {
		os.Remove(tempPath)
		return size, closeErr
	}

	if err := os.Rename(tempPath, path); err != nil {
		os.Remove(tempPath)
		return size, err
	}

	return size, nil
}

func (s *Store) Path(
	tenant string,
	object string,
) (string, error) {

	path, err := s.objectPath(tenant, object)

	if err != nil {
		return "", err
	}

	_, err = os.Stat(path)

	if errors.Is(err, os.ErrNotExist) {
		return "", ErrNotFound
	}

	if err != nil {
		return "", err
	}

	return path, nil
}

func (s *Store) Delete(
	tenant string,
	object string,
) error {

	path, err := s.objectPath(tenant, object)

	if err != nil {
		return err
	}

	err = os.Remove(path)

	if errors.Is(err, os.ErrNotExist) {
		return ErrNotFound
	}

	return err
}

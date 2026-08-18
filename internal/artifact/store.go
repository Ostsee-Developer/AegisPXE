package artifact

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Store struct {
	root string
}

func NewStore(root string) (*Store, error) {
	root = filepath.Clean(strings.TrimSpace(root))
	if root == "." || !filepath.IsAbs(root) {
		return nil, errors.New("artifact store root must be absolute")
	}
	if err := os.MkdirAll(root, 0750); err != nil {
		return nil, fmt.Errorf("create artifact store root: %w", err)
	}
	return &Store{root: root}, nil
}

func (s *Store) Put(item Verified) (string, error) {
	if err := VerifyContent(item.Descriptor, item.Content); err != nil {
		return "", err
	}
	path, err := s.path(item.Descriptor.Digest)
	if err != nil {
		return "", err
	}
	if existing, err := os.ReadFile(path); err == nil {
		if SHA256(existing) != item.Descriptor.Digest {
			return "", fmt.Errorf("%w: existing content-addressed artifact does not match digest", ErrIntegrityMismatch)
		}
		return path, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("read existing artifact: %w", err)
	}

	tmp, err := os.CreateTemp(s.root, ".artifact-*")
	if err != nil {
		return "", fmt.Errorf("create artifact temp file: %w", err)
	}
	tmpPath := tmp.Name()
	cleanup := func() {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
	}
	if err := tmp.Chmod(0640); err != nil {
		cleanup()
		return "", fmt.Errorf("set artifact temp permissions: %w", err)
	}
	if _, err := tmp.Write(item.Content); err != nil {
		cleanup()
		return "", fmt.Errorf("write artifact temp file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		cleanup()
		return "", fmt.Errorf("sync artifact temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return "", fmt.Errorf("close artifact temp file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		if existing, readErr := os.ReadFile(path); readErr == nil && SHA256(existing) == item.Descriptor.Digest {
			return path, nil
		}
		return "", fmt.Errorf("publish artifact: %w", err)
	}
	return path, nil
}

func (s *Store) Read(descriptor Descriptor) ([]byte, error) {
	if err := descriptor.Validate(); err != nil {
		return nil, err
	}
	path, err := s.path(descriptor.Digest)
	if err != nil {
		return nil, err
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read artifact: %w", err)
	}
	if err := VerifyContent(descriptor, content); err != nil {
		return nil, err
	}
	return content, nil
}

func (s *Store) path(digest string) (string, error) {
	if !ValidSHA256(digest) {
		return "", errors.New("artifact digest must be canonical sha256")
	}
	hexDigest := strings.TrimPrefix(digest, "sha256:")
	return filepath.Join(s.root, "sha256-"+hexDigest), nil
}

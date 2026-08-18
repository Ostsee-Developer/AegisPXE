package driver

import (
	"errors"
	"strings"
)

const maxSeedBytes = 256 << 10

type SeedBundle struct {
	Filename  string
	MediaType string
	Content   []byte
}

func (s SeedBundle) Validate() error {
	if strings.TrimSpace(s.Filename) == "" || len(s.Filename) > 128 || strings.ContainsAny(s.Filename, "/\\\r\n") {
		return errors.New("seed filename is invalid")
	}
	if strings.TrimSpace(s.MediaType) == "" || len(s.MediaType) > 128 || strings.ContainsAny(s.MediaType, "\r\n") {
		return errors.New("seed media type is invalid")
	}
	if len(s.Content) == 0 || len(s.Content) > maxSeedBytes {
		return errors.New("seed content size is invalid")
	}
	return nil
}

func (s SeedBundle) Clone() SeedBundle {
	copy := s
	copy.Content = append([]byte(nil), s.Content...)
	return copy
}

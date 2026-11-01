// Package memstore is an in-memory asset store used by tests and local demos.
package memstore

import (
	"bytes"
	"context"
	"io"
	"sync"

	"github.com/manovaspace/orbit-cli/pkg/assets/domain"
)

// Store keeps objects by SHA-256.
type Store struct {
	mu      sync.Mutex
	objects map[string][]byte
	puts    int
}

// New builds an empty Store.
func New() *Store {
	return &Store{objects: make(map[string][]byte)}
}

// Put stores an object.
func (s *Store) Put(_ context.Context, sha256 string, r io.Reader, _ int64, _, _ string) error {
	b, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.objects[sha256] = b
	s.puts++
	return nil
}

// Get returns an object.
func (s *Store) Get(_ context.Context, sha256 string) (io.ReadCloser, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.objects[sha256]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return io.NopCloser(bytes.NewReader(b)), nil
}

// Exists reports whether an object is present.
func (s *Store) Exists(_ context.Context, sha256 string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.objects[sha256]
	return ok, nil
}

// Len returns the number of stored objects.
func (s *Store) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.objects)
}

// PutCount returns how many Put calls the store received.
func (s *Store) PutCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.puts
}

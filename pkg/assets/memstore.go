package assets

import (
	"bytes"
	"context"
	"io"
	"sync"
)

type MemStore struct {
	mu      sync.Mutex
	objects map[string][]byte
	puts    int
}

func NewMemStore() *MemStore {
	return &MemStore{objects: make(map[string][]byte)}
}

func (s *MemStore) Put(ctx context.Context, sha256 string, r io.Reader, size int64, contentType, sourcePath string) error {
	_ = ctx
	_ = size
	_ = contentType
	_ = sourcePath
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

func (s *MemStore) Get(ctx context.Context, sha256 string) (io.ReadCloser, error) {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.objects[sha256]
	if !ok {
		return nil, ErrNotFound
	}
	return io.NopCloser(bytes.NewReader(b)), nil
}

func (s *MemStore) Exists(ctx context.Context, sha256 string) (bool, error) {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.objects[sha256]
	return ok, nil
}

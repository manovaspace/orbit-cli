package memstore

import (
	"context"
	"io"
	"strings"
	"testing"
)

func TestPutGetExists(t *testing.T) {
	store := New()
	ctx := context.Background()
	if err := store.Put(ctx, "abc", strings.NewReader("data"), 4, "text/plain", "a.txt"); err != nil {
		t.Fatal(err)
	}
	ok, err := store.Exists(ctx, "abc")
	if err != nil || !ok {
		t.Fatalf("Exists = %v, %v", ok, err)
	}
	reader, err := store.Get(ctx, "abc")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reader.Close() }()
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "data" {
		t.Fatalf("data = %q", data)
	}
	if store.Len() != 1 || store.PutCount() != 1 {
		t.Fatalf("Len=%d PutCount=%d", store.Len(), store.PutCount())
	}
}

func TestMissingObject(t *testing.T) {
	if _, err := New().Get(context.Background(), "absent"); err == nil {
		t.Fatal("expected an error")
	}
}

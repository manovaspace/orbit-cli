package netprobe

import (
	"net"
	"testing"
)

func TestAvailableFalseWhileIPv4Listening(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	p := New()
	if p.Available(port) {
		t.Fatalf("port %d held on 127.0.0.1 reported free", port)
	}
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	if !p.Available(port) {
		t.Fatalf("port %d released on 127.0.0.1 reported busy", port)
	}
}

func TestAvailableFalseWhileIPv6Listening(t *testing.T) {
	listener, err := net.Listen("tcp", "[::1]:0")
	if err != nil {
		t.Skipf("IPv6 loopback unavailable: %v", err)
	}
	defer listener.Close()
	port := listener.Addr().(*net.TCPAddr).Port
	if New().Available(port) {
		t.Fatalf("port %d held on [::1] reported free", port)
	}
}

func TestAvailableRejectsOutOfRange(t *testing.T) {
	p := New()
	if p.Available(0) || p.Available(65536) {
		t.Fatal("out-of-range port reported available")
	}
}

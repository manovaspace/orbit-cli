package netprobe

import (
	"context"
	"net"
	"testing"
	"time"
)

func listen(t *testing.T) (string, int) {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = l.Close() })
	addr := l.Addr().(*net.TCPAddr)
	return addr.IP.String(), addr.Port
}

func closedPort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()
	time.Sleep(20 * time.Millisecond)
	return port
}

func TestReady(t *testing.T) {
	host, port := listen(t)
	probe := New()
	if !probe.Ready(host, port, 500*time.Millisecond) {
		t.Errorf("expected open port %d to be ready", port)
	}
	if probe.Ready(host, closedPort(t), 50*time.Millisecond) {
		t.Error("expected closed port to be unready")
	}
}

func TestWaitSuccess(t *testing.T) {
	host, port := listen(t)
	if !New().Wait(context.Background(), host, port, time.Second) {
		t.Errorf("expected open port %d to become ready", port)
	}
}

func TestWaitTimesOut(t *testing.T) {
	host := "127.0.0.1"
	port := closedPort(t)
	start := time.Now()
	if New().Wait(context.Background(), host, port, 100*time.Millisecond) {
		t.Error("expected closed port to stay unready")
	}
	if elapsed := time.Since(start); elapsed < 80*time.Millisecond {
		t.Errorf("expected Wait to respect the timeout, elapsed %v", elapsed)
	}
}

func TestWaitStopsOnContextCancel(t *testing.T) {
	host := "127.0.0.1"
	port := closedPort(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if New().Wait(ctx, host, port, 2*time.Second) {
		t.Error("expected a cancelled context to return false")
	}
}

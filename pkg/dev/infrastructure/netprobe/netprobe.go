// Package netprobe implements the dev readiness port over TCP dials.
package netprobe

import (
	"context"
	"net"
	"strconv"
	"time"
)

// Probe checks TCP services.
type Probe struct {
	// PollInterval defaults to 150ms.
	PollInterval time.Duration
}

// New builds a Probe.
func New() *Probe { return &Probe{PollInterval: 150 * time.Millisecond} }

// Ready reports whether host:port accepts a connection within timeout.
func (p *Probe) Ready(host string, port int, timeout time.Duration) bool {
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	dialer := &net.Dialer{Timeout: timeout}
	conn, err := dialer.Dial("tcp", addr)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// Wait polls host:port until it accepts a connection or timeout/cancellation
// occurs.
func (p *Probe) Wait(ctx context.Context, host string, port int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	dialer := &net.Dialer{Timeout: 500 * time.Millisecond}
	interval := p.PollInterval
	if interval <= 0 {
		interval = 150 * time.Millisecond
	}

	for {
		if ctx.Err() != nil {
			return false
		}

		conn, err := dialer.DialContext(ctx, "tcp", addr)
		if err == nil {
			_ = conn.Close()
			return true
		}

		if time.Now().After(deadline) {
			return false
		}

		select {
		case <-ctx.Done():
			return false
		case <-time.After(interval):
		}
	}
}

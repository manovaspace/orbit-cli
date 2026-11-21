// Package netprobe implements the TCP loopback port probe with net.
package netprobe

import (
	"errors"
	"net"
	"strconv"
	"syscall"
)

// Probe binds candidate ports on loopback and closes them immediately. No
// reservation is retained.
type Probe struct{}

// New builds a Probe.
func New() *Probe { return &Probe{} }

// Available reports whether port can be bound on 127.0.0.1 and on [::1]. A
// port is free only if both binds succeed; a host without IPv6 loopback is
// ignored for the [::1] half, so only "address in use" on [::1] counts as busy.
func (p *Probe) Available(port int) bool {
	if port <= 0 || port > 65535 {
		return false
	}
	if err := tryBind("127.0.0.1", port); err != nil {
		return false
	}
	if err := tryBind("::1", port); err != nil && errors.Is(err, syscall.EADDRINUSE) {
		return false
	}
	return true
}

func tryBind(host string, port int) error {
	listener, err := net.Listen("tcp", net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil {
		return err
	}
	return listener.Close()
}

// Scan reports whether each port in [start, end] is in use.
func (p *Probe) Scan(start, end int) map[int]bool {
	results := make(map[int]bool)
	if start > end || start <= 0 || end > 65535 {
		return results
	}
	for port := start; port <= end; port++ {
		results[port] = !p.Available(port)
	}
	return results
}

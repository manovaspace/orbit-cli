// Package netprobe implements the TCP loopback port probe with net.
package netprobe

import (
	"net"
	"strconv"
)

// Probe binds candidate ports on IPv4 loopback and closes them immediately. No
// reservation is retained.
type Probe struct{}

// New builds a Probe.
func New() *Probe { return &Probe{} }

// Available reports whether port can be bound on 127.0.0.1 right now.
func (p *Probe) Available(port int) bool {
	if port <= 0 || port > 65535 {
		return false
	}
	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return false
	}
	_ = listener.Close()
	return true
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

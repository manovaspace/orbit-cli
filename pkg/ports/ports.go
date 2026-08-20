package ports

import (
	"fmt"
	"net"
	"strconv"
)

// BasePort calculates the base port number for a given project ID using the 50-port allocation model.
// Formula: BasePort = 10000 + (projectID * 50)
func BasePort(projectID int) int {
	return BasePortOffset + (projectID * BlockSize)
}

// CalculateBasePort is an alias for BasePort.
func CalculateBasePort(projectID int) int {
	return BasePort(projectID)
}

// DeterministicPort calculates and returns the port for a deterministic slot (0-9) within a project.
// Returns an error if the project ID is negative or if the slot is outside the 0-9 range.
func DeterministicPort(projectID int, slot int) (int, error) {
	if projectID < 0 {
		return 0, fmt.Errorf("invalid project ID %d: must be non-negative", projectID)
	}
	if slot < DeterministicSlotStart || slot > DeterministicSlotEnd {
		return 0, fmt.Errorf("invalid deterministic slot %d: must be between %d and %d", slot, DeterministicSlotStart, DeterministicSlotEnd)
	}
	return BasePort(projectID) + slot, nil
}

// AllocateDynamic searches dynamic slots 10..49 for the lowest available port.
// A port is considered unavailable if it is present in inUsePorts or if a socket probe
// determines the port is already bound on 127.0.0.1.
// Returns an error if all 40 dynamic slots are exhausted or in use.
func AllocateDynamic(projectID int, inUsePorts []int) (int, error) {
	if projectID < 0 {
		return 0, fmt.Errorf("invalid project ID %d: must be non-negative", projectID)
	}

	inUseSet := make(map[int]struct{}, len(inUsePorts))
	for _, p := range inUsePorts {
		inUseSet[p] = struct{}{}
	}

	base := BasePort(projectID)
	for slot := DynamicSlotStart; slot <= DynamicSlotEnd; slot++ {
		port := base + slot
		if _, exists := inUseSet[port]; exists {
			continue
		}
		if !IsPortAvailable(port) {
			continue
		}
		return port, nil
	}

	return 0, fmt.Errorf("all %d dynamic port slots (%d-%d) for project %d (ports %d-%d) are exhausted or in use",
		NumDynamicSlots, DynamicSlotStart, DynamicSlotEnd, projectID, base+DynamicSlotStart, base+DynamicSlotEnd)
}

// AllocateDynamicPort is an alias for AllocateDynamic.
func AllocateDynamicPort(projectID int, inUsePorts []int) (int, error) {
	return AllocateDynamic(projectID, inUsePorts)
}

// IsPortAvailable checks if a TCP port is free to bind on IPv4 loopback (127.0.0.1).
// It attempts to open a TCP listener and immediately closes it if successful.
func IsPortAvailable(port int) bool {
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

// IsPortInUse checks if a TCP port is currently bound or unavailable on 127.0.0.1.
func IsPortInUse(port int) bool {
	return !IsPortAvailable(port)
}

// ScanRange scans TCP ports from startPort to endPort (inclusive) on 127.0.0.1.
// Returns a map where key is the port number and value indicates whether the port is in use (true = in use, false = free).
func ScanRange(startPort, endPort int) map[int]bool {
	results := make(map[int]bool)
	if startPort > endPort || startPort <= 0 || endPort > 65535 {
		return results
	}

	for port := startPort; port <= endPort; port++ {
		results[port] = !IsPortAvailable(port)
	}

	return results
}

// GetProjectRange returns the start and end port numbers for a given project ID block (50 ports).
func GetProjectRange(projectID int) (startPort int, endPort int) {
	start := BasePort(projectID)
	return start, start + BlockSize - 1
}

// ScanProjectPorts scans all 50 ports assigned to a given project ID block.
func ScanProjectPorts(projectID int) (map[int]bool, error) {
	if projectID < 0 {
		return nil, fmt.Errorf("invalid project ID %d: must be non-negative", projectID)
	}
	start, end := GetProjectRange(projectID)
	return ScanRange(start, end), nil
}

// ResolveProjectID looks up the project ID by name from the given mapping or DefaultProjectMapping.
func ResolveProjectID(projectName string, customMapping ...ProjectMapping) (int, bool) {
	mapping := DefaultProjectMapping
	if len(customMapping) > 0 && customMapping[0] != nil {
		mapping = customMapping[0]
	}
	id, ok := mapping[projectName]
	return id, ok
}

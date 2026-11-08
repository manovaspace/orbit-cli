// Package domain holds the pure 50-port allocation rules: block arithmetic,
// slot ranges and project mappings. It performs no I/O.
package domain

import "fmt"

// PortSlotType defines whether a port allocation slot is deterministic (fixed) or dynamic.
type PortSlotType string

const (
	// Deterministic slot (0-9): Fixed ports for primary services (frontend, API, database, etc.).
	Deterministic PortSlotType = "Deterministic"
	// Dynamic slot (10-49): On-demand ports for preview servers, ephemeral workers, Storybook, etc.
	Dynamic PortSlotType = "Dynamic"
)

const (
	// BasePortOffset is the starting port number for project ID 0.
	BasePortOffset = 10000

	// BlockSize is the number of ports assigned to each project block.
	BlockSize = 50

	// DeterministicSlotStart is the first deterministic slot index.
	DeterministicSlotStart = 0

	// DeterministicSlotEnd is the last deterministic slot index.
	DeterministicSlotEnd = 9

	// DynamicSlotStart is the first dynamic slot index.
	DynamicSlotStart = 10

	// DynamicSlotEnd is the last dynamic slot index.
	DynamicSlotEnd = 49

	// NumDeterministicSlots is the count of deterministic slots per project.
	NumDeterministicSlots = 10

	// NumDynamicSlots is the count of dynamic slots per project.
	NumDynamicSlots = 40
)

// PortAllocation represents an assigned or evaluated port slot within a project's 50-port block.
type PortAllocation struct {
	Port      int          `json:"port" yaml:"port"`
	ProjectID int          `json:"project_id" yaml:"project_id"`
	Slot      int          `json:"slot" yaml:"slot"`
	Service   string       `json:"service" yaml:"service"`
	Type      PortSlotType `json:"type" yaml:"type"`
	InUse     bool         `json:"in_use" yaml:"in_use"`
}

// ProjectMapping maps project names/scopes to their assigned 50-port project block IDs.
type ProjectMapping map[string]int

// DefaultProjectMapping contains the canonical project-to-ID assignments of the
// ADR-058 50-port model. Block 2 belonged to the Fryto client; Fryto is now a
// tenant of the Falak product, which owns the block.
var DefaultProjectMapping = ProjectMapping{
	"orbit-platform": 0,
	"manova":         1,
	"falak":          2,
	"jouybari":       3,
	"jtash":          4,
	"kohan_kherad":   5,
}

// DefaultSlotNames names the deterministic slots every project block shares.
var DefaultSlotNames = map[int]string{
	0: "Base / Database",
	1: "Core API / Backend",
	2: "Frontend / Web",
	3: "Admin / Dashboard",
	4: "Metrics / Telemetry",
	5: "Health / Status",
	6: "Queue / Broker",
	7: "Ingress / Gateway",
	8: "Auth / Identity",
	9: "Aux / Worker",
}

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

// AllocateDynamic searches dynamic slots 10..49 for the lowest port that is
// neither listed in inUsePorts nor reported unavailable by available.
// Returns an error if all 40 dynamic slots are exhausted or in use.
func AllocateDynamic(projectID int, inUsePorts []int, available func(int) bool) (int, error) {
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
		if available != nil && !available(port) {
			continue
		}
		return port, nil
	}

	return 0, fmt.Errorf("all %d dynamic port slots (%d-%d) for project %d (ports %d-%d) are exhausted or in use",
		NumDynamicSlots, DynamicSlotStart, DynamicSlotEnd, projectID, base+DynamicSlotStart, base+DynamicSlotEnd)
}

// GetProjectRange returns the start and end port numbers for a given project ID block (50 ports).
func GetProjectRange(projectID int) (startPort int, endPort int) {
	start := BasePort(projectID)
	return start, start + BlockSize - 1
}

// ResolveProjectID looks up the project ID by name from the given mapping or DefaultProjectMapping.
func ResolveProjectID(projectName string, customMapping ...ProjectMapping) (int, bool) {
	mapping := DefaultProjectMapping
	if len(customMapping) > 0 && customMapping[0] != nil {
		mapping = customMapping[0]
	}
	id, ok := mapping[projectName]
	if !ok && projectName == "orbit-services" {
		return 1, true
	}
	return id, ok
}

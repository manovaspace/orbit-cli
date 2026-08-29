package ports

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

// DefaultProjectMapping contains the canonical project-to-ID assignments defined in ADR-006 / 50-port model.
var DefaultProjectMapping = ProjectMapping{
	"orbit-platform": 0,
	"orbit-services": 1,
	"fryto":          2,
	"jtash":          4,
}

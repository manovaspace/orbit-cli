package domain

import "sort"

// Registry is the parsed handbook/agents/workspace/ports.yaml (ADR-058). It is
// the only place a host port number is decided. The domain holds the rules
// over it; reading the file is the infrastructure layer's job.
type Registry struct {
	Version    int         `yaml:"version"`
	Base       int         `yaml:"base"`
	BlockSize  int         `yaml:"block_size"`
	FixedSlots int         `yaml:"fixed_slots"`
	Projects   []Project   `yaml:"projects"`
	Services   []Service   `yaml:"services"`
	Reserved   []Reserved  `yaml:"reserved"`
	Exceptions []Exception `yaml:"exceptions"`
	Retired    []Retired   `yaml:"retired"`
}

// Project is one named 50-port block owner.
type Project struct {
	ID   int    `yaml:"id"`
	Name string `yaml:"name"`
}

// Service is one registered host port.
type Service struct {
	Port      int      `yaml:"port"`
	Project   string   `yaml:"project"`
	Name      string   `yaml:"name"`
	Proto     string   `yaml:"proto"`
	Runs      string   `yaml:"runs"`
	Repos     []string `yaml:"repos"`
	Bind      string   `yaml:"bind"`
	Localhost string   `yaml:"localhost"`
	Note      string   `yaml:"note"`
}

// Reserved is a fixed slot held for a future service; nothing binds it yet.
type Reserved struct {
	Port    int    `yaml:"port"`
	Project string `yaml:"project"`
	Name    string `yaml:"name"`
}

// Exception is a port outside the blocks that is accepted on purpose.
type Exception struct {
	Port   int      `yaml:"port"`
	Name   string   `yaml:"name"`
	Runs   string   `yaml:"runs"`
	Repos  []string `yaml:"repos"`
	Reason string   `yaml:"reason"`
	Until  string   `yaml:"until"`
}

// Retired is a number that must never be bound again.
type Retired struct {
	Port    int    `yaml:"port"`
	Was     string `yaml:"was"`
	Retired string `yaml:"retired"`
}

// OwnerKind classifies what the registry says about one port number.
type OwnerKind string

const (
	OwnerNone      OwnerKind = "none"
	OwnerService   OwnerKind = "service"
	OwnerReserved  OwnerKind = "reserved"
	OwnerException OwnerKind = "exception"
	OwnerRetired   OwnerKind = "retired"
)

// Owner is the registry's answer for one port number. Name is the registry
// name when known; Repos lists the repositories allowed to bind the port.
type Owner struct {
	Kind    OwnerKind
	Port    int
	Name    string
	Project string
	Runs    string
	Repos   []string
	Service *Service
}

// Lookup reports what owns port. A port appears in at most one class; the
// order service, reserved, exception, retired only matters for malformed files.
func (r *Registry) Lookup(port int) Owner {
	for i := range r.Services {
		if r.Services[i].Port == port {
			s := &r.Services[i]
			return Owner{Kind: OwnerService, Port: port, Name: s.Name, Project: s.Project, Runs: s.Runs, Repos: s.Repos, Service: s}
		}
	}
	for _, rs := range r.Reserved {
		if rs.Port == port {
			return Owner{Kind: OwnerReserved, Port: port, Name: rs.Name, Project: rs.Project}
		}
	}
	for _, ex := range r.Exceptions {
		if ex.Port == port {
			return Owner{Kind: OwnerException, Port: port, Name: ex.Name, Runs: ex.Runs, Repos: ex.Repos}
		}
	}
	for _, rt := range r.Retired {
		if rt.Port == port {
			return Owner{Kind: OwnerRetired, Port: port, Name: rt.Was}
		}
	}
	return Owner{Kind: OwnerNone, Port: port}
}

// Taken reports every port number the registry claims in any class. Allocation
// must never return one of these.
func (r *Registry) Taken() map[int]bool {
	taken := make(map[int]bool, len(r.Services)+len(r.Reserved)+len(r.Exceptions)+len(r.Retired))
	for _, s := range r.Services {
		taken[s.Port] = true
	}
	for _, rs := range r.Reserved {
		taken[rs.Port] = true
	}
	for _, ex := range r.Exceptions {
		taken[ex.Port] = true
	}
	for _, rt := range r.Retired {
		taken[rt.Port] = true
	}
	return taken
}

// ProjectMapping maps project names to block IDs as declared in the registry.
func (r *Registry) ProjectMapping() ProjectMapping {
	mapping := make(ProjectMapping, len(r.Projects))
	for _, p := range r.Projects {
		mapping[p.Name] = p.ID
	}
	return mapping
}

// ServicesByPort returns the registered services sorted by port.
func (r *Registry) ServicesByPort() []Service {
	out := make([]Service, len(r.Services))
	copy(out, r.Services)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Port < out[j].Port })
	return out
}

// RetiredPort reports the retirement record for port, if any.
func (r *Registry) RetiredPort(port int) (Retired, bool) {
	for _, rt := range r.Retired {
		if rt.Port == port {
			return rt, true
		}
	}
	return Retired{}, false
}

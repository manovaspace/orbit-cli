package domain

import (
	"fmt"
	"slices"
)

// Kind names one class of `o port check` finding.
type Kind string

const (
	KindUnregistered Kind = "UNREGISTERED"
	KindRetired      Kind = "RETIRED"
	KindWrongRepo    Kind = "WRONG_REPO"
	KindReserved     Kind = "RESERVED"
	KindOpenBind     Kind = "OPEN_BIND"
)

// AllKinds lists every finding kind in report order.
var AllKinds = []Kind{KindUnregistered, KindRetired, KindWrongRepo, KindReserved, KindOpenBind}

// Severity is error (fails the check) or warning (fails only with --strict).
type Severity string

const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
)

// Finding is one problem found by `o port check`.
type Finding struct {
	File     string   `json:"file"`
	Line     int      `json:"line"`
	Port     int      `json:"port"`
	Kind     Kind     `json:"kind"`
	Severity Severity `json:"severity"`
	Repo     string   `json:"repo"`
	Name     string   `json:"name,omitempty"`
	Message  string   `json:"message"`
}

// Classify applies the registry to one occurrence found in repo. A retired
// number reports only RETIRED; an unknown number reports UNREGISTERED; the
// bind-only rules (RESERVED, WRONG_REPO) need the occurrence to be a BIND. An
// open compose publish adds OPEN_BIND on top of any other finding.
func Classify(reg *Registry, occ Occurrence, repo string) []Finding {
	var out []Finding
	owner := reg.Lookup(occ.Port)
	add := func(kind Kind, sev Severity, msg string) {
		out = append(out, Finding{
			File: occ.File, Line: occ.Line, Port: occ.Port, Kind: kind, Severity: sev,
			Repo: repo, Name: owner.Name, Message: msg,
		})
	}

	switch owner.Kind {
	case OwnerRetired:
		add(KindRetired, SeverityError, fmt.Sprintf("port %d was retired (was %s); never bind or reference it again", occ.Port, owner.Name))
	case OwnerNone:
		add(KindUnregistered, SeverityError, fmt.Sprintf("port %d is not in ports.yaml; register it in a project block or an exception", occ.Port))
	case OwnerReserved:
		if occ.Role == RoleBind {
			add(KindReserved, SeverityError, fmt.Sprintf("port %d is a reserved slot (%s); claim it in ports.yaml before binding", occ.Port, owner.Name))
		}
	case OwnerService, OwnerException:
		if occ.Role == RoleBind && repo != "" && !slices.Contains(owner.Repos, repo) {
			add(KindWrongRepo, SeverityError, fmt.Sprintf("port %d is registered for %s; repo %s is not in its repos", occ.Port, owner.Name, repo))
		}
	}

	if occ.Open {
		add(KindOpenBind, SeverityWarning, fmt.Sprintf("port %d is published on all interfaces; dev binds use 127.0.0.1", occ.Port))
	}
	return out
}

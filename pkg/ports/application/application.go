// Package application implements the port inspection and allocation use cases
// over a port-availability probe, keeping the CLI command thin.
package application

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"

	"github.com/manovaspace/orbit-cli/pkg/clierr"
	"github.com/manovaspace/orbit-cli/pkg/ports/domain"
)

// Prober reports whether local TCP ports can be bound. Scan reports whether
// each port in the inclusive range is in use.
type Prober interface {
	Available(port int) bool
	Scan(start, end int) map[int]bool
}

// UI carries the decoration cmd/orbit already renders, so this package writes
// the same lines without importing a styling library.
type UI struct {
	Title  func(string) string
	Header func(string) string
	Bold   func(string) string
	OK     string

	Success func(string) string
	Warning func(string) string
	Subtle  func(string) string
	Code    func(string) string
	Info    func(string) string
}

// Service is the port use-case layer.
type Service struct {
	Ports Prober
	Out   io.Writer
	UI    UI
}

// Slot is one port inside a project block.
type Slot struct {
	Index     int    `json:"index"`
	Port      int    `json:"port"`
	Name      string `json:"name"`
	Scanned   bool   `json:"scanned"`
	Available bool   `json:"available"`
}

// Project describes one 50-port block.
type Project struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	Base        int    `json:"base"`
	Start       int    `json:"start"`
	End         int    `json:"end"`
	Scanned     bool   `json:"scanned"`
	Active      int    `json:"active"`
	DynamicFree int    `json:"dynamicFree"`
	DynamicUsed int    `json:"dynamicInUse"`
	Slots       []Slot `json:"slots"`
}

// ListOptions selects scanning and paging for `port list`.
type ListOptions struct {
	Scan  bool
	Page  int
	Limit int
}

// ListResult is what `port list` produced.
type ListResult struct {
	Projects      []Project `json:"projects"`
	Scanned       bool      `json:"scanned"`
	Page          int       `json:"page"`
	Limit         int       `json:"limit"`
	TotalProjects int       `json:"totalProjects"`
	TotalPages    int       `json:"totalPages"`
	Start         int       `json:"start"`
	End           int       `json:"end"`
	Complete      bool      `json:"complete"`
}

// List renders the project blocks sorted by ID, one page at a time.
func (s *Service) List(_ context.Context, opts ListOptions) (ListResult, error) {
	out := s.out()
	title := s.UI.Title
	if title == nil {
		title = func(v string) string { return v }
	}
	fmt.Fprintln(out, title("Orbit Port Manager — 50-Port Block Allocations"))

	type projEntry struct {
		name string
		id   int
	}
	var entries []projEntry
	for name, id := range domain.DefaultProjectMapping {
		entries = append(entries, projEntry{name: name, id: id})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].id < entries[j].id })

	totalProjects := len(entries)
	displayEntries := entries
	page := opts.Page
	limit := opts.Limit
	var startIdx, endIdx, totalPages int
	if limit > 0 {
		if page < 1 {
			page = 1
		}
		totalPages = (totalProjects + limit - 1) / limit
		startIdx = (page - 1) * limit
		if startIdx < totalProjects {
			endIdx = startIdx + limit
			if endIdx > totalProjects {
				endIdx = totalProjects
			}
			displayEntries = entries[startIdx:endIdx]
		} else {
			displayEntries = nil
			startIdx = totalProjects
			endIdx = totalProjects
		}
	}

	result := ListResult{
		Projects:      make([]Project, 0, len(displayEntries)),
		Scanned:       opts.Scan,
		Page:          page,
		Limit:         limit,
		TotalProjects: totalProjects,
		TotalPages:    totalPages,
		Start:         startIdx,
		End:           endIdx,
		Complete:      limit == 0 || len(displayEntries) == totalProjects,
	}

	for _, p := range displayEntries {
		base := domain.BasePort(p.id)
		start, end := domain.GetProjectRange(p.id)

		var scanResults map[int]bool
		if opts.Scan {
			scanResults = s.Ports.Scan(start, end)
		}
		activeCount := 0
		for _, inUse := range scanResults {
			if inUse {
				activeCount++
			}
		}

		project := Project{
			ID:          p.id,
			Name:        p.name,
			Base:        base,
			Start:       start,
			End:         end,
			Scanned:     opts.Scan,
			Active:      activeCount,
			DynamicFree: 0,
			DynamicUsed: 0,
			Slots:       make([]Slot, 0, domain.NumDeterministicSlots),
		}

		header := fmt.Sprintf("Project %d: %s  [%d - %d]", p.id, p.name, start, end)
		fmt.Fprintf(out, "\n%s\n", s.header("── "+header+" ──────────────────────────────────"))
		fmt.Fprintf(out, "  Base Port: %s  |  Total Range: %s  |  Active: %s\n\n",
			s.code(strconv.Itoa(base)),
			s.subtle(fmt.Sprintf("%d-%d (50 ports)", start, end)),
			s.bold(func() string {
				if !opts.Scan {
					return "UNVERIFIED"
				}
				return fmt.Sprintf("%d unavailable at probe time", activeCount)
			}()),
		)

		fmt.Fprintln(out, s.bold("  Deterministic Slots (0-9):"))
		for slot := 0; slot < domain.NumDeterministicSlots; slot++ {
			port := base + slot
			inUse := scanResults[port]

			var statusBadge string
			switch {
			case !opts.Scan:
				statusBadge = s.subtle("[UNVERIFIED]")
			case inUse:
				statusBadge = s.warning("[UNAVAILABLE AT PROBE]")
			default:
				statusBadge = s.success("[BINDABLE AT PROBE]")
			}

			slotName := domain.DefaultSlotNames[slot]
			fmt.Fprintf(out, "    Slot +%d: %s  %s  %s\n",
				slot,
				s.code(strconv.Itoa(port)),
				statusBadge,
				s.subtle(slotName),
			)
			project.Slots = append(project.Slots, Slot{
				Index:     slot,
				Port:      port,
				Name:      slotName,
				Scanned:   opts.Scan,
				Available: !inUse,
			})
		}

		dynamicInUse := 0
		for slot := domain.DynamicSlotStart; slot <= domain.DynamicSlotEnd; slot++ {
			if scanResults[base+slot] {
				dynamicInUse++
			}
		}
		dynamicFree := domain.NumDynamicSlots - dynamicInUse
		project.DynamicUsed = dynamicInUse
		project.DynamicFree = dynamicFree

		if opts.Scan {
			fmt.Fprintf(out, "\n  %s %s (%d bindable, %d unavailable at probe time)\n",
				s.bold("Dynamic Slots (10-49):"),
				s.subtle(fmt.Sprintf("Ports %d-%d", base+domain.DynamicSlotStart, base+domain.DynamicSlotEnd)),
				dynamicFree,
				dynamicInUse,
			)
		} else {
			fmt.Fprintf(out, "\n  Dynamic Slots (10-49): Ports %d-%d [UNVERIFIED]\n", base+domain.DynamicSlotStart, base+domain.DynamicSlotEnd)
		}

		result.Projects = append(result.Projects, project)
	}

	if limit > 0 {
		var info string
		switch {
		case totalProjects == 0:
			info = "Showing 0 of 0 projects"
		case len(displayEntries) == 0:
			info = fmt.Sprintf("Page %d of %d (no projects on this page, total: %d)", page, totalPages, totalProjects)
		default:
			info = fmt.Sprintf("Showing %d-%d of %d projects (Page %d/%d)", startIdx+1, endIdx, totalProjects, page, totalPages)
		}
		fmt.Fprintf(out, "\n  %s\n", s.subtle(info))
	}

	fmt.Fprintln(out)
	return result, nil
}

// AllocateInput is one `port allocate` invocation.
type AllocateInput struct {
	Project string
	Service string
}

// AllocateResult is the suggested port.
type AllocateResult struct {
	Project     string `json:"project"`
	ProjectID   int    `json:"projectId"`
	Service     string `json:"service"`
	Port        int    `json:"port"`
	Slot        int    `json:"slot"`
	EnvVariable string `json:"export"`
	Reserved    bool   `json:"reserved"`
}

// Allocate suggests a bindable dynamic port. It is not a reservation.
func (s *Service) Allocate(_ context.Context, in AllocateInput) (AllocateResult, error) {
	projectID, ok := domain.ResolveProjectID(in.Project)
	if !ok {
		if id, err := strconv.Atoi(in.Project); err == nil && id >= 0 {
			projectID = id
			ok = true
		}
	}
	if !ok {
		var validProjects []string
		for k := range domain.DefaultProjectMapping {
			validProjects = append(validProjects, k)
		}
		sort.Strings(validProjects)
		return AllocateResult{}, clierr.Usage(fmt.Sprintf("unknown project %q. Valid projects: %s (or enter project ID number)", in.Project, strings.Join(validProjects, ", ")))
	}

	start, end := domain.GetProjectRange(projectID)
	scanResults := s.Ports.Scan(start, end)

	var inUse []int
	for p, used := range scanResults {
		if used {
			inUse = append(inUse, p)
		}
	}

	allocatedPort, err := domain.AllocateDynamic(projectID, inUse, s.Ports.Available)
	if err != nil {
		return AllocateResult{}, clierr.Wrap(err, clierr.Failed("port allocation failed"))
	}

	slot := allocatedPort - domain.BasePort(projectID)
	envVariable := fmt.Sprintf("export %s_PORT=%d", strings.ToUpper(strings.ReplaceAll(in.Service, "-", "_")), allocatedPort)

	out := s.out()
	title := s.UI.Title
	if title == nil {
		title = func(v string) string { return v }
	}
	fmt.Fprintln(out, title("Port Assignment Suggestion (not reserved)"))
	fmt.Fprintf(out, "  %s  Suggested port %s for service %s\n",
		s.UI.OK,
		s.success(strconv.Itoa(allocatedPort)),
		s.bold(in.Service),
	)
	fmt.Fprintf(out, "  Project:     %s (ID: %d)\n", s.bold(in.Project), projectID)
	fmt.Fprintf(out, "  Slot:        +%d (Dynamic)\n", slot)
	fmt.Fprintf(out, "  Export:      %s\n", s.code(envVariable))

	return AllocateResult{
		Project:     in.Project,
		ProjectID:   projectID,
		Service:     in.Service,
		Port:        allocatedPort,
		Slot:        slot,
		EnvVariable: envVariable,
		Reserved:    false,
	}, nil
}

func (s *Service) out() io.Writer {
	if s.Out == nil {
		return io.Discard
	}
	return s.Out
}

func (s *Service) header(text string) string {
	if s.UI.Header == nil {
		return text
	}
	return s.UI.Header(text)
}

func (s *Service) bold(text string) string {
	if s.UI.Bold == nil {
		return text
	}
	return s.UI.Bold(text)
}

func (s *Service) code(text string) string {
	if s.UI.Code == nil {
		return text
	}
	return s.UI.Code(text)
}

func (s *Service) subtle(text string) string {
	if s.UI.Subtle == nil {
		return text
	}
	return s.UI.Subtle(text)
}

func (s *Service) success(text string) string {
	if s.UI.Success == nil {
		return text
	}
	return s.UI.Success(text)
}

func (s *Service) warning(text string) string {
	if s.UI.Warning == nil {
		return text
	}
	return s.UI.Warning(text)
}

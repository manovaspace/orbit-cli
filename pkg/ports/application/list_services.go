package application

import "context"

// ServiceRow is one registered service in `port list` with a registry.
// Listening is a probe of 127.0.0.1 and [::1] at call time.
type ServiceRow struct {
	Port      int      `json:"port"`
	Project   string   `json:"project"`
	Service   string   `json:"service"`
	Proto     string   `json:"proto"`
	Runs      string   `json:"runs"`
	Bind      string   `json:"bind,omitempty"`
	Repos     []string `json:"repos"`
	Note      string   `json:"note,omitempty"`
	Listening bool     `json:"listening"`
}

// ServiceListResult is what `port list` produced when a registry was found.
type ServiceListResult struct {
	Services      []ServiceRow `json:"services"`
	Probed        bool         `json:"probed"`
	Page          int          `json:"page"`
	Limit         int          `json:"limit"`
	TotalServices int          `json:"totalServices"`
	TotalPages    int          `json:"totalPages"`
	Start         int          `json:"start"`
	End           int          `json:"end"`
}

// ListServices lists the registered services by port, one page at a time. The
// listening column is always probed: one bind pair per service, not a block
// scan. opts.Scan has no effect here.
func (s *Service) ListServices(_ context.Context, opts ListOptions) ServiceListResult {
	if s.Registry == nil {
		return ServiceListResult{Services: []ServiceRow{}, Probed: true}
	}

	all := s.Registry.ServicesByPort()
	total := len(all)
	page := opts.Page
	limit := opts.Limit
	var startIdx, endIdx, totalPages int
	display := all
	if limit > 0 {
		if page < 1 {
			page = 1
		}
		totalPages = (total + limit - 1) / limit
		startIdx = (page - 1) * limit
		if startIdx < total {
			endIdx = min(startIdx+limit, total)
			display = all[startIdx:endIdx]
		} else {
			display = nil
			startIdx, endIdx = total, total
		}
	} else {
		endIdx = total
		page = 1
	}

	result := ServiceListResult{
		Services:      make([]ServiceRow, 0, len(display)),
		Probed:        true,
		Page:          page,
		Limit:         limit,
		TotalServices: total,
		TotalPages:    totalPages,
		Start:         startIdx,
		End:           endIdx,
	}
	for _, svc := range display {
		repos := svc.Repos
		if repos == nil {
			repos = []string{}
		}
		result.Services = append(result.Services, ServiceRow{
			Port:      svc.Port,
			Project:   svc.Project,
			Service:   svc.Name,
			Proto:     svc.Proto,
			Runs:      svc.Runs,
			Bind:      svc.Bind,
			Repos:     repos,
			Note:      svc.Note,
			Listening: !s.Ports.Available(svc.Port),
		})
	}
	return result
}

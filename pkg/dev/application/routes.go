package application

import (
	"fmt"

	"github.com/manovaspace/orbit-cli/pkg/clierr"
	"github.com/manovaspace/orbit-cli/pkg/dev/domain"
	portsdomain "github.com/manovaspace/orbit-cli/pkg/ports/domain"
)

// RoutesFor lists the native dev routes of the registry. A nil registry is an
// unverified check, the same as `orbit port check` without a registry.
func RoutesFor(reg *portsdomain.Registry) ([]domain.Route, error) {
	if reg == nil {
		return nil, clierr.Unverified("no port registry; nothing to route")
	}
	return domain.Routes(reg), nil
}

// RouteService writes the generated Caddyfile.
type RouteService struct {
	Files TextFiles
}

// Caddyfile is the generated Caddyfile text for routes.
func (s *RouteService) Caddyfile(routes []domain.Route) string {
	return domain.Caddyfile(routes)
}

// WriteCaddyfile writes the Caddyfile for routes to path. The adapter creates
// the parent directory.
func (s *RouteService) WriteCaddyfile(path string, routes []domain.Route) error {
	if err := s.Files.WriteFile(path, []byte(domain.Caddyfile(routes))); err != nil {
		return clierr.Wrap(err, clierr.Failed(fmt.Sprintf("cannot write Caddyfile to %s", path)))
	}
	return nil
}

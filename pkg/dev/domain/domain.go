// Package domain holds the local development stack model: the endpoints,
// services and actions `orbit dev` knows about. It performs no I/O.
package domain

import "fmt"

// ServiceStatus is the connectivity and health state of one core service.
type ServiceStatus struct {
	Name    string `json:"name"`
	Host    string `json:"host"`
	Port    int    `json:"port"`
	URL     string `json:"url"`
	Ready   bool   `json:"ready"`
	Message string `json:"message"`
}

// Endpoint is one local URL the dev stack exposes.
type Endpoint struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

// PortalURL is the local developer portal.
const PortalURL = "http://localhost:10007"

// Endpoints lists the local developer URLs shown by `dev portal` and the
// `dev up` banner.
func Endpoints() []Endpoint {
	return []Endpoint{
		{Name: "Developer Portal", URL: PortalURL},
		{Name: "Authelia SSO", URL: "http://auth.internal.manova.space:10000"},
		{Name: "Forgejo Git", URL: "http://git.internal.manova.space:10000"},
		{Name: "Mailpit", URL: "http://mail.internal.manova.space:10000"},
		{Name: "Grafana", URL: "http://grafana.internal.manova.space:10000"},
	}
}

// CoreService is one stack service readiness probes.
type CoreService struct {
	Name         string
	Host         string
	Port         int
	FallbackPort int
	URL          string
}

// CoreServices are the services `dev up` reports on.
func CoreServices() []CoreService {
	return []CoreService{
		{Name: "Postgres", Host: "127.0.0.1", Port: 10333, URL: "postgres://orbit@localhost:10333/orbit"},
		{Name: "NATS", Host: "127.0.0.1", Port: 10006, URL: "nats://localhost:10006"},
		{Name: "LLDAP", Host: "127.0.0.1", Port: 3890, URL: "ldap://localhost:3890"},
		{Name: "Caddy Ingress", Host: "127.0.0.1", Port: 80, FallbackPort: 10443, URL: "http://localhost:80"},
		{Name: "Mailpit", Host: "127.0.0.1", Port: 10025, URL: "http://localhost:10025"},
	}
}

// CaddyAction is one `dev caddy` sub-action.
type CaddyAction string

const (
	CaddyReload  CaddyAction = "reload"
	CaddyRestart CaddyAction = "restart"
	CaddyLogs    CaddyAction = "logs"
)

// ParseCaddyAction validates the optional `dev caddy` argument.
func ParseCaddyAction(raw string) (CaddyAction, error) {
	switch CaddyAction(raw) {
	case CaddyReload, CaddyRestart, CaddyLogs:
		return CaddyAction(raw), nil
	default:
		return "", fmt.Errorf("unknown caddy action %q. Valid actions: reload, restart, logs", raw)
	}
}

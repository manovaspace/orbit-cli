package application

import (
	"context"
	"fmt"
	"strings"

	"github.com/manovaspace/orbit-cli/pkg/clierr"
	"github.com/manovaspace/orbit-cli/pkg/dev/domain"
	portsdomain "github.com/manovaspace/orbit-cli/pkg/ports/domain"
)

// Attached runs a foreground command with the terminal attached. The
// infrastructure adapter implements it with os/exec; tests inject a fake.
type Attached interface {
	Run(ctx context.Context, argv []string) error
}

// TextFiles reads and writes the files the tunnel and route commands use.
type TextFiles interface {
	ReadFile(path string) ([]byte, error)
	WriteFile(path string, data []byte) error
}

// TunnelInput is the resolved request for `dev tunnel`: flags already merged
// over the dev.tunnel_host and dev.tunnel_port configuration.
type TunnelInput struct {
	Registry *portsdomain.Registry
	Host     string
	Port     int
	Identity string
	Only     []string
}

// TunnelPlan is the ssh command `dev tunnel` runs and the forwards it sets up.
type TunnelPlan struct {
	Host     string           `json:"host"`
	Port     int              `json:"port"`
	Identity string           `json:"identity"`
	Forwards []domain.Forward `json:"forwards"`
	Command  []string         `json:"command"`
}

// SelectForwards returns the shared registry forwards, restricted by only. A nil
// registry or a bad name is a usage error.
func SelectForwards(reg *portsdomain.Registry, only []string) ([]domain.Forward, error) {
	if reg == nil {
		return nil, clierr.Unverified("no port registry; nothing to tunnel")
	}
	forwards, err := domain.SelectForwards(reg, only)
	if err != nil {
		return nil, clierr.Usage(err.Error())
	}
	return forwards, nil
}

// PlanTunnel validates the host and port and builds the ssh command. A missing
// host is a usage error that names the config key to set.
func PlanTunnel(in TunnelInput) (TunnelPlan, error) {
	if strings.TrimSpace(in.Host) == "" {
		return TunnelPlan{}, clierr.Usage("no tunnel host: run orbit config set dev.tunnel_host user@host")
	}
	if _, _, err := domain.ParseTunnelHost(in.Host); err != nil {
		return TunnelPlan{}, clierr.Usage(err.Error())
	}
	if in.Port < 1 || in.Port > 65535 {
		return TunnelPlan{}, clierr.Usage(fmt.Sprintf("tunnel ssh port must be between 1 and 65535, got %d", in.Port))
	}
	forwards, err := SelectForwards(in.Registry, in.Only)
	if err != nil {
		return TunnelPlan{}, err
	}
	host := strings.TrimSpace(in.Host)
	return TunnelPlan{
		Host:     host,
		Port:     in.Port,
		Identity: in.Identity,
		Forwards: forwards,
		Command:  domain.SSHArgs(host, in.Port, in.Identity, forwards),
	}, nil
}

// TunnelService runs tunnel plans and renders the artifacts that go with them.
type TunnelService struct {
	Attached Attached
	Files    TextFiles
}

// Run executes the plan's ssh command in the foreground.
func (s *TunnelService) Run(ctx context.Context, plan TunnelPlan) error {
	return s.Attached.Run(ctx, plan.Command)
}

// SSHConfig is the ~/.ssh/config Host block for the plan.
func (s *TunnelService) SSHConfig(plan TunnelPlan) (string, error) {
	text, err := domain.SSHConfigBlock(plan.Host, plan.Port, plan.Identity, plan.Forwards)
	if err != nil {
		return "", clierr.Usage(err.Error())
	}
	return text, nil
}

// AuthorizedKeyLine reads the public key file at keyPath and returns the
// server-side authorized_keys entry that allows only forwards.
func (s *TunnelService) AuthorizedKeyLine(forwards []domain.Forward, keyPath string) (string, error) {
	data, err := s.Files.ReadFile(keyPath)
	if err != nil {
		return "", clierr.Wrap(err, clierr.Failed(fmt.Sprintf("cannot read public key %s", keyPath)))
	}
	line, err := domain.AuthorizedKeyLine(forwards, string(data))
	if err != nil {
		return "", clierr.Usage(fmt.Sprintf("public key %s: %v", keyPath, err))
	}
	return line, nil
}

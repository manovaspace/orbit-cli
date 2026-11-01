package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/manovaspace/orbit-cli/pkg/dev/application"
	"github.com/manovaspace/orbit-cli/pkg/runner"
	"github.com/manovaspace/orbit-cli/pkg/runner/runnertest"
)

func TestDevCmd_Registration(t *testing.T) {
	cmd := newDevCmd()
	if cmd.Use != "dev" {
		t.Errorf("expected command use 'dev', got %q", cmd.Use)
	}

	expectedSubcommands := []string{"up", "down", "tier2", "caddy", "portal", "logs"}
	subcommands := make(map[string]bool)
	for _, sub := range cmd.Commands() {
		subcommands[sub.Name()] = true
	}

	for _, expected := range expectedSubcommands {
		if !subcommands[expected] {
			t.Errorf("expected subcommand %q to be registered under 'dev'", expected)
		}
	}
}

func TestDevUpCmd_Flags(t *testing.T) {
	cmd := newDevUpCmd()

	waitFlag := cmd.Flags().Lookup("wait")
	if waitFlag == nil {
		t.Fatal("expected --wait flag on dev up")
	}
	if waitFlag.DefValue != "false" {
		t.Errorf("expected --wait default to be false, got %s", waitFlag.DefValue)
	}
	if waitFlag.Shorthand != "w" {
		t.Errorf("expected --wait shorthand to be 'w', got %q", waitFlag.Shorthand)
	}

	timeoutFlag := cmd.Flags().Lookup("timeout")
	if timeoutFlag == nil {
		t.Fatal("expected --timeout flag on dev up")
	}
	if timeoutFlag.DefValue != "15s" {
		t.Errorf("expected --timeout default to be 15s, got %s", timeoutFlag.DefValue)
	}

	if cmd.Flags().Lookup("json") == nil {
		t.Error("expected --json flag on dev up")
	}
}

type devTestProbe struct{}

func (devTestProbe) Wait(context.Context, string, int, time.Duration) bool { return true }

func setupDevCommandTest(t *testing.T) (infraDir string, fake *runnertest.Fake) {
	t.Helper()
	infraDir = t.TempDir()
	t.Setenv("ORBIT_INFRA_DIR", infraDir)
	workspace := t.TempDir()
	t.Setenv("ORBIT_WORKSPACE", workspace)

	fake = &runnertest.Fake{}
	previousRunner, previousProbe := devRunner, devProbe
	devRunner = fake
	devProbe = devTestProbe{}
	t.Cleanup(func() {
		devRunner = previousRunner
		devProbe = previousProbe
	})
	return infraDir, fake
}

func writeInfraFile(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestDevJSONEnvelopes(t *testing.T) {
	t.Run("up", func(t *testing.T) {
		infraDir, fake := setupDevCommandTest(t)
		writeInfraFile(t, filepath.Join(infraDir, "compose.sh"))
		fake.On(filepath.Join(infraDir, "compose.sh")+" up -d", runner.Result{}, nil)

		out := new(bytes.Buffer)
		cmd := newDevUpCmd()
		cmd.SetOut(out)
		cmd.SetArgs([]string{"--json", "--timeout", "1ms"})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("dev up --json: %v", err)
		}
		data := envelopeData(t, requireEnvelope(t, out))
		if data["infraDir"] != infraDir {
			t.Errorf("infraDir = %v, want %s", data["infraDir"], infraDir)
		}
		command, ok := data["command"].(map[string]any)
		if !ok || command["name"] != filepath.Join(infraDir, "compose.sh") {
			t.Errorf("command = %v", data["command"])
		}
	})

	t.Run("down", func(t *testing.T) {
		infraDir, fake := setupDevCommandTest(t)
		writeInfraFile(t, filepath.Join(infraDir, "compose.sh"))
		fake.On(filepath.Join(infraDir, "compose.sh")+" down", runner.Result{}, nil)

		out := new(bytes.Buffer)
		cmd := newDevDownCmd()
		cmd.SetOut(out)
		cmd.SetArgs([]string{"--json"})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("dev down --json: %v", err)
		}
		data := envelopeData(t, requireEnvelope(t, out))
		if data["action"] != "down" {
			t.Errorf("action = %v", data["action"])
		}
	})

	t.Run("tier2", func(t *testing.T) {
		infraDir, fake := setupDevCommandTest(t)
		script := filepath.Join(infraDir, "scripts", "start-tier2.sh")
		writeInfraFile(t, script)
		fake.On(script, runner.Result{}, nil)

		out := new(bytes.Buffer)
		cmd := newDevTier2Cmd()
		cmd.SetOut(out)
		cmd.SetArgs([]string{"--json"})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("dev tier2 --json: %v", err)
		}
		data := envelopeData(t, requireEnvelope(t, out))
		if data["action"] != "tier2" {
			t.Errorf("action = %v", data["action"])
		}
	})

	t.Run("caddy", func(t *testing.T) {
		infraDir, fake := setupDevCommandTest(t)
		writeInfraFile(t, filepath.Join(infraDir, "compose.sh"))
		fake.On(filepath.Join(infraDir, "compose.sh")+" exec caddy", runner.Result{}, nil)

		out := new(bytes.Buffer)
		cmd := newDevCaddyCmd()
		cmd.SetOut(out)
		cmd.SetArgs([]string{"reload", "--json"})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("dev caddy --json: %v", err)
		}
		data := envelopeData(t, requireEnvelope(t, out))
		if data["action"] != "reload" {
			t.Errorf("action = %v", data["action"])
		}
	})

	t.Run("portal", func(t *testing.T) {
		setupDevCommandTest(t)

		out := new(bytes.Buffer)
		cmd := newDevPortalCmd()
		cmd.SetOut(out)
		cmd.SetArgs([]string{"--json"})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("dev portal --json: %v", err)
		}
		data := envelopeData(t, requireEnvelope(t, out))
		if data["url"] != "http://localhost:10007" {
			t.Errorf("url = %v", data["url"])
		}
	})

	t.Run("logs", func(t *testing.T) {
		_, fake := setupDevCommandTest(t)

		out := new(bytes.Buffer)
		cmd := newDevLogsCmd()
		cmd.SetOut(out)
		cmd.SetArgs([]string{"postgres", "--json"})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("dev logs --json: %v", err)
		}
		data := envelopeData(t, requireEnvelope(t, out))
		if data["streaming"] != false {
			t.Errorf("streaming = %v, want false for a plan", data["streaming"])
		}
		if len(fake.Calls()) != 0 {
			t.Errorf("logs --json ran commands: %+v", fake.Calls())
		}
	})
}

func TestDevJSONFailure(t *testing.T) {
	setupDevCommandTest(t)
	out := new(bytes.Buffer)
	cmd := newDevCaddyCmd()
	cmd.SetOut(out)
	cmd.SetArgs([]string{"bogus", "--json"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected an error for an unknown caddy action")
	}
	env := decodeEnvelope(t, out.String())
	if env.OK {
		t.Error("expected a failed envelope")
	}
	if env.Error == nil || env.Error.Code != "usage" {
		t.Errorf("error = %+v, want usage", env.Error)
	}
}

var _ application.Probe = devTestProbe{}

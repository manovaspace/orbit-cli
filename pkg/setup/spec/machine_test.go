package spec

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/manovaspace/orbit-cli/pkg/clierr"
)

func TestLoadMachineDefaults(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	missing := filepath.Join(t.TempDir(), "machine.yaml")

	m, existed, err := LoadMachine(missing)
	if err != nil {
		t.Fatalf("missing file: %v", err)
	}
	if existed {
		t.Fatal("missing file reported as existing")
	}
	if want := filepath.Join(home, "Dev", "Manova"); m.Workspace != want {
		t.Fatalf("workspace default: got %q want %q", m.Workspace, want)
	}
	if m.Services != "native" || m.GitProtocol != "https" {
		t.Fatalf("defaults: services=%q git_protocol=%q", m.Services, m.GitProtocol)
	}
	host, _ := os.Hostname()
	if want := strings.ToLower(strings.SplitN(host, ".", 2)[0]); m.Device != want {
		t.Fatalf("device default: got %q want %q", m.Device, want)
	}

	path := filepath.Join(t.TempDir(), "machine.yaml")
	body := "workspace: ~/work/manova\nservices: container\ngit_protocol: ssh\ndevice: laptop\nprojects: [falak]\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	m, existed, err = LoadMachine(path)
	if err != nil || !existed {
		t.Fatalf("existing file: existed=%v err=%v", existed, err)
	}
	if want := filepath.Join(home, "work", "manova"); m.Workspace != want {
		t.Fatalf("expanded workspace: got %q want %q", m.Workspace, want)
	}
	if m.Services != "container" || m.GitProtocol != "ssh" || m.Device != "laptop" || len(m.Projects) != 1 || m.Projects[0] != "falak" {
		t.Fatalf("file values not kept: %+v", m)
	}

	bad := filepath.Join(t.TempDir(), "machine.yaml")
	if err := os.WriteFile(bad, []byte("workspace: [\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, _, err = LoadMachine(bad)
	if clierr.Code(err) != clierr.CodeUsage || !strings.Contains(err.Error(), bad) {
		t.Fatalf("malformed file: want usage error naming path, got %v", err)
	}
}

func TestDefaultMachinePath(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/xdg")
	if got := DefaultMachinePath(); got != filepath.Join("/xdg", "orbit", "machine.yaml") {
		t.Fatalf("xdg: got %q", got)
	}
	t.Setenv("XDG_CONFIG_HOME", "")
	home, _ := os.UserHomeDir()
	if got := DefaultMachinePath(); got != filepath.Join(home, ".config", "orbit", "machine.yaml") {
		t.Fatalf("fallback: got %q", got)
	}
}

func TestSaveMachinePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix permissions only")
	}
	dir := filepath.Join(t.TempDir(), "orbit")
	path := filepath.Join(dir, "machine.yaml")
	m := Machine{Workspace: "/abs/ws", Services: "native", GitProtocol: "https", Device: "box"}
	if err := SaveMachine(path, m); err != nil {
		t.Fatalf("save: %v", err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("file mode: got %o want 600", fi.Mode().Perm())
	}
	di, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if di.Mode().Perm() != 0o700 {
		t.Fatalf("dir mode: got %o want 700", di.Mode().Perm())
	}
	loaded, existed, err := LoadMachine(path)
	if err != nil || !existed || loaded.Workspace != "/abs/ws" || loaded.Device != "box" {
		t.Fatalf("round trip: %+v existed=%v err=%v", loaded, existed, err)
	}
}

func TestMachineValidate(t *testing.T) {
	ok := Machine{Workspace: "/abs/ws", Services: "external", GitProtocol: "ssh"}
	if err := ok.Validate(); err != nil {
		t.Fatalf("valid machine: %v", err)
	}
	cases := map[string]Machine{
		"services":     {Workspace: "/abs/ws", Services: "docker", GitProtocol: "https"},
		"git_protocol": {Workspace: "/abs/ws", Services: "native", GitProtocol: "http"},
		"workspace":    {Workspace: "relative/ws", Services: "native", GitProtocol: "https"},
	}
	for field, m := range cases {
		err := m.Validate()
		if err == nil {
			t.Fatalf("%s: want error", field)
		}
		if clierr.Code(err) != clierr.CodeUsage || !strings.Contains(err.Error(), field) {
			t.Fatalf("%s: want usage error naming field, got %v", field, err)
		}
	}
}

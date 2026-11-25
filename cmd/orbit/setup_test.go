package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/manovaspace/orbit-cli/pkg/clierr"
	"github.com/manovaspace/orbit-cli/pkg/output"
	"github.com/manovaspace/orbit-cli/pkg/setup/engine"
	"github.com/manovaspace/orbit-cli/pkg/setup/spec"
	"github.com/spf13/cobra"
)

// recordPart is a fake setup part. When pending is set its plan has one action whose Do
// appends the part name to *ran; otherwise it plans nothing.
type recordPart struct {
	name    string
	pending string
	ran     *[]string
}

func (p recordPart) Name() string    { return p.name }
func (p recordPart) Needs() []string { return nil }
func (p recordPart) Plan(context.Context, *engine.State) (engine.Plan, error) {
	if p.pending == "" {
		return engine.Plan{}, nil
	}
	return engine.Plan{Actions: []engine.Action{{
		Part:    p.name,
		Summary: p.pending,
		Do: func(context.Context) error {
			*p.ran = append(*p.ran, p.name)
			return nil
		},
	}}}, nil
}

// setupFixture points the command seams at a temp home, a temp workspace with a manifest
// of projects a (orbit/a), b (orbit/b) and mixed (orbit/a and orbit/b), and fake parts.
type setupFixture struct {
	root    string
	machine string
	ran     []string
}

func newSetupFixture(t *testing.T, pending ...string) *setupFixture {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CI", "")
	fx := &setupFixture{
		root:    filepath.Join(t.TempDir(), "ws"),
		machine: filepath.Join(home, "machine.yaml"),
	}
	manifestDir := filepath.Join(fx.root, "handbook", "agents", "workspace")
	if err := os.MkdirAll(manifestDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `version: "1"
workspace: test
groups:
  orbit:
    path: orbit
    repositories:
      - name: a
      - name: b
projects:
  a:
    repos: [orbit/a]
  b:
    repos: [orbit/b]
  mixed:
    repos: [orbit/a, orbit/b]
`
	if err := os.WriteFile(filepath.Join(manifestDir, "workspace.yaml"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}

	prevMachine, prevParts, prevTTY := machinePath, defaultParts, stdinIsTerminal
	t.Cleanup(func() { machinePath, defaultParts, stdinIsTerminal = prevMachine, prevParts, prevTTY })
	machinePath = func() string { return fx.machine }
	stdinIsTerminal = func() bool { return false }
	var parts []engine.Part
	for _, name := range []string{"system", "identity", "handbook", "repos", "root", "toolchains", "services", "env", "prepare"} {
		p := recordPart{name: name, ran: &fx.ran}
		for _, pend := range pending {
			if pend == name {
				p.pending = name + " change"
			}
		}
		parts = append(parts, p)
	}
	defaultParts = func() []engine.Part { return parts }
	return fx
}

// saveMachine writes a machine file with the fixture workspace and the given projects.
func (fx *setupFixture) saveMachine(t *testing.T, projects ...string) {
	t.Helper()
	m := spec.Machine{Workspace: fx.root, Projects: projects, Services: "native", GitProtocol: "https", Device: "test"}
	if err := spec.SaveMachine(fx.machine, m); err != nil {
		t.Fatal(err)
	}
}

// runCmd executes cmd with args and returns stdout, stderr and the error. Stdin is empty.
func runCmd(t *testing.T, cmd *cobra.Command, stdin string, args ...string) (string, string, error) {
	t.Helper()
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetIn(strings.NewReader(stdin))
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), errOut.String(), err
}

func TestSetupNonInteractiveRequiresProjects(t *testing.T) {
	fx := newSetupFixture(t, "system")

	_, _, err := runCmd(t, newSetupCmd(), "", "--yes")
	if clierr.Code(err) != clierr.CodeUsage {
		t.Fatalf("err = %v, want usage error", err)
	}
	if err.Error() != "choose projects: o setup --projects falak" {
		t.Fatalf("message = %q", err.Error())
	}
	if len(fx.ran) != 0 {
		t.Fatalf("parts ran before the usage error: %v", fx.ran)
	}
	if _, statErr := os.Stat(fx.machine); statErr == nil {
		t.Fatal("machine file written before the usage error")
	}
}

func TestSetupFlagsPersistToMachineFile(t *testing.T) {
	fx := newSetupFixture(t, "system")

	_, _, err := runCmd(t, newSetupCmd(), "",
		"--projects", "a,b", "--services", "container", "--git-protocol", "ssh",
		"--workspace", fx.root, "--yes")
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	m, existed, err := spec.LoadMachine(fx.machine)
	if err != nil || !existed {
		t.Fatalf("LoadMachine existed=%v err=%v", existed, err)
	}
	if strings.Join(m.Projects, ",") != "a,b" || m.Services != "container" || m.GitProtocol != "ssh" || m.Workspace != fx.root {
		t.Fatalf("machine = %+v", m)
	}
	if len(fx.ran) != 1 || fx.ran[0] != "system" {
		t.Fatalf("parts ran = %v, want [system]", fx.ran)
	}
}

func TestSetupFlagsRejectInvalidValues(t *testing.T) {
	fx := newSetupFixture(t)
	_, _, err := runCmd(t, newSetupCmd(), "", "--projects", "a", "--services", "cloud", "--yes")
	if clierr.Code(err) != clierr.CodeUsage {
		t.Fatalf("err = %v, want usage error", err)
	}
	if _, statErr := os.Stat(fx.machine); statErr == nil {
		t.Fatal("invalid flags were saved")
	}
}

func TestSetupUnknownProjectListsNames(t *testing.T) {
	fx := newSetupFixture(t)
	_, _, err := runCmd(t, newSetupCmd(), "", "--projects", "nope", "--workspace", fx.root, "--yes")
	if clierr.Code(err) != clierr.CodeUsage {
		t.Fatalf("err = %v, want usage error", err)
	}
	if !strings.Contains(err.Error(), `"nope"`) || !strings.Contains(err.Error(), "a, b, mixed") {
		t.Fatalf("message does not list the valid names: %v", err)
	}
	if len(fx.ran) != 0 {
		t.Fatalf("parts ran for an unknown project: %v", fx.ran)
	}
}

func TestSetupInteractiveChoosesProjects(t *testing.T) {
	fx := newSetupFixture(t)
	stdinIsTerminal = func() bool { return true }

	out, _, err := runCmd(t, newSetupCmd(), "2\n", "--workspace", fx.root)
	if err != nil {
		t.Fatalf("setup: %v\n%s", err, out)
	}
	m, _, err := spec.LoadMachine(fx.machine)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(m.Projects, ",") != "b" {
		t.Fatalf("projects = %v, want [b]", m.Projects)
	}
	if !strings.Contains(out, "1) a") || !strings.Contains(out, "3) mixed") {
		t.Fatalf("prompt does not list the projects: %s", out)
	}
}

func TestParseProjectChoice(t *testing.T) {
	names := []string{"a", "b", "c"}
	got, err := parseProjectChoice("1, 3", names)
	if err != nil || strings.Join(got, ",") != "a,c" {
		t.Fatalf("numbers: got %v err %v", got, err)
	}
	got, err = parseProjectChoice("b 2", names)
	if err != nil || strings.Join(got, ",") != "b" {
		t.Fatalf("names and duplicates: got %v err %v", got, err)
	}
	if _, err := parseProjectChoice("x", names); clierr.Code(err) != clierr.CodeUsage || !strings.Contains(err.Error(), "a, b, c") {
		t.Fatalf("unknown name: err %v", err)
	}
	if _, err := parseProjectChoice("  ", names); clierr.Code(err) != clierr.CodeUsage {
		t.Fatalf("empty choice: err %v", err)
	}
}

func TestAddUnknownProjectListsNames(t *testing.T) {
	fx := newSetupFixture(t)
	fx.saveMachine(t, "a")

	_, _, err := runCmd(t, newAddCmd(), "", "nope")
	if clierr.Code(err) != clierr.CodeUsage {
		t.Fatalf("err = %v, want usage error", err)
	}
	if !strings.Contains(err.Error(), `"nope"`) || !strings.Contains(err.Error(), "a, b, mixed") {
		t.Fatalf("message does not list the valid names: %v", err)
	}
}

func TestAddWithoutManifestAsksForSetup(t *testing.T) {
	fx := newSetupFixture(t)
	if err := os.Remove(filepath.Join(fx.root, "handbook", "agents", "workspace", "workspace.yaml")); err != nil {
		t.Fatal(err)
	}
	_, _, err := runCmd(t, newAddCmd(), "", "a")
	if clierr.Code(err) != clierr.CodeUsage || !strings.Contains(err.Error(), "run o setup first") {
		t.Fatalf("err = %v, want usage error asking for o setup", err)
	}
}

func TestAddSavesProjectAndAppliesIt(t *testing.T) {
	fx := newSetupFixture(t, "repos")
	fx.saveMachine(t, "a")

	if _, _, err := runCmd(t, newAddCmd(), "", "--yes", "b"); err != nil {
		t.Fatalf("add: %v", err)
	}
	m, _, err := spec.LoadMachine(fx.machine)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(m.Projects, ",") != "a,b" {
		t.Fatalf("projects = %v, want [a b]", m.Projects)
	}
	if len(fx.ran) != 1 || fx.ran[0] != "repos" {
		t.Fatalf("parts ran = %v, want [repos]", fx.ran)
	}
}

func TestRemoveNeverDeletes(t *testing.T) {
	fx := newSetupFixture(t)
	fx.saveMachine(t, "a", "b")
	repoDir := filepath.Join(fx.root, "orbit", "b")
	if err := os.MkdirAll(filepath.Join(repoDir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoDir, ".git", "HEAD"), []byte("ref: refs/heads/main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	keep := filepath.Join(repoDir, "unpushed.txt")
	if err := os.WriteFile(keep, []byte("work"), 0o600); err != nil {
		t.Fatal(err)
	}

	out, _, err := runCmd(t, newRemoveCmd(), "", "--yes", "b")
	if err != nil {
		t.Fatalf("remove: %v\n%s", err, out)
	}
	if _, statErr := os.Stat(keep); statErr != nil {
		t.Fatalf("remove deleted the repo files: %v", statErr)
	}
	if !strings.Contains(out, "no longer selected") || !strings.Contains(out, "orbit/b") {
		t.Fatalf("remove did not report the repo no longer selected: %s", out)
	}
	m, _, err := spec.LoadMachine(fx.machine)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(m.Projects, ",") != "a" {
		t.Fatalf("projects = %v, want [a]", m.Projects)
	}
}

func TestProjectsReportsChosenAndState(t *testing.T) {
	fx := newSetupFixture(t)
	fx.saveMachine(t, "a", "mixed")
	if err := os.MkdirAll(filepath.Join(fx.root, "orbit", "a", ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fx.root, "orbit", "a", ".git", "HEAD"), []byte("ref: refs/heads/main\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	out, _, err := runCmd(t, newProjectsCmd(), "", "--json")
	if err != nil {
		t.Fatalf("projects --json: %v\n%s", err, out)
	}
	var envelope output.Envelope
	if err := json.Unmarshal([]byte(out), &envelope); err != nil {
		t.Fatalf("decode envelope: %v\n%s", err, out)
	}
	data, _ := json.Marshal(envelope.Data)
	var rows []struct {
		Name   string `json:"name"`
		Chosen bool   `json:"chosen"`
		State  string `json:"state"`
	}
	if err := json.Unmarshal(data, &rows); err != nil {
		t.Fatalf("decode rows: %v\n%s", err, out)
	}
	got := map[string]string{}
	chosen := map[string]bool{}
	for _, r := range rows {
		got[r.Name] = r.State
		chosen[r.Name] = r.Chosen
	}
	want := map[string]string{"a": "cloned", "b": "not cloned", "mixed": "partial"}
	for name, state := range want {
		if got[name] != state {
			t.Errorf("state of %s = %q, want %q", name, got[name], state)
		}
	}
	if !chosen["a"] || !chosen["mixed"] || chosen["b"] {
		t.Errorf("chosen = %v, want a and mixed only", chosen)
	}
}

func TestInitAliasPrintsNoticeAndRunsSetup(t *testing.T) {
	fx := newSetupFixture(t)
	_, errOut, err := runCmd(t, newInitCmd(), "", "--projects", "a", "--workspace", fx.root, "--yes")
	if err != nil {
		t.Fatalf("init: %v", err)
	}
	if !strings.Contains(errOut, "o init is now o setup") {
		t.Fatalf("stderr = %q, want the alias notice", errOut)
	}
	if _, statErr := os.Stat(fx.machine); statErr != nil {
		t.Fatalf("init did not run setup: %v", statErr)
	}
}

func TestSetupCancelledAnswerStopsBeforeParts(t *testing.T) {
	fx := newSetupFixture(t, "system")
	stdinIsTerminal = func() bool { return true }
	fx.saveMachine(t, "a")

	_, _, err := runCmd(t, newSetupCmd(), "n\n")
	if err == nil || len(fx.ran) != 0 {
		t.Fatalf("cancelled setup: err %v, ran %v", err, fx.ran)
	}
}

// failPart is a setup part whose plan fails, so the handbook phase errors out.
type failPart struct{ name string }

func (p failPart) Name() string    { return p.name }
func (p failPart) Needs() []string { return nil }
func (p failPart) Plan(context.Context, *engine.State) (engine.Plan, error) {
	return engine.Plan{}, clierr.Failed("fake " + p.name + " failure")
}

func TestSetupYesNeverPrompts(t *testing.T) {
	fx := newSetupFixture(t, "system")
	stdinIsTerminal = func() bool { return true }

	_, _, err := runCmd(t, newSetupCmd(), "2\n", "--yes", "--workspace", fx.root)
	if err == nil || err.Error() != setupProjectsUsage {
		t.Fatalf("err = %v, want %q", err, setupProjectsUsage)
	}
	if clierr.Code(err) != clierr.CodeUsage || len(fx.ran) != 0 {
		t.Fatalf("code %s, parts ran %v", clierr.Code(err), fx.ran)
	}
}

func TestSetupFlagsSaveBeforeHandbookFailure(t *testing.T) {
	fx := newSetupFixture(t)
	if err := os.RemoveAll(filepath.Join(fx.root, "handbook")); err != nil {
		t.Fatal(err)
	}
	defaultParts = func() []engine.Part { return []engine.Part{failPart{name: "handbook"}} }

	_, _, err := runCmd(t, newSetupCmd(), "", "--projects", "a", "--services", "container", "--workspace", fx.root, "--yes")
	if err == nil {
		t.Fatal("setup succeeded with a failing handbook phase")
	}
	m, existed, loadErr := spec.LoadMachine(fx.machine)
	if loadErr != nil || !existed {
		t.Fatalf("machine file existed=%v err=%v", existed, loadErr)
	}
	// Projects wait for the manifest, so the --projects value is not saved here.
	if len(m.Projects) != 0 || m.Services != "container" {
		t.Fatalf("machine = %+v, want the other flags saved and no projects", m)
	}
}

// manifestWritePart stands in for the handbook phase: its only action writes the manifest.
type manifestWritePart struct {
	path    string
	content []byte
}

func (p manifestWritePart) Name() string    { return "handbook" }
func (p manifestWritePart) Needs() []string { return nil }
func (p manifestWritePart) Plan(context.Context, *engine.State) (engine.Plan, error) {
	return engine.Plan{Actions: []engine.Action{{
		Part:    "handbook",
		Summary: "write manifest",
		Do: func(context.Context) error {
			if err := os.MkdirAll(filepath.Dir(p.path), 0o755); err != nil {
				return err
			}
			return os.WriteFile(p.path, p.content, 0o600)
		},
	}}}, nil
}

func TestSetupProjectsWaitForManifest(t *testing.T) {
	fx := newSetupFixture(t)
	manifestPath := filepath.Join(fx.root, "handbook", "agents", "workspace", "workspace.yaml")
	content, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(fx.root, "handbook")); err != nil {
		t.Fatal(err)
	}
	defaultParts = func() []engine.Part { return []engine.Part{manifestWritePart{path: manifestPath, content: content}} }

	_, _, err = runCmd(t, newSetupCmd(), "", "--projects", "bogus", "--services", "container", "--workspace", fx.root, "--yes")
	if clierr.Code(err) != clierr.CodeUsage {
		t.Fatalf("err = %v, want usage error for unknown project", err)
	}
	m, existed, loadErr := spec.LoadMachine(fx.machine)
	if loadErr != nil || !existed {
		t.Fatalf("machine file existed=%v err=%v", existed, loadErr)
	}
	if len(m.Projects) != 0 {
		t.Fatalf("machine projects = %v, want none saved before the manifest exists", m.Projects)
	}
	if m.Services != "container" {
		t.Fatalf("machine = %+v, want the other flags saved", m)
	}
}

func TestSetupDoneMessageNamesShellRC(t *testing.T) {
	cases := map[string]string{
		"/bin/zsh":  "source ~/.zshrc",
		"/bin/bash": "source ~/.bashrc",
	}
	for shell, want := range cases {
		msg := setupDoneMessage(shell)
		if !strings.Contains(msg, want) {
			t.Fatalf("shell %s: message %q lacks %q", shell, msg, want)
		}
	}
	if msg := setupDoneMessage("/bin/bash"); strings.Contains(msg, "zshrc") {
		t.Fatalf("bash message names zshrc: %q", msg)
	}
}

func TestProjectsWithoutManifestIsUnverified(t *testing.T) {
	fx := newSetupFixture(t)
	fx.saveMachine(t, "a")
	if err := os.RemoveAll(filepath.Join(fx.root, "handbook")); err != nil {
		t.Fatal(err)
	}

	_, _, err := runCmd(t, newProjectsCmd(), "")
	if clierr.ExitCode(err) != clierr.ExitUnverified {
		t.Fatalf("exit = %d, err = %v, want %d", clierr.ExitCode(err), err, clierr.ExitUnverified)
	}
	if !strings.Contains(err.Error(), "run o setup first") {
		t.Fatalf("message = %v", err)
	}
}

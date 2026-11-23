package identity

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/manovaspace/orbit-cli/pkg/runner"
	"github.com/manovaspace/orbit-cli/pkg/runner/runnertest"
	"github.com/manovaspace/orbit-cli/pkg/setup/engine"
	"github.com/manovaspace/orbit-cli/pkg/setup/spec"
)

var errExit = errors.New("exit status 1")

// gitState describes the machine the identity part reads through the fake runner.
type gitState struct {
	loggedIn  bool   // gh auth status exits 0
	name      string // git config --global user.name; "" is unset
	email     string // git config --global user.email; "" is unset
	goPrivate string // go env GOPRIVATE; "" is unset
	helper    bool   // git config credential.https://github.com.helper names gh
}

// script registers the plan-time answers for g on f. Answers registered earlier on f win,
// so tests that need a specific Do command register it before calling script.
func script(f *runnertest.Fake, g gitState) {
	if g.loggedIn {
		f.On("gh auth status", runner.Result{}, nil)
	} else {
		f.On("gh auth status", runner.Result{ExitCode: 1}, errExit)
	}
	onValue(f, "git config --global user.name", g.name)
	onValue(f, "git config --global user.email", g.email)
	if g.helper {
		f.On("git config --global --get-all credential.https://github.com.helper", runner.Result{Stdout: "gh auth git-credential\n"}, nil)
	} else {
		f.On("git config --global --get-all credential.https://github.com.helper", runner.Result{ExitCode: 1}, errExit)
	}
	f.On("go env GOPRIVATE", runner.Result{Stdout: g.goPrivate + "\n"}, nil)
}

// onValue answers a git config read: unset exits 1, set prints the value.
func onValue(f *runnertest.Fake, prefix, v string) {
	if v == "" {
		f.On(prefix, runner.Result{ExitCode: 1}, errExit)
		return
	}
	f.On(prefix, runner.Result{Stdout: v + "\n"}, nil)
}

func allLookPath(string) (string, error) { return "/usr/bin/go", nil }

func noEnv(string) string { return "" }

func envOf(kv map[string]string) func(string) string {
	return func(k string) string { return kv[k] }
}

// setCalls returns the git config write commands ("config --global user.* value") in order.
func setCalls(calls []runnertest.Call) []string {
	var out []string
	for _, c := range calls {
		if c.Name == "git" && len(c.Args) == 4 && strings.HasPrefix(c.Args[2], "user.") {
			out = append(out, strings.Join(append([]string{c.Name}, c.Args...), " "))
		}
	}
	return out
}

func summaries(plan engine.Plan) []string {
	var out []string
	for _, a := range plan.Actions {
		out = append(out, a.Summary)
	}
	return out
}

func TestIdentityDeviceLoginWhenInteractive(t *testing.T) {
	run := &runnertest.Fake{}
	script(run, gitState{})
	tty := &runnertest.Fake{}
	tty.On("gh auth login", runner.Result{}, nil)
	s := &engine.State{Machine: spec.Machine{GitProtocol: "https"}, Run: run, Tty: tty}

	plan, err := part{lookPath: allLookPath, getenv: noEnv}.Plan(context.Background(), s)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(plan.Findings) != 0 {
		t.Fatalf("findings = %v, want none", plan.Findings)
	}
	if len(plan.Actions) == 0 || plan.Actions[0].Summary != "GitHub login" {
		t.Fatalf("actions = %v, want GitHub login first", summaries(plan))
	}
	login := plan.Actions[0]
	if !login.Browser {
		t.Fatal("GitHub login action: Browser = false, want true")
	}
	if err := login.Do(context.Background()); err != nil {
		t.Fatalf("login Do: %v", err)
	}
	calls := tty.Calls()
	want := []string{"gh", "auth", "login", "-h", "github.com", "-p", "https", "-w"}
	if len(calls) != 1 || !calls[0].Interactive {
		t.Fatalf("tty calls = %+v, want one interactive call", calls)
	}
	got := append([]string{calls[0].Name}, calls[0].Args...)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("login command = %v, want %v", got, want)
	}
}

func TestIdentityTokenModeNoPrompt(t *testing.T) {
	run := &runnertest.Fake{}
	// gh auth status fails here; GH_TOKEN is what would make it pass in reality. No login must be planned.
	script(run, gitState{name: "Jane", email: "jane@example.com", goPrivate: "github.com/manovaspace/*"})
	run.On("gh auth setup-git", runner.Result{}, nil)
	tty := &runnertest.Fake{}
	s := &engine.State{NonInteractive: true, Machine: spec.Machine{GitProtocol: "https"}, Run: run, Tty: tty}

	plan, err := part{lookPath: allLookPath, getenv: envOf(map[string]string{"GH_TOKEN": "token"})}.Plan(context.Background(), s)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(plan.Findings) != 0 {
		t.Fatalf("findings = %v, want none", plan.Findings)
	}
	if got := summaries(plan); !reflect.DeepEqual(got, []string{"Configure git to use GitHub credentials"}) {
		t.Fatalf("actions = %v, want only the credential helper", got)
	}
	if err := plan.Actions[0].Do(context.Background()); err != nil {
		t.Fatalf("setup-git Do: %v", err)
	}
	if calls := tty.Calls(); len(calls) != 0 {
		t.Fatalf("tty calls = %+v, want none in token mode", calls)
	}
	calls := run.Calls()
	last := calls[len(calls)-1]
	if got := strings.Join(append([]string{last.Name}, last.Args...), " "); got != "gh auth setup-git -h github.com" {
		t.Fatalf("last command = %q, want gh auth setup-git -h github.com", got)
	}
}

func TestIdentityNonInteractiveNoTokenIsFinding(t *testing.T) {
	run := &runnertest.Fake{}
	script(run, gitState{})
	s := &engine.State{NonInteractive: true, Machine: spec.Machine{GitProtocol: "https"}, Run: run, Tty: &runnertest.Fake{}}

	plan, err := part{lookPath: allLookPath, getenv: noEnv}.Plan(context.Background(), s)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(plan.Actions) != 0 {
		t.Fatalf("actions = %v, want none", summaries(plan))
	}
	if len(plan.Findings) != 1 {
		t.Fatalf("findings = %v, want one", plan.Findings)
	}
	f := plan.Findings[0]
	want := "GitHub login needed: run o setup in a terminal or set GH_TOKEN"
	if f.Severity != engine.SeverityError || f.Message != want {
		t.Fatalf("finding = %+v, want error %q", f, want)
	}
}

func TestIdentityFillsGitNameEmail(t *testing.T) {
	cases := []struct {
		name       string
		g          gitState
		ghName     string
		ghLogin    string
		ghEmails   string
		emailsFail bool
		ghID       string
		wantSets   []string
	}{
		{
			name:     "from profile",
			g:        gitState{loggedIn: true, helper: true, goPrivate: "github.com/manovaspace/*"},
			ghLogin:  "octo",
			ghEmails: "octo@example.com\n",
			wantSets: []string{"git config --global user.name octo", "git config --global user.email octo@example.com"},
		},
		{
			name:       "noreply fallback",
			g:          gitState{loggedIn: true, helper: true, goPrivate: "github.com/manovaspace/*"},
			ghName:     "Jane Doe",
			ghLogin:    "octo",
			emailsFail: true,
			ghID:       "123",
			wantSets:   []string{"git config --global user.name Jane Doe", "git config --global user.email 123+octo@users.noreply.github.com"},
		},
		{
			name:     "only missing email",
			g:        gitState{loggedIn: true, helper: true, goPrivate: "github.com/manovaspace/*", name: "Jane"},
			ghLogin:  "octo",
			ghEmails: "octo@example.com\n",
			wantSets: []string{"git config --global user.email octo@example.com"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			run := &runnertest.Fake{}
			for _, line := range c.wantSets {
				run.On(line, runner.Result{}, nil)
			}
			run.On("gh api user -q .name", runner.Result{Stdout: c.ghName + "\n"}, nil)
			run.On("gh api user -q .login", runner.Result{Stdout: c.ghLogin + "\n"}, nil)
			run.On("gh api user -q .id", runner.Result{Stdout: c.ghID + "\n"}, nil)
			if c.emailsFail {
				run.On("gh api user/emails", runner.Result{ExitCode: 1}, errExit)
			} else {
				run.On("gh api user/emails", runner.Result{Stdout: c.ghEmails}, nil)
			}
			script(run, c.g)
			s := &engine.State{Machine: spec.Machine{GitProtocol: "https"}, Run: run, Tty: &runnertest.Fake{}}

			plan, err := part{lookPath: allLookPath, getenv: noEnv}.Plan(context.Background(), s)
			if err != nil {
				t.Fatalf("Plan: %v", err)
			}
			if got := summaries(plan); !reflect.DeepEqual(got, []string{"Set git name and email"}) {
				t.Fatalf("actions = %v, want only Set git name and email", got)
			}
			if err := plan.Actions[0].Do(context.Background()); err != nil {
				t.Fatalf("Do: %v", err)
			}
			if got := setCalls(run.Calls()); !reflect.DeepEqual(got, c.wantSets) {
				t.Fatalf("git config writes = %v, want %v", got, c.wantSets)
			}
		})
	}
}

func TestIdentityGOPRIVATE(t *testing.T) {
	cases := []struct {
		name      string
		goPrivate string
		noGo      bool
		want      string // summary of the expected action, "" for none
	}{
		{name: "unset", want: "go env -w GOPRIVATE=github.com/manovaspace/*"},
		{name: "appends", goPrivate: "example.com/*", want: "go env -w GOPRIVATE=example.com/*,github.com/manovaspace/*"},
		{name: "present", goPrivate: "example.com/*,github.com/manovaspace/*"},
		{name: "no go", noGo: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			run := &runnertest.Fake{}
			script(run, gitState{loggedIn: true, helper: true, name: "Jane", email: "jane@example.com", goPrivate: c.goPrivate})
			run.On("go env -w", runner.Result{}, nil)
			look := allLookPath
			if c.noGo {
				look = func(string) (string, error) { return "", errExit }
			}
			s := &engine.State{Machine: spec.Machine{GitProtocol: "https"}, Run: run, Tty: &runnertest.Fake{}}

			plan, err := part{lookPath: look, getenv: noEnv}.Plan(context.Background(), s)
			if err != nil {
				t.Fatalf("Plan: %v", err)
			}
			if c.want == "" {
				if len(plan.Actions) != 0 {
					t.Fatalf("actions = %v, want none", summaries(plan))
				}
				return
			}
			if got := summaries(plan); !reflect.DeepEqual(got, []string{c.want}) {
				t.Fatalf("actions = %v, want %q", got, c.want)
			}
			if err := plan.Actions[0].Do(context.Background()); err != nil {
				t.Fatalf("Do: %v", err)
			}
			calls := run.Calls()
			last := calls[len(calls)-1]
			if got := strings.Join(append([]string{last.Name}, last.Args...), " "); got != c.want {
				t.Fatalf("last command = %q, want %q", got, c.want)
			}
		})
	}
}

func TestIdentityCredentialHelper(t *testing.T) {
	cases := []struct {
		name     string
		protocol string
		helper   bool
		want     []string
	}{
		{name: "ssh ignores helper", protocol: "ssh", helper: false},
		{name: "https with helper", protocol: "https", helper: true},
		{name: "https without helper", protocol: "https", helper: false, want: []string{"Configure git to use GitHub credentials"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			run := &runnertest.Fake{}
			script(run, gitState{loggedIn: true, name: "Jane", email: "jane@example.com", goPrivate: "github.com/manovaspace/*", helper: c.helper})
			s := &engine.State{Machine: spec.Machine{GitProtocol: c.protocol}, Run: run, Tty: &runnertest.Fake{}}

			plan, err := part{lookPath: allLookPath, getenv: noEnv}.Plan(context.Background(), s)
			if err != nil {
				t.Fatalf("Plan: %v", err)
			}
			if got := summaries(plan); !reflect.DeepEqual(got, c.want) {
				t.Fatalf("actions = %v, want %v", got, c.want)
			}
		})
	}
}

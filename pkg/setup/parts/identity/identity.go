// Package identity makes a machine able to reach GitHub as the staff member: a gh login (or a
// GH_TOKEN), git credentials through gh, a git name and email, and GOPRIVATE for the manovaspace
// modules.
package identity

import (
	"context"
	"os"
	"os/exec"
	"strings"

	"github.com/manovaspace/orbit-cli/pkg/runner"
	"github.com/manovaspace/orbit-cli/pkg/setup/engine"
	"github.com/manovaspace/orbit-cli/pkg/setup/pkgmgr"
)

const (
	partName         = "identity"
	githubHost       = "github.com"
	goPrivatePattern = "github.com/manovaspace/*"
	credentialKey    = "credential.https://github.com.helper"
	credentialHelper = "gh auth git-credential"
	noreplyDomain    = "users.noreply.github.com"
	primaryEmailJQ   = `[.[]|select(.primary and .verified)][0].email // empty`
)

type part struct {
	lookPath pkgmgr.LookPath
	getenv   func(string) string
}

// New returns the identity part, looking up go on PATH and reading GH_TOKEN from the environment.
func New() engine.Part {
	return part{lookPath: exec.LookPath, getenv: os.Getenv}
}

func (p part) Name() string    { return partName }
func (p part) Needs() []string { return []string{"system"} }

// Plan checks GitHub login first. When the machine is not logged in and no GH_TOKEN is set, it plans
// a login on a terminal, or reports an error finding without a terminal and plans nothing else.
// Otherwise it plans the credential helper (https only), the git name and email, and GOPRIVATE.
func (p part) Plan(ctx context.Context, s *engine.State) (engine.Plan, error) {
	var plan engine.Plan
	loggedIn, err := exitStatus(ctx, s.Run, "gh", "auth", "status", "-h", githubHost)
	if err != nil {
		return engine.Plan{}, err
	}
	if !loggedIn && p.getenv("GH_TOKEN") == "" {
		if s.NonInteractive {
			plan.Findings = append(plan.Findings, engine.Finding{
				Part:     partName,
				Subject:  "github",
				Message:  "GitHub login needed: run o setup in a terminal or set GH_TOKEN",
				Severity: engine.SeverityError,
			})
			return plan, nil
		}
		plan.Actions = append(plan.Actions, loginAction(s))
	}

	if s.Machine.GitProtocol == "https" {
		helper, err := output(ctx, s.Run, "git", "config", "--global", "--get-all", credentialKey)
		if err != nil {
			return engine.Plan{}, err
		}
		if !strings.Contains(helper, credentialHelper) {
			plan.Actions = append(plan.Actions, credentialAction(s))
		}
	}

	nameAction, err := identityAction(ctx, s)
	if err != nil {
		return engine.Plan{}, err
	}
	if nameAction != nil {
		plan.Actions = append(plan.Actions, *nameAction)
	}

	goAction, err := p.goPrivateAction(ctx, s)
	if err != nil {
		return engine.Plan{}, err
	}
	if goAction != nil {
		plan.Actions = append(plan.Actions, *goAction)
	}
	return plan, nil
}

// loginAction runs gh auth login on the terminal, with the protocol from the machine file.
func loginAction(s *engine.State) engine.Action {
	tty := s.Tty
	protocol := s.Machine.GitProtocol
	return engine.Action{
		Part:    partName,
		Subject: "github",
		Summary: "GitHub login",
		Browser: true,
		Do: func(ctx context.Context) error {
			return tty.RunInteractive(ctx, "", nil, "gh", "auth", "login", "-h", githubHost, "-p", protocol, "-w")
		},
	}
}

// credentialAction points git's https credential helper for github.com at gh.
func credentialAction(s *engine.State) engine.Action {
	run := s.Run
	return engine.Action{
		Part:    partName,
		Subject: "git credential helper",
		Summary: "Configure git to use GitHub credentials",
		Do: func(ctx context.Context) error {
			_, err := run.Run(ctx, "", "gh", "auth", "setup-git", "-h", githubHost)
			return err
		},
	}
}

// identityAction plans one action that fills whichever of git's user.name and user.email are unset.
// The GitHub profile is read in Do, because the login may not exist yet at plan time.
func identityAction(ctx context.Context, s *engine.State) (*engine.Action, error) {
	name, err := output(ctx, s.Run, "git", "config", "--global", "user.name")
	if err != nil {
		return nil, err
	}
	email, err := output(ctx, s.Run, "git", "config", "--global", "user.email")
	if err != nil {
		return nil, err
	}
	setName, setEmail := name == "", email == ""
	if !setName && !setEmail {
		return nil, nil
	}
	run := s.Run
	return &engine.Action{
		Part:    partName,
		Subject: "git identity",
		Summary: "Set git name and email",
		Do: func(ctx context.Context) error {
			return fillGitIdentity(ctx, run, setName, setEmail)
		},
	}, nil
}

// fillGitIdentity writes the requested git user.name and user.email from the GitHub profile.
// The name falls back to the login. The email is the primary verified address, else the GitHub
// noreply address.
func fillGitIdentity(ctx context.Context, run runner.Runner, setName, setEmail bool) error {
	login, err := ghValue(ctx, run, "user", "-q", ".login")
	if err != nil {
		return err
	}
	if setName {
		name, err := ghValue(ctx, run, "user", "-q", ".name")
		if err != nil {
			return err
		}
		if name == "" {
			name = login
		}
		if err := gitSet(ctx, run, "user.name", name); err != nil {
			return err
		}
	}
	if setEmail {
		email := primaryEmail(ctx, run)
		if email == "" {
			id, err := ghValue(ctx, run, "user", "-q", ".id")
			if err != nil {
				return err
			}
			email = id + "+" + login + "@" + noreplyDomain
		}
		if err := gitSet(ctx, run, "user.email", email); err != nil {
			return err
		}
	}
	return nil
}

// primaryEmail returns the primary verified address, or "" when the lookup fails or finds none.
func primaryEmail(ctx context.Context, run runner.Runner) string {
	email, err := ghValue(ctx, run, "user/emails", "--jq", primaryEmailJQ)
	if err != nil {
		return ""
	}
	return email
}

// ghValue runs gh api with args and returns its trimmed stdout. Any failure is an error.
func ghValue(ctx context.Context, run runner.Runner, args ...string) (string, error) {
	res, err := run.Run(ctx, "", "gh", append([]string{"api"}, args...)...)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(res.Stdout), nil
}

func gitSet(ctx context.Context, run runner.Runner, key, value string) error {
	_, err := run.Run(ctx, "", "git", "config", "--global", key, value)
	return err
}

// goPrivateAction plans go env -w GOPRIVATE so the manovaspace modules skip the public proxy. It
// is skipped silently when go is not on PATH; the toolchain part re-checks once go is installed.
func (p part) goPrivateAction(ctx context.Context, s *engine.State) (*engine.Action, error) {
	if _, err := p.lookPath("go"); err != nil {
		return nil, nil
	}
	current, err := output(ctx, s.Run, "go", "env", "GOPRIVATE")
	if err != nil {
		return nil, err
	}
	for _, pattern := range strings.Split(current, ",") {
		if strings.TrimSpace(pattern) == goPrivatePattern {
			return nil, nil
		}
	}
	value := goPrivatePattern
	if current != "" {
		value = current + "," + goPrivatePattern
	}
	line := "go env -w GOPRIVATE=" + value
	run := s.Run
	return &engine.Action{
		Part:    partName,
		Subject: "GOPRIVATE",
		Summary: line,
		Do: func(ctx context.Context) error {
			_, err := run.Run(ctx, "", "go", "env", "-w", "GOPRIVATE="+value)
			return err
		},
	}, nil
}

// exitStatus reports whether a command exited 0. A command that ran and exited non-zero answers
// no; a command that could not start is an error.
func exitStatus(ctx context.Context, run runner.Runner, name string, args ...string) (bool, error) {
	res, err := run.Run(ctx, "", name, args...)
	if err == nil {
		return true, nil
	}
	if res.ExitCode != 0 {
		return false, nil
	}
	return false, err
}

// output returns the trimmed stdout of a command. A non-zero exit reads as empty output, because
// git config and go env exit non-zero for unset keys; a command that could not start is an error.
func output(ctx context.Context, run runner.Runner, name string, args ...string) (string, error) {
	res, err := run.Run(ctx, "", name, args...)
	if err != nil {
		if res.ExitCode != 0 {
			return "", nil
		}
		return "", err
	}
	return strings.TrimSpace(res.Stdout), nil
}

package audit_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/manovaspace/orbit-cli/pkg/audit/application"
	"github.com/manovaspace/orbit-cli/pkg/audit/domain"
)

// fakeRegistry answers npm registry reads from a map.
type fakeRegistry map[string]string

func (r fakeRegistry) Latest(_ context.Context, pkg string) (string, error) {
	if version, ok := r[pkg]; ok {
		return version, nil
	}
	return "", errors.New("registry: package not found")
}

// fakeAdvisories records the queried packages and returns scripted hits.
type fakeAdvisories struct {
	hits []domain.AdvisoryHit
	err  error
	got  []domain.PackageRef
}

func (a *fakeAdvisories) Query(_ context.Context, packages []domain.PackageRef) ([]domain.AdvisoryHit, error) {
	a.got = packages
	return a.hits, a.err
}

// fakeGitHub answers releases, merged pull requests and Actions billing.
type fakeGitHub struct {
	releases   map[string]string
	releaseErr error
	prs        map[string][]domain.MergedPullRequest
	prErr      map[string]error
	usage      domain.ActionsUsage
	usageErr   error
	since      time.Time
	mu         sync.Mutex
}

func (g *fakeGitHub) Releases(_ context.Context, owner, repo string) (string, error) {
	if g.releaseErr != nil {
		return "", g.releaseErr
	}
	key := owner + "/" + repo
	if version, ok := g.releases[key]; ok {
		return version, nil
	}
	return "", errors.New("release: not found")
}

func (g *fakeGitHub) MergedPullRequests(_ context.Context, slug string, since time.Time) ([]domain.MergedPullRequest, error) {
	g.mu.Lock()
	g.since = since
	g.mu.Unlock()
	if err := g.prErr[slug]; err != nil {
		return nil, err
	}
	return g.prs[slug], nil
}

func (g *fakeGitHub) ActionsUsage(_ context.Context, _ string) (domain.ActionsUsage, error) {
	if g.usageErr != nil {
		return domain.ActionsUsage{}, g.usageErr
	}
	return g.usage, nil
}

type commandAnswer struct {
	prefix string
	result application.CommandResult
	err    error
}

// fakeCommands answers command calls from scripted prefixes and records calls.
type fakeCommands struct {
	answers []commandAnswer
	paths   map[string]string
	lookErr error
	calls   []string
	mu      sync.Mutex
}

func (c *fakeCommands) Run(_ context.Context, _ string, name string, args ...string) (application.CommandResult, error) {
	line := strings.Join(append([]string{name}, args...), " ")
	c.mu.Lock()
	c.calls = append(c.calls, line)
	c.mu.Unlock()
	for _, answer := range c.answers {
		if strings.HasPrefix(line, answer.prefix) {
			return answer.result, answer.err
		}
	}
	return application.CommandResult{}, errors.New("fake command: unexpected " + line)
}

func (c *fakeCommands) LookPath(name string) (string, error) {
	if c.lookErr != nil {
		return "", c.lookErr
	}
	if path, ok := c.paths[name]; ok {
		return path, nil
	}
	return "", errors.New("executable not found")
}

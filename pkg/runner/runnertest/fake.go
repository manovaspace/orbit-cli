// Package runnertest provides a scripted runner.Fake for tests.
package runnertest

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/manovaspace/orbit-cli/pkg/runner"
)

// Call is one command the fake received.
type Call struct {
	Dir  string
	Name string
	Args []string
}

type answer struct {
	prefix string
	result runner.Result
	err    error
}

// Fake records calls in order and answers them from scripted prefixes. The
// zero value is ready to use.
type Fake struct {
	mu      sync.Mutex
	calls   []Call
	answers []answer
}

// On registers an answer for command lines that start with prefix. The first
// matching registration wins.
func (f *Fake) On(prefix string, result runner.Result, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.answers = append(f.answers, answer{prefix: prefix, result: result, err: err})
}

// Run records the call and returns the first registered answer whose prefix
// matches the joined command line.
func (f *Fake) Run(_ context.Context, dir, name string, args ...string) (runner.Result, error) {
	line := strings.Join(append([]string{name}, args...), " ")

	f.mu.Lock()
	f.calls = append(f.calls, Call{Dir: dir, Name: name, Args: append([]string(nil), args...)})
	var matched *answer
	for i := range f.answers {
		if strings.HasPrefix(line, f.answers[i].prefix) {
			matched = &f.answers[i]
			break
		}
	}
	f.mu.Unlock()

	if matched == nil {
		return runner.Result{}, fmt.Errorf("runnertest: unexpected command: %s", line)
	}
	return matched.result, matched.err
}

// Calls returns a copy of the recorded calls in execution order.
func (f *Fake) Calls() []Call {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]Call, len(f.calls))
	for i, c := range f.calls {
		out[i] = Call{Dir: c.Dir, Name: c.Name, Args: append([]string(nil), c.Args...)}
	}
	return out
}

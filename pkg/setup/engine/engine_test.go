package engine

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/manovaspace/orbit-cli/pkg/clierr"
	"github.com/manovaspace/orbit-cli/pkg/manifest"
	"github.com/manovaspace/orbit-cli/pkg/setup/spec"
)

// fakePart is a Part whose Plan is a closure, so tests control what each run sees.
type fakePart struct {
	name  string
	needs []string
	plan  func(ctx context.Context, s *State) (Plan, error)
}

func (p fakePart) Name() string    { return p.name }
func (p fakePart) Needs() []string { return p.needs }
func (p fakePart) Plan(ctx context.Context, s *State) (Plan, error) {
	if p.plan == nil {
		return Plan{}, nil
	}
	return p.plan(ctx, s)
}

// actionPart plans one action named summary whose Do runs run.
func actionPart(name string, needs []string, summary string, run func() error) fakePart {
	return fakePart{
		name:  name,
		needs: needs,
		plan: func(context.Context, *State) (Plan, error) {
			return Plan{Actions: []Action{{
				Part:    name,
				Summary: summary,
				Do:      func(context.Context) error { return run() },
			}}}, nil
		},
	}
}

func newState(t *testing.T) *State {
	t.Helper()
	return &State{Root: t.TempDir(), FS: OSFS{}}
}

func statuses(r Result) map[string]string {
	out := make(map[string]string, len(r.Parts))
	for _, pr := range r.Parts {
		out[pr.Part] = pr.Status
	}
	return out
}

func TestApplyReplansAfterEarlierPart(t *testing.T) {
	flag := false
	setFlag := actionPart("a", nil, "set flag", func() error { flag = true; return nil })
	ranB := 0
	b := fakePart{
		name:  "b",
		needs: []string{"a"},
		plan: func(context.Context, *State) (Plan, error) {
			if flag {
				return Plan{}, nil
			}
			return Plan{Actions: []Action{{Part: "b", Summary: "needs flag", Do: func(context.Context) error {
				ranB++
				return nil
			}}}}, nil
		},
	}
	e := &Engine{Parts: []Part{setFlag, b}}

	res, err := e.Apply(context.Background(), newState(t))
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	got := statuses(res)
	if got["a"] != StatusChanged || got["b"] != StatusOK {
		t.Fatalf("statuses = %v, want a=changed b=ok", got)
	}
	if ranB != 0 {
		t.Fatalf("b action ran %d times, want 0 after replan", ranB)
	}
	if res.Changed != 1 || res.Failed != 0 {
		t.Fatalf("Changed=%d Failed=%d, want 1 and 0", res.Changed, res.Failed)
	}
}

func TestFailedPartSkipsDependents(t *testing.T) {
	boom := errors.New("boom")
	a := actionPart("a", nil, "fails", func() error { return boom })
	ranB := false
	b := actionPart("b", []string{"a"}, "after a", func() error { ranB = true; return nil })
	c := actionPart("c", []string{"b"}, "after b", func() error { t.Fatal("c must not run"); return nil })
	d := actionPart("d", nil, "independent", func() error { return nil })
	e := &Engine{Parts: []Part{a, b, c, d}}

	res, err := e.Apply(context.Background(), newState(t))
	if clierr.Code(err) != clierr.CodeFailed {
		t.Fatalf("Apply error code = %q (%v), want failed", clierr.Code(err), err)
	}
	if err.Error() != "setup incomplete: 1 parts failed" {
		t.Fatalf("Apply error = %q", err.Error())
	}
	got := statuses(res)
	want := map[string]string{"a": StatusFailed, "b": StatusSkipped, "c": StatusSkipped, "d": StatusChanged}
	for name, st := range want {
		if got[name] != st {
			t.Fatalf("part %s status = %q, want %q (all: %v)", name, got[name], st, got)
		}
	}
	if ranB {
		t.Fatal("b ran although its need failed")
	}
	if res.Failed != 1 || res.Changed != 1 {
		t.Fatalf("Changed=%d Failed=%d, want 1 and 1", res.Changed, res.Failed)
	}
	if res.Parts[0].Error != "boom" {
		t.Fatalf("a error = %q, want boom", res.Parts[0].Error)
	}
}

func TestCheckAppliesNothing(t *testing.T) {
	ran := 0
	a := actionPart("a", nil, "do it", func() error { ran++; return nil })
	e := &Engine{Parts: []Part{a}}

	res := e.Check(context.Background(), newState(t))
	if ran != 0 {
		t.Fatalf("Check ran %d actions, want 0", ran)
	}
	if res.Parts[0].Status != StatusPending || res.Changed != 1 {
		t.Fatalf("status=%q Changed=%d, want pending and 1", res.Parts[0].Status, res.Changed)
	}
	if len(res.Parts[0].Actions) != 1 || res.Parts[0].Actions[0] != "do it" {
		t.Fatalf("actions = %v", res.Parts[0].Actions)
	}
}

func TestConfirmCalledOnceAndNotWhenNothingToDo(t *testing.T) {
	t.Run("with actions", func(t *testing.T) {
		calls := 0
		ran := 0
		e := &Engine{
			Parts: []Part{
				actionPart("a", nil, "one", func() error { ran++; return nil }),
				actionPart("b", nil, "two", func() error { ran++; return nil }),
			},
			Confirm: func(preview []PartResult) bool {
				calls++
				if len(preview) != 2 {
					t.Errorf("preview has %d parts, want 2", len(preview))
				}
				return true
			},
		}
		if _, err := e.Apply(context.Background(), newState(t)); err != nil {
			t.Fatalf("Apply: %v", err)
		}
		if calls != 1 || ran != 2 {
			t.Fatalf("Confirm calls=%d ran=%d, want 1 and 2", calls, ran)
		}
	})

	t.Run("nothing to do", func(t *testing.T) {
		calls := 0
		e := &Engine{
			Parts: []Part{fakePart{name: "a"}},
			Confirm: func([]PartResult) bool {
				calls++
				return false
			},
		}
		if _, err := e.Apply(context.Background(), newState(t)); err != nil {
			t.Fatalf("Apply: %v", err)
		}
		if calls != 0 {
			t.Fatalf("Confirm called %d times with nothing to do, want 0", calls)
		}
	})
}

func TestCancelledReturnsUsage(t *testing.T) {
	ran := 0
	e := &Engine{
		Parts:   []Part{actionPart("a", nil, "do it", func() error { ran++; return nil })},
		Confirm: func([]PartResult) bool { return false },
	}
	_, err := e.Apply(context.Background(), newState(t))
	if clierr.Code(err) != clierr.CodeUsage {
		t.Fatalf("error code = %q (%v), want usage", clierr.Code(err), err)
	}
	if err.Error() != "setup cancelled" {
		t.Fatalf("error = %q, want setup cancelled", err.Error())
	}
	if ran != 0 {
		t.Fatalf("action ran %d times after cancel, want 0", ran)
	}
}

func TestSecondApplyHasNoActions(t *testing.T) {
	// Review Focus 1: an idempotent part plans its action only until the action has run.
	done := false
	a := fakePart{
		name: "a",
		plan: func(context.Context, *State) (Plan, error) {
			if done {
				return Plan{}, nil
			}
			return Plan{Actions: []Action{{Part: "a", Summary: "make it", Do: func(context.Context) error {
				done = true
				return nil
			}}}}, nil
		},
	}
	e := &Engine{Parts: []Part{a}}
	s := newState(t)

	if _, err := e.Apply(context.Background(), s); err != nil {
		t.Fatalf("first Apply: %v", err)
	}
	confirmed := false
	e.Confirm = func([]PartResult) bool { confirmed = true; return true }
	res, err := e.Apply(context.Background(), s)
	if err != nil {
		t.Fatalf("second Apply: %v", err)
	}
	if confirmed {
		t.Fatal("second Apply asked for confirmation with nothing to do")
	}
	if res.Changed != 0 || res.Parts[0].Status != StatusOK || len(res.Parts[0].Actions) != 0 {
		t.Fatalf("second Apply = %+v, want ok with no actions", res)
	}
}

func TestCheckWaitsWhenNeedPending(t *testing.T) {
	a := actionPart("a", nil, "install", func() error { return nil })
	b := fakePart{
		name:  "b",
		needs: []string{"a"},
		plan: func(context.Context, *State) (Plan, error) {
			return Plan{}, errors.New("tool missing until a runs")
		},
	}
	ranC := false
	c := actionPart("c", []string{"b"}, "configure", func() error { ranC = true; return nil })
	e := &Engine{Parts: []Part{a, b, c}}

	res := e.Check(context.Background(), newState(t))
	got := res.Parts
	if got[0].Status != StatusPending {
		t.Fatalf("a status = %q, want pending", got[0].Status)
	}
	if got[1].Status != StatusWaits || got[1].Error != "tool missing until a runs" {
		t.Fatalf("b = %+v, want waits with its error kept", got[1])
	}
	if got[2].Status != StatusPending {
		t.Fatalf("c status = %q, want pending (planned normally, not skipped)", got[2].Status)
	}
	if res.Failed != 0 {
		t.Fatalf("Failed = %d, want 0 for a waiting part", res.Failed)
	}
	if res.Changed != 2 {
		t.Fatalf("Changed = %d, want 2 (a and c pending)", res.Changed)
	}
	if ranC {
		t.Fatal("Check ran an action")
	}
}

func TestErrorFindingFailsPart(t *testing.T) {
	ranE, ranW := false, false
	e1 := fakePart{
		name: "e",
		plan: func(context.Context, *State) (Plan, error) {
			return Plan{
				Actions: []Action{{Part: "e", Summary: "would fix", Do: func(context.Context) error {
					ranE = true
					return nil
				}}},
				Findings: []Finding{{Part: "e", Subject: "port 10000", Message: "foreign listener", Fix: "stop it", Severity: SeverityError}},
			}, nil
		},
	}
	dependent := actionPart("d", []string{"e"}, "after e", func() error { t.Fatal("d must not run"); return nil })
	w := fakePart{
		name: "w",
		plan: func(context.Context, *State) (Plan, error) {
			return Plan{
				Actions: []Action{{Part: "w", Summary: "warn only", Do: func(context.Context) error {
					ranW = true
					return nil
				}}},
				Findings: []Finding{{Part: "w", Message: "slow mirror", Severity: SeverityWarn}},
			}, nil
		},
	}
	e := &Engine{Parts: []Part{e1, dependent, w}}

	check := e.Check(context.Background(), newState(t))
	got := statuses(check)
	if got["e"] != StatusFailed || got["d"] != StatusSkipped || got["w"] != StatusPending {
		t.Fatalf("Check statuses = %v, want e=failed d=skipped w=pending", got)
	}

	res, err := e.Apply(context.Background(), newState(t))
	if clierr.Code(err) != clierr.CodeFailed {
		t.Fatalf("Apply error = %v, want failed", err)
	}
	if ranE {
		t.Fatal("action of an error-finding part ran")
	}
	if !ranW {
		t.Fatal("action of a warn-only part did not run")
	}
	got = statuses(res)
	if got["e"] != StatusFailed || got["d"] != StatusSkipped || got["w"] != StatusChanged {
		t.Fatalf("Apply statuses = %v", got)
	}
}

func TestReloadMissingManifestIsNil(t *testing.T) {
	s := newState(t)
	s.Manifest = &manifest.WorkspaceManifest{}
	if err := s.Reload(); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	if s.Manifest != nil {
		t.Fatal("Manifest not cleared for a missing workspace.yaml")
	}
}

func TestReloadMalformedManifestIsError(t *testing.T) {
	s := newState(t)
	path := filepath.Join(s.Root, "handbook", "agents", "workspace", "workspace.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("groups: [\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := s.Reload()
	if clierr.Code(err) != clierr.CodeUsage {
		t.Fatalf("Reload error = %v, want usage", err)
	}
	if !strings.Contains(err.Error(), path) {
		t.Fatalf("Reload error %q does not name the path", err.Error())
	}
}

func TestSelectedWithoutManifestIsUnverified(t *testing.T) {
	s := &State{Machine: spec.Machine{Projects: []string{"*"}}}
	_, err := s.Selected()
	if clierr.Code(err) != clierr.CodeUnverified || clierr.ExitCode(err) != clierr.ExitUnverified {
		t.Fatalf("Selected error = %v, want unverified exit 2", err)
	}
}

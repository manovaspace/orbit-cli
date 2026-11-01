package application

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/manovaspace/orbit-cli/pkg/updater/domain"
)

type fakeUpdater struct {
	check      *domain.UpdateCheckResult
	checkErr   error
	updateErr  error
	updateCall int
}

func (f *fakeUpdater) Check(context.Context, string, string, string) (*domain.UpdateCheckResult, error) {
	return f.check, f.checkErr
}

func (f *fakeUpdater) CheckCached(context.Context, string, string, time.Duration, string, string) (*domain.UpdateCheckResult, error) {
	return f.check, f.checkErr
}

func (f *fakeUpdater) SelfUpdate(context.Context, string, string) error {
	f.updateCall++
	return f.updateErr
}

func TestSelfUpdateAlreadyUpToDate(t *testing.T) {
	buf := &bytes.Buffer{}
	svc := &Service{
		Updater: &fakeUpdater{check: &domain.UpdateCheckResult{LatestVersion: "1.0.0"}},
		Out:     buf,
	}
	res, err := svc.SelfUpdate(context.Background(), SelfUpdateInput{CurrentVersion: "v1.0.0"})
	if err != nil {
		t.Fatalf("SelfUpdate: %v", err)
	}
	if !res.UpToDate || res.HasUpdate {
		t.Errorf("result = %+v", res)
	}
	out := buf.String()
	if !strings.Contains(out, "Orbit CLI Self-Update") || !strings.Contains(out, "already up to date") {
		t.Errorf("output = %s", out)
	}
}

func TestSelfUpdateCheckOnlyWithHighlights(t *testing.T) {
	buf := &bytes.Buffer{}
	svc := &Service{
		Updater: &fakeUpdater{check: &domain.UpdateCheckResult{
			LatestVersion: "1.2.0",
			HasUpdate:     true,
			Release:       &domain.ReleaseInfo{ReleaseNotes: "- Fixed a thing\n- Added another"},
		}},
		Out: buf,
	}
	res, err := svc.SelfUpdate(context.Background(), SelfUpdateInput{CurrentVersion: "v1.0.0", CheckOnly: true})
	if err != nil {
		t.Fatalf("SelfUpdate: %v", err)
	}
	if !res.CheckOnly || res.Updated || len(res.Highlights) != 2 {
		t.Errorf("result = %+v", res)
	}
	out := buf.String()
	if !strings.Contains(out, "v1.0.0") || !strings.Contains(out, "v1.2.0") {
		t.Errorf("expected versions in output: %s", out)
	}
	if !strings.Contains(out, "Run 'orbit self-update' to install") {
		t.Errorf("expected install hint: %s", out)
	}
}

func TestSelfUpdateInstallsWhenConfirmed(t *testing.T) {
	buf := &bytes.Buffer{}
	fake := &fakeUpdater{check: &domain.UpdateCheckResult{LatestVersion: "1.2.0", HasUpdate: true}}
	svc := &Service{Updater: fake, Out: buf}
	res, err := svc.SelfUpdate(context.Background(), SelfUpdateInput{CurrentVersion: "v1.0.0", Yes: true})
	if err != nil {
		t.Fatalf("SelfUpdate: %v", err)
	}
	if !res.Updated || fake.updateCall != 1 {
		t.Errorf("result = %+v, update calls = %d", res, fake.updateCall)
	}
	if !strings.Contains(buf.String(), "Successfully updated Orbit to v1.2.0!") {
		t.Errorf("output = %s", buf.String())
	}
}

func TestSelfUpdateCancelsWithoutConfirmation(t *testing.T) {
	buf := &bytes.Buffer{}
	fake := &fakeUpdater{check: &domain.UpdateCheckResult{LatestVersion: "1.2.0", HasUpdate: true}}
	svc := &Service{Updater: fake, Out: buf}
	res, err := svc.SelfUpdate(context.Background(), SelfUpdateInput{CurrentVersion: "v1.0.0"})
	if err != nil {
		t.Fatalf("SelfUpdate: %v", err)
	}
	if !res.Cancelled || fake.updateCall != 0 {
		t.Errorf("result = %+v, update calls = %d", res, fake.updateCall)
	}
}

func TestSelfUpdatePermissionError(t *testing.T) {
	buf := &bytes.Buffer{}
	fake := &fakeUpdater{
		check:     &domain.UpdateCheckResult{LatestVersion: "1.2.0", HasUpdate: true},
		updateErr: errors.New("permission denied"),
	}
	svc := &Service{Updater: fake, Out: buf}
	_, err := svc.SelfUpdate(context.Background(), SelfUpdateInput{
		CurrentVersion: "v1.0.0",
		Yes:            true,
		ExecutablePath: "/usr/local/bin/orbit",
	})
	if err == nil || !strings.Contains(err.Error(), "insufficient permissions to replace /usr/local/bin/orbit") {
		t.Fatalf("error = %v", err)
	}
}

package main

import (
	"bytes"
	"testing"
	"time"

	assetsapp "github.com/manovaspace/orbit-cli/pkg/assets/application"
	assetsdomain "github.com/manovaspace/orbit-cli/pkg/assets/domain"
	"github.com/manovaspace/orbit-cli/pkg/doctor"
	envapp "github.com/manovaspace/orbit-cli/pkg/env/application"
	envdomain "github.com/manovaspace/orbit-cli/pkg/env/domain"
	migrateapp "github.com/manovaspace/orbit-cli/pkg/migrate/application"
	migratedomain "github.com/manovaspace/orbit-cli/pkg/migrate/domain"
	portsapp "github.com/manovaspace/orbit-cli/pkg/ports/application"
	"github.com/manovaspace/orbit-cli/pkg/render"
	"github.com/manovaspace/orbit-cli/pkg/render/rendertest"
	workspacedomain "github.com/manovaspace/orbit-cli/pkg/workspace/domain"
)

// batch1TextGolden names a golden file, the title line its command shows and
// how many context KV lines sit directly under the title.
type batch1TextGolden struct {
	name  string
	title string
	kvs   int
}

// batch1TextGoldens is shared with text_shape_test.go.
var batch1TextGoldens = []batch1TextGolden{
	{name: "text/status_normal", title: "Workspace status — all"},
	{name: "text/status_empty", title: "Workspace status — all"},
	{name: "text/status_failure", title: "Workspace status — all"},
	{name: "text/sync_normal", title: "Workspace sync — orbit"},
	{name: "text/sync_empty", title: "Workspace sync — orbit"},
	{name: "text/sync_failure", title: "Workspace sync — orbit"},
	{name: "text/migrate_status_normal", title: "Workspace migrations", kvs: 1},
	{name: "text/migrate_status_empty", title: "Workspace migrations", kvs: 1},
	{name: "text/migrate_apply_normal", title: "Workspace migrations", kvs: 1},
	{name: "text/migrate_apply_empty", title: "Workspace migrations", kvs: 1},
	{name: "text/port_list_normal", title: "Port allocations", kvs: 2},
	{name: "text/port_list_scanned", title: "Port allocations", kvs: 2},
	{name: "text/port_list_empty", title: "Port allocations", kvs: 2},
	{name: "text/port_allocate_normal", title: "Port allocation", kvs: 6},
	{name: "text/assets_status_normal", title: "Assets", kvs: 1},
	{name: "text/assets_status_empty", title: "Assets", kvs: 1},
	{name: "text/assets_status_problems", title: "Assets", kvs: 1},
	{name: "text/env_check_normal", title: "Environment contracts", kvs: 1},
	{name: "text/env_check_empty", title: "Environment contracts", kvs: 1},
	{name: "text/env_check_failure", title: "Environment contracts", kvs: 1},
	{name: "text/doctor_normal", title: "System doctor"},
	{name: "text/doctor_empty", title: "System doctor"},
	{name: "text/doctor_failure", title: "System doctor"},
}

func renderGold(t *testing.T, name string, draw func(*render.Printer)) {
	t.Helper()
	var buf bytes.Buffer
	p := render.NewPrinter(&buf)
	draw(p)
	if err := p.Err(); err != nil {
		t.Fatalf("render %s: %v", name, err)
	}
	rendertest.Golden(t, name, buf.String())
}

func TestBatch1TextGoldens(t *testing.T) {
	t.Setenv("COLUMNS", "120")

	t.Run("status", func(t *testing.T) {
		renderGold(t, "text/status_normal", func(p *render.Printer) {
			renderStatusResult(p, "all", []workspacedomain.RepoStatus{
				{Name: "repo-clean", Path: "repo-clean", CurrentBranch: "main", IsClean: true},
				{Name: "repo-dirty", Path: "repo-dirty", CurrentBranch: "feature", IsClean: false, ModifiedCount: 2},
				{Name: "repo-ahead", Path: "repo-ahead", CurrentBranch: "main", IsClean: true, AheadCount: 2},
				{Name: "repo-diverged", Path: "repo-diverged", CurrentBranch: "main", IsClean: false, ModifiedCount: 1, AheadCount: 1, BehindCount: 3},
				{Name: "repo-missing", Path: "repo-missing", Error: workspacedomain.ErrMissing},
				{Name: "repo-gitless", Path: "repo-gitless", Error: workspacedomain.ErrGitless},
			}, 1, 0)
		})
		renderGold(t, "text/status_empty", func(p *render.Printer) {
			renderStatusResult(p, "all", nil, 1, 0)
		})
		renderGold(t, "text/status_failure", func(p *render.Printer) {
			renderStatusResult(p, "all", []workspacedomain.RepoStatus{
				{Name: "repo-broken", Path: "repo-broken", Error: "git status failed: boom"},
			}, 1, 0)
		})
	})

	t.Run("sync", func(t *testing.T) {
		renderGold(t, "text/sync_normal", func(p *render.Printer) {
			renderSyncResult(p, "orbit", []workspacedomain.SyncResult{
				{Name: "repo-updated", Path: "repo-updated", Success: true, FastForwarded: true},
				{Name: "repo-current", Path: "repo-current", Success: true},
				{Name: "repo-skipped", Path: "repo-skipped", SkippedReason: workspacedomain.SkipUncommitted},
				{Name: "repo-failed", Path: "repo-failed", Error: "git fetch origin failed: boom"},
			}, "")
		})
		renderGold(t, "text/sync_empty", func(p *render.Printer) {
			renderSyncResult(p, "orbit", nil, "")
		})
		renderGold(t, "text/sync_failure", func(p *render.Printer) {
			renderSyncResult(p, "orbit", []workspacedomain.SyncResult{
				{Name: "repo-failed", Path: "repo-failed", Error: "git fetch origin failed: boom"},
			}, "R2 access denied")
		})
	})

	t.Run("migrate", func(t *testing.T) {
		applied := time.Date(2026, 11, 3, 9, 30, 0, 0, time.UTC)
		renderGold(t, "text/migrate_status_normal", func(p *render.Printer) {
			renderMigrateStatus(p, migrateapp.StatusResult{
				WorkspaceRoot: "/workspace",
				StatePath:     "/workspace/.orbit/migrations.json",
				Applied:       1,
				Pending:       1,
				Entries: []migrateapp.StatusEntry{
					{ID: "001_workspace_init", Description: "Initialize workspace directory tree", Applied: true, AppliedAt: &applied},
					{ID: "002_env_contracts", Description: "Verify environment contracts"},
				},
			}, 1, 0)
		})
		renderGold(t, "text/migrate_status_empty", func(p *render.Printer) {
			renderMigrateStatus(p, migrateapp.StatusResult{
				WorkspaceRoot: "/workspace",
				StatePath:     "/workspace/.orbit/migrations.json",
			}, 1, 0)
		})
		renderGold(t, "text/migrate_apply_normal", func(p *render.Printer) {
			renderMigrateApply(p, migrateapp.ApplyResult{
				WorkspaceRoot: "/workspace",
				Applied:       1,
				Results: []migratedomain.MigrationResult{
					{ID: "001_workspace_init", Description: "Initialize workspace directory tree", Success: true},
					{ID: "002_env_contracts", Description: "Verify environment contracts", Skipped: true},
					{ID: "003_remote_links", Description: "Link remote repositories", Error: "git remote failed"},
				},
			})
		})
		renderGold(t, "text/migrate_apply_empty", func(p *render.Printer) {
			renderMigrateApply(p, migrateapp.ApplyResult{WorkspaceRoot: "/workspace"})
		})
	})

	t.Run("port", func(t *testing.T) {
		renderGold(t, "text/port_list_normal", func(p *render.Printer) {
			renderPortList(p, portsapp.ListResult{
				Projects: []portsapp.Project{
					{ID: 0, Name: "orbit-platform", Base: 10000, Start: 10000, End: 10049, Scanned: false, Slots: []portsapp.Slot{
						{Index: 0, Port: 10000, Name: "Base / Database"},
						{Index: 1, Port: 10001, Name: "Core API / Backend"},
					}},
					{ID: 1, Name: "manova", Base: 10050, Start: 10050, End: 10099, Scanned: false, Slots: []portsapp.Slot{
						{Index: 0, Port: 10050, Name: "Base / Database"},
					}},
				},
				Scanned: false, Page: 1, TotalProjects: 2, TotalPages: 1, Complete: true,
			}, 0)
		})
		renderGold(t, "text/port_list_scanned", func(p *render.Printer) {
			renderPortList(p, portsapp.ListResult{
				Projects: []portsapp.Project{
					{ID: 0, Name: "orbit-platform", Base: 10000, Start: 10000, End: 10049, Scanned: true, Active: 1, DynamicFree: 38, DynamicUsed: 2, Slots: []portsapp.Slot{
						{Index: 0, Port: 10000, Name: "Base / Database", Scanned: true, Available: true},
						{Index: 1, Port: 10001, Name: "Core API / Backend", Scanned: true, Available: false},
					}},
				},
				Scanned: true, Page: 1, TotalProjects: 1, TotalPages: 1, Complete: true,
			}, 0)
		})
		renderGold(t, "text/port_list_empty", func(p *render.Printer) {
			renderPortList(p, portsapp.ListResult{TotalProjects: 0}, 0)
		})
		renderGold(t, "text/port_allocate_normal", func(p *render.Printer) {
			renderPortAllocate(p, portsapp.AllocateResult{
				Project:     "orbit-platform",
				ProjectID:   0,
				Service:     "fixture",
				Port:        10010,
				Slot:        10,
				EnvVariable: "export FIXTURE_PORT=10010",
				Reserved:    false,
			})
		})
	})

	t.Run("assets", func(t *testing.T) {
		renderGold(t, "text/assets_status_normal", func(p *render.Printer) {
			renderAssetsStatus(p, "all", assetsapp.StatusResult{
				Repositories: []assetsapp.StatusRepository{
					{Path: "repo", Files: []assetsdomain.FileStatus{
						{Path: "docs/a.pdf", State: assetsdomain.FileOK},
					}},
				},
			})
		})
		renderGold(t, "text/assets_status_empty", func(p *render.Printer) {
			renderAssetsStatus(p, "all", assetsapp.StatusResult{})
		})
		renderGold(t, "text/assets_status_problems", func(p *render.Printer) {
			renderAssetsStatus(p, "all", assetsapp.StatusResult{
				Repositories: []assetsapp.StatusRepository{
					{Path: "repo", Files: []assetsdomain.FileStatus{
						{Path: "docs/a.pdf", State: assetsdomain.FileOK},
						{Path: "docs/b.pdf", State: assetsdomain.FileMissing},
						{Path: "docs/c.pdf", State: assetsdomain.FileMismatch, Error: "hash differs"},
					}},
				},
				Problems: 2,
			})
		})
	})

	t.Run("env", func(t *testing.T) {
		renderGold(t, "text/env_check_normal", func(p *render.Printer) {
			renderEnvCheck(p, envapp.CheckResult{
				SearchPath: "/workspace",
				Verified:   true,
				ValidCount: 1,
				Schemas: []envapp.SchemaReport{
					{Directory: "services/backend", VariableCount: 3, Valid: true},
				},
				MCPEnv: &envapp.MCPEnvReport{Exists: true, Permissions: 0o600, Secure: true},
			})
		})
		renderGold(t, "text/env_check_empty", func(p *render.Printer) {
			renderEnvCheck(p, envapp.CheckResult{SearchPath: "/workspace"})
		})
		renderGold(t, "text/env_check_failure", func(p *render.Printer) {
			renderEnvCheck(p, envapp.CheckResult{
				SearchPath: "/workspace",
				Verified:   true,
				ErrorCount: 3,
				Schemas: []envapp.SchemaReport{
					{Directory: "services/frontend", Errors: []envdomain.ValidationError{
						{Variable: "DATABASE_URL", Message: "required variable is missing", Type: "missing_required"},
						{Variable: "API_URL", Message: "must be a valid URL", Type: "invalid_url"},
					}},
				},
				MCPEnv: &envapp.MCPEnvReport{Exists: true, Permissions: 0o644},
			})
		})
	})

	t.Run("doctor", func(t *testing.T) {
		normal := &doctor.DoctorReport{Results: []doctor.DiagnosticResult{
			{Category: "System", Name: "Host", Status: doctor.StatusOK, Message: "Supported host: Ubuntu 24.04 LTS amd64, zsh"},
			{Category: "Toolchain", Name: "Git", Status: doctor.StatusOK, Message: "Git v2.50.0 installed"},
			{Category: "Toolchain", Name: "Go Compiler", Status: doctor.StatusError, Message: "Go Compiler not found in PATH", FixSuggestion: "Install Go from https://go.dev/dl/"},
			{Category: "Authentication", Name: "SSH Agent", Status: doctor.StatusWarning, Message: "SSH agent is not running (SSH_AUTH_SOCK unset)", FixSuggestion: "Start SSH agent: eval $(ssh-agent -s)"},
			{Category: "Assets", Name: "R2 assets", Status: doctor.StatusUnverified, Message: "Cloud assets unverified (local mode)"},
		}}
		renderGold(t, "text/doctor_normal", func(p *render.Printer) {
			renderDoctorText(p, normal, nil)
		})
		renderGold(t, "text/doctor_empty", func(p *render.Printer) {
			renderDoctorText(p, &doctor.DoctorReport{}, nil)
		})
		renderGold(t, "text/doctor_failure", func(p *render.Printer) {
			renderDoctorText(p, &doctor.DoctorReport{Results: []doctor.DiagnosticResult{
				{Category: "Toolchain", Name: "Git", Status: doctor.StatusError, Message: "Git CLI not found in PATH", FixSuggestion: "Install Git: sudo apt install git"},
			}}, nil)
		})
	})
}

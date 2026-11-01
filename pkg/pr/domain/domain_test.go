package domain

import (
	"encoding/json"
	"testing"
)

func TestParseRemoteURL(t *testing.T) {
	tests := []struct {
		name      string
		rawURL    string
		wantOwner string
		wantRepo  string
		wantErr   bool
	}{
		{
			name:      "Standard SSH with scheme",
			rawURL:    "ssh://git@git.internal.manova.space/manova/handbook.git",
			wantOwner: "manova",
			wantRepo:  "handbook",
		},
		{
			name:      "SSH with port",
			rawURL:    "ssh://git@git.internal.manova.space:2222/manova/handbook.git",
			wantOwner: "manova",
			wantRepo:  "handbook",
		},
		{
			name:      "SCP-like syntax",
			rawURL:    "git@git.internal.manova.space:manova/handbook.git",
			wantOwner: "manova",
			wantRepo:  "handbook",
		},
		{
			name:      "GitHub SCP-like",
			rawURL:    "git@github.com:manovaspace/orbit-cli.git",
			wantOwner: "manovaspace",
			wantRepo:  "orbit-cli",
		},
		{
			name:      "HTTPS with .git suffix",
			rawURL:    "https://git.internal.manova.space/manova/orbit-frontend.git",
			wantOwner: "manova",
			wantRepo:  "orbit-frontend",
		},
		{
			name:      "HTTPS without .git suffix",
			rawURL:    "https://git.internal.manova.space/clients/pulse",
			wantOwner: "clients",
			wantRepo:  "pulse",
		},
		{
			name:      "HTTPS with trailing slash",
			rawURL:    "https://git.internal.manova.space/manova/toolkit/",
			wantOwner: "manova",
			wantRepo:  "toolkit",
		},
		{
			name:    "Empty URL",
			rawURL:  "",
			wantErr: true,
		},
		{
			name:    "Single segment URL",
			rawURL:  "https://git.internal.manova.space/onlyone",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			owner, repo, err := ParseRemoteURL(tt.rawURL)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseRemoteURL(%q) expected error, got nil", tt.rawURL)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseRemoteURL(%q) unexpected error: %v", tt.rawURL, err)
			}
			if owner != tt.wantOwner {
				t.Errorf("ParseRemoteURL(%q) owner = %q, want %q", tt.rawURL, owner, tt.wantOwner)
			}
			if repo != tt.wantRepo {
				t.Errorf("ParseRemoteURL(%q) repo = %q, want %q", tt.rawURL, repo, tt.wantRepo)
			}
		})
	}
}

func TestParseMethod(t *testing.T) {
	tests := []struct {
		value   string
		want    Method
		wantErr bool
	}{
		{value: "", want: MethodMerge},
		{value: "merge", want: MethodMerge},
		{value: " Merge ", want: MethodMerge},
		{value: "squash", want: MethodSquash},
		{value: "rebase", want: MethodRebase},
		{value: "rebase-merge", want: MethodRebase},
		{value: "fast-forward", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			got, err := ParseMethod(tt.value)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseMethod(%q) expected error, got nil", tt.value)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseMethod(%q) unexpected error: %v", tt.value, err)
			}
			if got != tt.want {
				t.Errorf("ParseMethod(%q) = %q, want %q", tt.value, got, tt.want)
			}
		})
	}
}

func TestRepoStringAndJSON(t *testing.T) {
	repo := Repo{Owner: "manovaspace", Name: "orbit-cli"}
	if got := repo.String(); got != "manovaspace/orbit-cli" {
		t.Errorf("String() = %q, want manovaspace/orbit-cli", got)
	}
	data, err := json.Marshal(repo)
	if err != nil {
		t.Fatalf("marshal repo: %v", err)
	}
	if string(data) != `"manovaspace/orbit-cli"` {
		t.Errorf("marshalled repo = %s, want the owner/name string", data)
	}
}

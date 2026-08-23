package session

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSessionCheckpointAndResume(t *testing.T) {
	tempDir := t.TempDir()
	sessionPath := filepath.Join(tempDir, "session.json")

	sm, err := NewSessionManager(sessionPath)
	if err != nil {
		t.Fatalf("NewSessionManager failed: %v", err)
	}

	if sm.FilePath() != sessionPath {
		t.Errorf("expected FilePath %s, got %s", sessionPath, sm.FilePath())
	}

	if sm.HasPendingSession() {
		t.Fatal("expected no pending session initially")
	}

	// Create and advance stage
	s := sm.CreateSession("alex@example.com", "Alex Smith")
	if s.ID == "" {
		t.Fatal("expected session ID to be populated")
	}
	if s.CurrentStage != StageInit {
		t.Fatalf("expected initial stage %s, got %s", StageInit, s.CurrentStage)
	}

	s.CurrentStage = StageDoctorPassed
	s.SSHPublicKey = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5..."
	s.UID = "alex"
	s.Metadata["role"] = "engineer"

	if err := sm.SaveSession(s); err != nil {
		t.Fatalf("SaveSession failed: %v", err)
	}

	// Verify permissions (0600)
	info, err := os.Stat(sessionPath)
	if err != nil {
		t.Fatalf("stat failed: %v", err)
	}
	if info.Mode().Perm() != 0600 {
		t.Errorf("expected permissions 0600, got %v", info.Mode().Perm())
	}

	// Reload in fresh manager
	sm2, err := NewSessionManager(sessionPath)
	if err != nil {
		t.Fatalf("NewSessionManager reload failed: %v", err)
	}

	if !sm2.HasPendingSession() {
		t.Fatal("expected pending session to be detected")
	}

	loaded, err := sm2.LoadSession()
	if err != nil {
		t.Fatalf("LoadSession failed: %v", err)
	}
	if loaded == nil {
		t.Fatal("expected loaded session to not be nil")
	}

	if loaded.Email != "alex@example.com" {
		t.Errorf("expected email alex@example.com, got %s", loaded.Email)
	}
	if loaded.DisplayName != "Alex Smith" {
		t.Errorf("expected display name Alex Smith, got %s", loaded.DisplayName)
	}
	if loaded.CurrentStage != StageDoctorPassed {
		t.Errorf("expected stage %s, got %s", StageDoctorPassed, loaded.CurrentStage)
	}
	if loaded.SSHPublicKey != s.SSHPublicKey {
		t.Errorf("expected SSH public key %s, got %s", s.SSHPublicKey, loaded.SSHPublicKey)
	}
	if loaded.UID != "alex" {
		t.Errorf("expected UID alex, got %s", loaded.UID)
	}
	if loaded.Metadata["role"] != "engineer" {
		t.Errorf("expected metadata role engineer, got %s", loaded.Metadata["role"])
	}

	// Progress through multiple stages and verify persistence
	stages := []Stage{
		StageKeypairReady,
		StageTokenClaimed,
		StageNetworkConfigured,
		StageReposCloned,
		StageMCPConfigured,
		StageDevStackReady,
	}

	for _, stage := range stages {
		loaded.CurrentStage = stage
		if err := sm2.SaveSession(loaded); err != nil {
			t.Fatalf("SaveSession at stage %s failed: %v", stage, err)
		}

		reloaded, err := sm2.LoadSession()
		if err != nil {
			t.Fatalf("LoadSession at stage %s failed: %v", stage, err)
		}
		if reloaded.CurrentStage != stage {
			t.Errorf("expected stage %s, got %s", stage, reloaded.CurrentStage)
		}
	}
}

func TestSessionCompleteAndClear(t *testing.T) {
	tempDir := t.TempDir()
	sessionPath := filepath.Join(tempDir, "session.json")

	sm, err := NewSessionManager(sessionPath)
	if err != nil {
		t.Fatalf("NewSessionManager failed: %v", err)
	}

	s := sm.CreateSession("alex@example.com", "Alex Smith")
	s.CurrentStage = StageCompleted
	if err := sm.SaveSession(s); err != nil {
		t.Fatalf("SaveSession failed: %v", err)
	}

	// Completed session should not be pending
	if sm.HasPendingSession() {
		t.Fatal("expected completed session to not count as pending")
	}

	if err := sm.ClearSession(); err != nil {
		t.Fatalf("ClearSession failed: %v", err)
	}

	if _, err := os.Stat(sessionPath); !os.IsNotExist(err) {
		t.Fatal("expected session file to be deleted")
	}

	// Clearing when file does not exist should not return error
	if err := sm.ClearSession(); err != nil {
		t.Fatalf("ClearSession on non-existent file failed: %v", err)
	}
}

func TestSessionManagerEdgeCases(t *testing.T) {
	tempDir := t.TempDir()
	sessionPath := filepath.Join(tempDir, "nested", "dir", "session.json")

	sm, err := NewSessionManager(sessionPath)
	if err != nil {
		t.Fatalf("NewSessionManager failed: %v", err)
	}

	// Loading non-existent file returns nil, nil
	loaded, err := sm.LoadSession()
	if err != nil {
		t.Fatalf("expected no error on missing session file, got %v", err)
	}
	if loaded != nil {
		t.Fatalf("expected nil session for missing file, got %+v", loaded)
	}

	// Saving nil session returns error
	if err := sm.SaveSession(nil); err == nil {
		t.Fatal("expected error when saving nil session, got nil")
	}

	// Corrupt file handling
	if err := os.MkdirAll(filepath.Dir(sessionPath), 0700); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	if err := os.WriteFile(sessionPath, []byte("invalid-json{"), 0600); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	if sm.HasPendingSession() {
		t.Fatal("expected HasPendingSession to be false for corrupted file")
	}

	if _, err := sm.LoadSession(); err == nil {
		t.Fatal("expected error when loading corrupt session file, got nil")
	}

	// Default path test
	defaultSM, err := NewSessionManager("")
	if err != nil {
		t.Fatalf("NewSessionManager with empty path failed: %v", err)
	}
	if defaultSM.FilePath() == "" {
		t.Fatal("expected default path to be non-empty")
	}
}

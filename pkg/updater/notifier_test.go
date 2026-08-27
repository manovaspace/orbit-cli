package updater

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNotifyIfUpdateAvailable(t *testing.T) {
	tempDir := t.TempDir()
	cachePath := filepath.Join(tempDir, "update-check.json")

	// 1. Create a cached result with an available update
	cached := UpdateCheckResult{
		CurrentVersion: "v0.1.0",
		LatestVersion:  "v0.2.0",
		HasUpdate:      true,
		CheckedAt:      time.Now(),
	}
	data, _ := json.Marshal(cached)
	_ = os.WriteFile(cachePath, data, 0644)

	// Set home dir or cache path for test
	t.Setenv("HOME", tempDir)
	// Create .orbit dir
	_ = os.MkdirAll(filepath.Join(tempDir, ".orbit"), 0755)
	_ = os.WriteFile(filepath.Join(tempDir, ".orbit", "update-check.json"), data, 0644)

	buf := new(bytes.Buffer)
	NotifyIfUpdateAvailable(buf, "v0.1.0")

	output := buf.String()
	if !strings.Contains(output, "A new release of Orbit is available") {
		t.Errorf("expected update notification, got: %s", output)
	}
	if !strings.Contains(output, "v0.1.0") || !strings.Contains(output, "v0.2.0") {
		t.Errorf("expected versions in notification, got: %s", output)
	}

	// 2. Test suppression via CI=true
	t.Setenv("CI", "true")
	buf.Reset()
	NotifyIfUpdateAvailable(buf, "v0.1.0")
	if buf.Len() > 0 {
		t.Errorf("expected no notification when CI=true, got: %s", buf.String())
	}
	_ = os.Unsetenv("CI")

	// 3. Test suppression via ORBIT_NO_UPDATE_NOTIFIER=1
	t.Setenv("ORBIT_NO_UPDATE_NOTIFIER", "1")
	buf.Reset()
	NotifyIfUpdateAvailable(buf, "v0.1.0")
	if buf.Len() > 0 {
		t.Errorf("expected no notification when ORBIT_NO_UPDATE_NOTIFIER=1, got: %s", buf.String())
	}
	_ = os.Unsetenv("ORBIT_NO_UPDATE_NOTIFIER")
}

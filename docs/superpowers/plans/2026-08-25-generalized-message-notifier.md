# Generalized Message & Notifier System — Implementation Plan

> **For agents:** Use the `executing-plans` skill to implement this plan task-by-task.

**Goal:** Replace version-only poller with `/api/feed` message feed; add typed message tracking in the CLI; fix worker auto-start and doctor exit-code bugs.

**Architecture:** New `pkg/notifier` package handles feed fetching, feed state persistence (`~/.manova/feed.json`), and message-store tracking (`~/.manova/messages.json`). Worker polls `/api/feed`. `PersistentPostRun` renders unseen messages. Edge worker gains `/api/feed` route. Legacy `/version` kept.

**Tech Stack:** Go 1.23+, Cloudflare Workers JS (no deps), `encoding/json`, `os.Rename` atomic I/O.

**Spec:** `docs/superpowers/specs/2026-08-25-generalized-message-notifier.md`

---

## Task 1: Edge Worker — `/api/feed` Route

**Scope:** `clients/manova/manova-infra`

**Files:**
- Modify: `static/worker.js`

### Step 1: Add the `/api/feed` handler in `worker.js`

After the existing `/version` block (around line 34), insert:

```javascript
if (url.pathname === "/api/feed") {
  const feedPayload = {
    version: "v0.2.4",
    published_at: "2026-08-24T21:23:48Z",
    status: "operational",
    messages: [
      {
        id: "release-v0.2.4",
        type: "release",
        priority: "high",
        title: "v0.2.4 available — Mandatory Zsh + OMZ enforcement",
        body: "Zsh and Oh My Zsh are now hard requirements on your machine. Run 'manova doctor --fix' to auto-install.",
        action: "manova self-update",
        published_at: "2026-08-24T21:23:48Z",
        expires_at: null
      },
      {
        id: "tip-2026-08-25-doctor-fix",
        type: "tip",
        priority: "low",
        title: "New machine? Run 'manova doctor --fix' to auto-configure your environment.",
        body: null,
        action: "manova doctor --fix",
        published_at: "2026-08-25T00:00:00Z",
        expires_at: "2026-09-15T00:00:00Z"
      }
    ]
  };
  return new Response(JSON.stringify(feedPayload, null, 2), {
    status: 200,
    headers: {
      "Content-Type": "application/json; charset=utf-8",
      "Cache-Control": "public, max-age=60, s-maxage=60, stale-while-revalidate=30",
      "Access-Control-Allow-Origin": "*",
    },
  });
}
```

### Step 2: Deploy to Cloudflare

```bash
curl -s -X PUT "https://api.cloudflare.com/client/v4/accounts/<ACCOUNT_ID>/workers/scripts/manova-get" \
  -H "Authorization: Bearer <CLOUDFLARE_API_TOKEN>" \
  -H "Content-Type: application/javascript" \
  --data-binary "@static/worker.js" | jq '.success, .errors'
```
Expected: `true` and `[]`

### Step 3: Verify

```bash
curl -s https://get.manova.space/api/feed | jq '{version, messages_count: (.messages | length)}'
```
Expected: `{"version": "v0.2.4", "messages_count": 2}`

### Step 4: Commit

```bash
cd clients/manova/manova-infra
git add static/worker.js
git commit -m "feat(edge): add /api/feed unified message+version endpoint"
git push origin main
```

---

## Task 2: `pkg/notifier` — Types

**Scope:** `orbit/orbit-cli`

**Files:**
- Create: `pkg/notifier/types.go`

### Step 1: Write `types.go`

```go
package notifier

import "time"

const (
    DefaultFeedURL      = "https://get.manova.space/api/feed"
    DefaultFeedFile     = "~/.manova/feed.json"
    DefaultStoreFile    = "~/.manova/messages.json"
    MaxMessagesPerRun   = 3
)

// FeedResponse is the /api/feed response schema.
type FeedResponse struct {
    Version     string    `json:"version"`
    PublishedAt time.Time `json:"published_at"`
    Status      string    `json:"status"`
    Messages    []Message `json:"messages"`
}

// Message is a single notifier message from the feed.
type Message struct {
    ID          string     `json:"id"`
    Type        string     `json:"type"`     // release|broadcast|tip|alert
    Priority    string     `json:"priority"` // critical|high|normal|low
    Title       string     `json:"title"`
    Body        string     `json:"body,omitempty"`
    Action      string     `json:"action,omitempty"`
    PublishedAt time.Time  `json:"published_at"`
    ExpiresAt   *time.Time `json:"expires_at,omitempty"`
}

// IsExpired returns true if the message has a non-nil ExpiresAt in the past.
func (m Message) IsExpired() bool {
    return m.ExpiresAt != nil && time.Now().UTC().After(*m.ExpiresAt)
}

// IsCritical returns true for critical priority messages.
func (m Message) IsCritical() bool {
    return m.Priority == "critical"
}

// TypeIcon returns a display icon string for the message type.
func (m Message) TypeIcon() string {
    switch m.Type {
    case "release":
        return "🔔"
    case "broadcast":
        return "📢"
    case "tip":
        return "💡"
    case "alert":
        return "⚠"
    default:
        return "ℹ"
    }
}

// MessageStore is the client-side tracking file (~/.manova/messages.json).
type MessageStore struct {
    Seen      []string  `json:"seen"`
    UpdatedAt time.Time `json:"updated_at"`
}

// FeedState is the cached feed persisted to ~/.manova/feed.json.
type FeedState struct {
    LatestVersion string    `json:"latest_version"`
    LastCheckedAt time.Time `json:"last_checked_at"`
    ServerStatus  string    `json:"server_status"`
    LastError     string    `json:"last_error,omitempty"`
    Messages      []Message `json:"messages"`
}
```

### Step 2: Run build check (no test yet)
```bash
cd orbit/orbit-cli && go build ./pkg/notifier/...
```
Expected: exits 0 (no compilation errors)

### Step 3: Commit
```bash
git add pkg/notifier/types.go
git commit -m "feat(notifier): add FeedResponse, Message, FeedState, MessageStore types"
```

---

## Task 3: `pkg/notifier` — Feed Fetcher & State Persistence

**Files:**
- Create: `pkg/notifier/feed.go`
- Create: `pkg/notifier/feed_test.go`

### Step 1: Write failing tests in `feed_test.go`

```go
package notifier_test

import (
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "os"
    "path/filepath"
    "testing"
    "time"

    "github.com/manovaspace/orbit-cli/pkg/notifier"
)

func TestFetchFeed_Success(t *testing.T) {
    expires := time.Now().Add(24 * time.Hour)
    feed := notifier.FeedResponse{
        Version: "v0.2.4",
        Status:  "operational",
        Messages: []notifier.Message{
            {ID: "release-v0.2.4", Type: "release", Priority: "high", Title: "v0.2.4"},
            {ID: "tip-1", Type: "tip", Priority: "low", Title: "a tip", ExpiresAt: &expires},
        },
    }
    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("Content-Type", "application/json")
        json.NewEncoder(w).Encode(feed)
    }))
    defer srv.Close()

    result, err := notifier.FetchFeed(srv.URL)
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    if result.Version != "v0.2.4" {
        t.Errorf("expected version v0.2.4, got %s", result.Version)
    }
    if len(result.Messages) != 2 {
        t.Errorf("expected 2 messages, got %d", len(result.Messages))
    }
}

func TestFetchFeed_NetworkError(t *testing.T) {
    _, err := notifier.FetchFeed("http://127.0.0.1:1")
    if err == nil {
        t.Fatal("expected network error, got nil")
    }
}

func TestPollFeed_WritesStateFile(t *testing.T) {
    dir := t.TempDir()
    stateFile := filepath.Join(dir, "feed.json")

    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        feed := notifier.FeedResponse{Version: "v0.2.4", Status: "operational"}
        json.NewEncoder(w).Encode(feed)
    }))
    defer srv.Close()

    state, err := notifier.PollFeed(srv.URL, stateFile)
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    if state.LatestVersion != "v0.2.4" {
        t.Errorf("expected v0.2.4, got %s", state.LatestVersion)
    }

    if _, err := os.Stat(stateFile); err != nil {
        t.Errorf("state file not written: %v", err)
    }
}

func TestPollFeed_PreservesVersionOnServerDown(t *testing.T) {
    dir := t.TempDir()
    stateFile := filepath.Join(dir, "feed.json")

    // Pre-seed a state file
    seed := notifier.FeedState{LatestVersion: "v0.2.3", ServerStatus: "ok"}
    data, _ := json.MarshalIndent(seed, "", "  ")
    os.WriteFile(stateFile, data, 0644)

    // Server returns 500
    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        http.Error(w, "internal error", http.StatusInternalServerError)
    }))
    defer srv.Close()

    state, _ := notifier.PollFeed(srv.URL, stateFile)
    if state.LatestVersion != "v0.2.3" {
        t.Errorf("expected preserved v0.2.3, got %s", state.LatestVersion)
    }
    if state.ServerStatus != "down" {
        t.Errorf("expected status down, got %s", state.ServerStatus)
    }
}
```

### Step 2: Run tests to confirm they fail
```bash
cd orbit/orbit-cli && go test ./pkg/notifier/... -v 2>&1 | head -20
```
Expected: compile error (feed.go not yet written)

### Step 3: Write `feed.go`

```go
package notifier

import (
    "encoding/json"
    "fmt"
    "net/http"
    "os"
    "path/filepath"
    "time"
)

// FetchFeed fetches and parses the /api/feed JSON from endpoint.
func FetchFeed(endpoint string) (*FeedResponse, error) {
    if endpoint == "" {
        endpoint = DefaultFeedURL
    }
    client := &http.Client{Timeout: 8 * time.Second}
    req, err := http.NewRequest(http.MethodGet, endpoint, nil)
    if err != nil {
        return nil, fmt.Errorf("feed request build failed: %w", err)
    }
    req.Header.Set("User-Agent", "manova-notifier/1.0")
    req.Header.Set("Accept", "application/json")

    resp, err := client.Do(req)
    if err != nil {
        return nil, fmt.Errorf("feed request failed: %w", err)
    }
    defer resp.Body.Close()

    if resp.StatusCode != http.StatusOK {
        return nil, fmt.Errorf("feed server returned %d", resp.StatusCode)
    }

    var feed FeedResponse
    if err := json.NewDecoder(resp.Body).Decode(&feed); err != nil {
        return nil, fmt.Errorf("feed decode failed: %w", err)
    }
    return &feed, nil
}

// expandPath expands leading ~ to home directory.
func expandPath(path string) string {
    if path == "" {
        path = DefaultFeedFile
    }
    if len(path) >= 2 && path[:2] == "~/" {
        if home, err := os.UserHomeDir(); err == nil {
            return filepath.Join(home, path[2:])
        }
    }
    return path
}

// ReadFeedState reads the persisted FeedState from statePath.
func ReadFeedState(statePath string) (*FeedState, error) {
    data, err := os.ReadFile(expandPath(statePath))
    if err != nil {
        return nil, err
    }
    var s FeedState
    if err := json.Unmarshal(data, &s); err != nil {
        return nil, fmt.Errorf("feed state parse failed: %w", err)
    }
    return &s, nil
}

// WriteFeedStateAtomic writes FeedState atomically via temp-file rename.
func WriteFeedStateAtomic(statePath string, state *FeedState) error {
    if state == nil {
        return fmt.Errorf("state cannot be nil")
    }
    dst := expandPath(statePath)
    if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
        return err
    }
    data, err := json.MarshalIndent(state, "", "  ")
    if err != nil {
        return err
    }
    data = append(data, '\n')
    tmp, err := os.CreateTemp(filepath.Dir(dst), ".feed-*.tmp")
    if err != nil {
        return err
    }
    tmpPath := tmp.Name()
    defer func() { _ = os.Remove(tmpPath) }()
    if _, err := tmp.Write(data); err != nil {
        _ = tmp.Close()
        return err
    }
    _ = tmp.Chmod(0644)
    _ = tmp.Sync()
    if err := tmp.Close(); err != nil {
        return err
    }
    return os.Rename(tmpPath, dst)
}

// PollFeed fetches the feed, updates persisted state, and returns it.
// On failure it preserves the last known state and marks server as "down".
func PollFeed(endpoint, statePath string) (*FeedState, error) {
    existing, _ := ReadFeedState(statePath)
    if existing == nil {
        existing = &FeedState{LatestVersion: "dev"}
    }

    feed, err := FetchFeed(endpoint)
    if err != nil {
        existing.ServerStatus = "down"
        existing.LastError = err.Error()
        existing.LastCheckedAt = time.Now().UTC()
        _ = WriteFeedStateAtomic(statePath, existing)
        return existing, err
    }

    existing.LatestVersion = feed.Version
    existing.Messages = feed.Messages
    existing.ServerStatus = "ok"
    existing.LastError = ""
    existing.LastCheckedAt = time.Now().UTC()

    if err := WriteFeedStateAtomic(statePath, existing); err != nil {
        return existing, fmt.Errorf("failed to persist feed state: %w", err)
    }
    return existing, nil
}
```

### Step 4: Run tests — should all pass
```bash
cd orbit/orbit-cli && go test ./pkg/notifier/... -v -run TestFetch
```
Expected: All PASS

### Step 5: Commit
```bash
git add pkg/notifier/feed.go pkg/notifier/feed_test.go
git commit -m "feat(notifier): add FetchFeed, PollFeed, and feed state persistence"
```

---

## Task 4: `pkg/notifier` — Message Store (Seen Tracking)

**Files:**
- Create: `pkg/notifier/store.go`
- Create: `pkg/notifier/store_test.go`

### Step 1: Write failing tests in `store_test.go`

```go
package notifier_test

import (
    "os"
    "path/filepath"
    "testing"
    "time"

    "github.com/manovaspace/orbit-cli/pkg/notifier"
)

func TestReadWriteStore_RoundTrip(t *testing.T) {
    dir := t.TempDir()
    path := filepath.Join(dir, "messages.json")

    store := &notifier.MessageStore{Seen: []string{"msg-1"}}
    if err := notifier.WriteStoreAtomic(path, store); err != nil {
        t.Fatalf("write failed: %v", err)
    }
    loaded, err := notifier.ReadStore(path)
    if err != nil {
        t.Fatalf("read failed: %v", err)
    }
    if len(loaded.Seen) != 1 || loaded.Seen[0] != "msg-1" {
        t.Errorf("unexpected seen: %v", loaded.Seen)
    }
}

func TestMarkSeen_Idempotent(t *testing.T) {
    dir := t.TempDir()
    path := filepath.Join(dir, "messages.json")

    if err := notifier.MarkSeen(path, "msg-1"); err != nil {
        t.Fatalf("first MarkSeen failed: %v", err)
    }
    if err := notifier.MarkSeen(path, "msg-1"); err != nil {
        t.Fatalf("second MarkSeen failed: %v", err)
    }
    store, _ := notifier.ReadStore(path)
    count := 0
    for _, id := range store.Seen {
        if id == "msg-1" {
            count++
        }
    }
    if count != 1 {
        t.Errorf("expected exactly 1 occurrence of msg-1, got %d", count)
    }
}

func TestFilterVisible_ExpiryAndSeen(t *testing.T) {
    past := time.Now().Add(-time.Hour)
    future := time.Now().Add(time.Hour)

    messages := []notifier.Message{
        {ID: "expired", Type: "tip", Priority: "low", Title: "expired", ExpiresAt: &past},
        {ID: "seen-normal", Type: "tip", Priority: "normal", Title: "seen"},
        {ID: "critical-seen", Type: "alert", Priority: "critical", Title: "critical"},
        {ID: "active", Type: "release", Priority: "high", Title: "active", ExpiresAt: &future},
    }

    store := &notifier.MessageStore{Seen: []string{"seen-normal", "critical-seen"}}
    visible := notifier.FilterVisible(messages, store)

    ids := make(map[string]bool)
    for _, m := range visible {
        ids[m.ID] = true
    }

    if ids["expired"] {
        t.Error("expired message should not be visible")
    }
    if ids["seen-normal"] {
        t.Error("seen normal message should not be visible")
    }
    if !ids["critical-seen"] {
        t.Error("critical message should always be visible even when seen")
    }
    if !ids["active"] {
        t.Error("active unseen message should be visible")
    }
}
```

### Step 2: Run to confirm failure
```bash
cd orbit/orbit-cli && go test ./pkg/notifier/... -run TestMarkSeen -v 2>&1 | head -10
```
Expected: compile error (store.go not yet written)

### Step 3: Write `store.go`

```go
package notifier

import (
    "encoding/json"
    "os"
    "path/filepath"
    "time"
)

// ReadStore reads the MessageStore from storePath. Returns an empty store if file missing.
func ReadStore(storePath string) (*MessageStore, error) {
    if storePath == "" {
        storePath = DefaultStoreFile
    }
    data, err := os.ReadFile(expandPath(storePath))
    if os.IsNotExist(err) {
        return &MessageStore{}, nil
    }
    if err != nil {
        return nil, err
    }
    var s MessageStore
    if err := json.Unmarshal(data, &s); err != nil {
        return &MessageStore{}, nil // corrupt file treated as empty
    }
    return &s, nil
}

// WriteStoreAtomic writes the MessageStore atomically via temp-file rename.
func WriteStoreAtomic(storePath string, store *MessageStore) error {
    if storePath == "" {
        storePath = DefaultStoreFile
    }
    dst := expandPath(storePath)
    if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
        return err
    }
    store.UpdatedAt = time.Now().UTC()
    data, err := json.MarshalIndent(store, "", "  ")
    if err != nil {
        return err
    }
    data = append(data, '\n')
    tmp, err := os.CreateTemp(filepath.Dir(dst), ".msgstore-*.tmp")
    if err != nil {
        return err
    }
    tmpPath := tmp.Name()
    defer func() { _ = os.Remove(tmpPath) }()
    if _, err := tmp.Write(data); err != nil {
        _ = tmp.Close()
        return err
    }
    _ = tmp.Chmod(0644)
    _ = tmp.Sync()
    if err := tmp.Close(); err != nil {
        return err
    }
    return os.Rename(tmpPath, dst)
}

// IsSeen returns true if the given message ID is in the store's Seen list.
func IsSeen(store *MessageStore, id string) bool {
    for _, s := range store.Seen {
        if s == id {
            return true
        }
    }
    return false
}

// MarkSeen adds id to the store's Seen list (idempotent) and persists atomically.
func MarkSeen(storePath, id string) error {
    store, err := ReadStore(storePath)
    if err != nil {
        return err
    }
    if IsSeen(store, id) {
        return nil
    }
    store.Seen = append(store.Seen, id)
    return WriteStoreAtomic(storePath, store)
}

// FilterVisible returns messages that should be displayed to the user.
// Filters out expired messages and non-critical already-seen messages.
// Caps results at MaxMessagesPerRun.
func FilterVisible(messages []Message, store *MessageStore) []Message {
    var visible []Message
    for _, m := range messages {
        if m.IsExpired() {
            continue
        }
        if !m.IsCritical() && IsSeen(store, m.ID) {
            continue
        }
        visible = append(visible, m)
        if len(visible) >= MaxMessagesPerRun {
            break
        }
    }
    return visible
}
```

### Step 4: Run all notifier tests — should all pass
```bash
cd orbit/orbit-cli && go test ./pkg/notifier/... -v
```
Expected: All PASS

### Step 5: Commit
```bash
git add pkg/notifier/store.go pkg/notifier/store_test.go
git commit -m "feat(notifier): add MessageStore seen-tracking with atomic I/O"
```

---

## Task 5: Wire Worker's `run-once` to notifier

**Scope:** `orbit/orbit-cli`

**Files:**
- Modify: `cmd/manova/worker.go` — `newWorkerRunOnceCmd` calls `notifier.PollFeed`
- Modify: `pkg/worker/poller.go` — add backward-compat shim that delegates to notifier

### Step 1: Update `cmd/manova/worker.go` imports and `run-once` handler

In `newWorkerRunOnceCmd`, replace `worker.PollOnce(ep, sp)` with:
```go
import "github.com/manovaspace/orbit-cli/pkg/notifier"
// ...
ep := notifier.DefaultFeedURL
sp := notifier.DefaultFeedFile
// override with flags if set
state, err := notifier.PollFeed(ep, sp)
```

Display fields map:
- `state.LatestVersion` → Latest Version
- `state.ServerStatus` → Server Status
- `state.LastCheckedAt` → Last Checked

### Step 2: Build check
```bash
cd orbit/orbit-cli && go build ./cmd/manova/...
```
Expected: exits 0

### Step 3: Run worker tests
```bash
cd orbit/orbit-cli && go test ./cmd/manova -run TestWorker -v
```
Expected: All PASS

### Step 4: Commit
```bash
git add cmd/manova/worker.go
git commit -m "feat(worker): delegate run-once to notifier.PollFeed"
```

---

## Task 6: Replace Update Banner with Message Renderer

**Files:**
- Modify: `cmd/manova/main.go`
- Modify: `cmd/manova/ui.go`

### Step 1: Add `renderMessageBanner` in `ui.go`

```go
func renderMessageBanner(msg notifier.Message) string {
    var sb strings.Builder
    icon := msg.TypeIcon()
    sb.WriteString(fmt.Sprintf("%s %s\n", icon, msg.Title))
    if msg.Body != "" {
        sb.WriteString(fmt.Sprintf("\n   %s\n", msg.Body))
    }
    if msg.Action != "" {
        sb.WriteString(fmt.Sprintf("\n → %s\n", msg.Action))
    }
    return cardStyle.Render(sb.String())
}
```

### Step 2: Update `PersistentPostRun` in `main.go`

Replace the existing update-banner block with:

```go
// Load and display feed messages (at most MaxMessagesPerRun)
if state, err := notifier.ReadFeedState(notifier.DefaultFeedFile); err == nil && state != nil {
    store, _ := notifier.ReadStore(notifier.DefaultStoreFile)
    if store == nil {
        store = &notifier.MessageStore{}
    }
    visible := notifier.FilterVisible(state.Messages, store)
    for _, msg := range visible {
        fmt.Fprintln(cmd.OutOrStdout(), renderMessageBanner(msg))
        _ = notifier.MarkSeen(notifier.DefaultStoreFile, msg.ID)
    }
}

// Trigger background worker heal/bootstrap if state file is missing or stale
needsHealing, _, _ := worker.CheckWatchdog(worker.DefaultStateFile, worker.DefaultStaleThreshold)
if needsHealing {
    execPath, _ := os.Executable()
    worker.HealWorkerBackground(execPath)
}
```

Also add: if `~/.manova/feed.json` does NOT exist (first run), call `HealWorkerBackground` unconditionally to bootstrap.

### Step 3: Compile
```bash
cd orbit/orbit-cli && go build ./cmd/manova/...
```

### Step 4: Run main tests
```bash
cd orbit/orbit-cli && go test ./cmd/manova -v 2>&1 | tail -20
```
Expected: All PASS

### Step 5: Commit
```bash
git add cmd/manova/main.go cmd/manova/ui.go
git commit -m "feat(cli): replace version banner with typed message renderer from notifier feed"
```

---

## Task 7: Fix `manova doctor` Exit Code

**Files:**
- Modify: `cmd/manova/doctor.go`

### Step 1: Locate the `RunE` return in `doctor.go`

Find this pattern (approximately):
```go
if report.HasErrors() {
    return fmt.Errorf("pre-flight diagnostics failed with %d error(s)", errorsCount)
}
```

### Step 2: Replace with non-failing return

```go
if report.HasErrors() {
    // Print error count but do NOT exit non-zero — doctor is informational
    fmt.Fprintf(out, "\n  %s  Run 'manova doctor --fix' to auto-resolve errors.\n", iconWarn)
}
return nil  // always exit 0
```

### Step 3: Test
```bash
cd orbit/orbit-cli && manova doctor; echo "exit code: $?"
```
Expected: output shows errors, but `exit code: 0`

### Step 4: Run tests
```bash
cd orbit/orbit-cli && go test ./cmd/manova -run TestDoctor -v
```

### Step 5: Commit
```bash
git add cmd/manova/doctor.go
git commit -m "fix(doctor): exit 0 even when errors found — doctor is informational, not a gate"
```

---

## Task 8: Deploy Remote Machine & Verify End-to-End

### Step 1: Build v0.2.5 binary
```bash
cd orbit/orbit-cli
bash scripts/build-release.sh v0.2.5
```

### Step 2: Push to GitHub, create release, deploy binary to remote
```bash
git tag v0.2.5
git push origin v0.2.5
git push github main && git push github v0.2.5
gh release create v0.2.5 \
  --title "v0.2.5 — Generalized Message & Notifier Feed" \
  --notes "Unified /api/feed endpoint; typed message tracking; doctor exit-code fix; worker auto-bootstrap" \
  dist/manova-linux-amd64 dist/manova-linux-arm64
```

### Step 3: Update remote machine binary
```bash
ssh root@91.107.146.32 'curl -fsSL https://get.manova.space | bash'
# or manually:
scp dist/manova-linux-amd64 root@91.107.146.32:/usr/local/bin/manova
ssh root@91.107.146.32 'chmod +x /usr/local/bin/manova && manova version'
```

### Step 4: Bootstrap worker on remote
```bash
ssh root@91.107.146.32 'manova worker start'
```
Expected: `✔ Worker daemon started successfully (systemd user timer: manova-worker.timer)`

### Step 5: Force a poll and verify messages
```bash
ssh root@91.107.146.32 'manova worker run-once && cat ~/.manova/feed.json'
```
Expected: feed.json written with `latest_version: v0.2.5` and `messages[]`

### Step 6: Trigger a CLI command to see message banner
```bash
ssh root@91.107.146.32 'manova doctor 2>&1 | tail -20'
```
Expected: message banner rendered at bottom, exit code 0

### Step 7: Verify messages.json tracking
```bash
ssh root@91.107.146.32 'cat ~/.manova/messages.json'
```
Expected: `{"seen":["release-v0.2.4","tip-2026-08-25-doctor-fix"], ...}`

### Step 8: Commit and push all remaining changes
```bash
cd orbit/orbit-cli
git add .
git commit -m "chore: final v0.2.5 wiring and remote verification"
git push origin main
```

### Step 9: Update Edge Worker version to v0.2.5
Update `worker.js` feed payload version to `v0.2.5`, deploy to Cloudflare.

---

## Full Test Run (after all tasks)

```bash
cd orbit/orbit-cli && go test ./... -v 2>&1 | grep -E "^(ok|FAIL|---)"
```
Expected: All `ok`, zero `FAIL`.

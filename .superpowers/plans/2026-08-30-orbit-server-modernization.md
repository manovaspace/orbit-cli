# `orbit-server` Modernization Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Modernize `orbit-server` with a pure-Go SQLite storage engine (`modernc.org/sqlite`), persistent OTP challenge and grant state, hardened reverse proxy rate limiting, and structured `log/slog` / OpenTelemetry HTTP middleware.

**Architecture:** Replace volatile in-memory maps and raw `os.WriteFile` JSON storage with an ACID pure-Go SQLite database (`pkg/serverstore/sqlite`). Wire persistent challenges and grants so state survives server restarts. Add proxy CIDR verification to rate limiting, and wrap handlers with structured logging and OpenTelemetry tracing.

**Tech Stack:** Go 1.26, `modernc.org/sqlite` (Pure Go, 0 CGO), `log/slog`, `go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp`.

## Global Constraints

- Must use pure Go (`modernc.org/sqlite`) without CGO or external gcc compiler requirements.
- Must maintain 100% backward compatibility with existing CLI workflows (`orbit onboard`, `orbit invite`, `orbit admin`).
- Existing legacy JSON files (`~/.config/manova/invites.json` / `~/.config/orbit/invites.json`) must be automatically migrated into SQLite on first run.
- Workstation CLI commands must remain pure API clients and never hold direct server secrets.

---

### Task 1: Add pure-Go SQLite dependency, define `serverstore.Store` interfaces, and implement database engine

**Files:**
- Modify: `orbit/orbit-cli/go.mod`
- Create: `orbit/orbit-cli/pkg/serverstore/store.go`
- Create: `orbit/orbit-cli/pkg/serverstore/sqlite/db.go`
- Create: `orbit/orbit-cli/pkg/serverstore/sqlite/schema.go`
- Test: `orbit/orbit-cli/pkg/serverstore/sqlite/db_test.go`

**Interfaces:**
- Produces:
  - `serverstore.Store`: interface combining `InviteStore`, `ChallengeStore`, `GrantStore`, `RateLimitStore`
  - `sqlite.NewDB(path string) (*DB, error)`
  - `sqlite.NewTestDB(t *testing.T) *DB`

- [ ] **Step 1: Write test for SQLite DB connection and schema initialization**

```go
package sqlite_test

import (
	"testing"

	"github.com/manovaspace/orbit-cli/pkg/serverstore/sqlite"
)

func TestNewDB_InMemory(t *testing.T) {
	db, err := sqlite.NewDB(":memory:")
	if err != nil {
		t.Fatalf("failed to create in-memory sqlite db: %v", err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		t.Fatalf("db ping failed: %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -v ./pkg/serverstore/sqlite` in `orbit/orbit-cli`
Expected: FAIL (packages not found)

- [ ] **Step 3: Add `modernc.org/sqlite` to `go.mod` and implement `db.go`, `schema.go`, and `store.go`**

```go
// pkg/serverstore/store.go
package serverstore

import (
	"context"
	"time"

	"github.com/manovaspace/orbit-cli/pkg/invite"
	"github.com/manovaspace/orbit-cli/pkg/owner"
)

type Store interface {
	Invites() InviteStore
	Challenges() ChallengeStore
	Grants() GrantStore
	RateLimits() RateLimitStore
	Close() error
}

type InviteStore interface {
	SaveInvite(ctx context.Context, rec *invite.InviteRecord) error
	GetInvite(ctx context.Context, tokenOrID string) (*invite.InviteRecord, error)
	ListInvites(ctx context.Context) ([]*invite.InviteRecord, error)
	RevokeInvite(ctx context.Context, tokenOrID string) (*invite.InviteRecord, error)
}

type ChallengeStore interface {
	SaveChallenge(ctx context.Context, ch *owner.Challenge) error
	GetActiveChallenge(ctx context.Context, email string) (*owner.Challenge, error)
	IncrementAttempts(ctx context.Context, id string) (int, error)
	MarkVerified(ctx context.Context, id string) error
	PruneExpired(ctx context.Context) error
}

type GrantStore interface {
	SaveGrant(ctx context.Context, g *owner.AdminGrant) error
	GetGrant(ctx context.Context, email, codeHash string) (*owner.AdminGrant, error)
	MarkUsed(ctx context.Context, id string) error
	ListActiveGrants(ctx context.Context) ([]*owner.AdminGrant, error)
}

type RateLimitStore interface {
	RecordEvent(ctx context.Context, key, route string, ts time.Time) error
	CountEventsSince(ctx context.Context, key, route string, since time.Time) (int, error)
	PruneEventsOlderThan(ctx context.Context, cutoff time.Time) error
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -v ./pkg/serverstore/sqlite`
Expected: PASS

---

### Task 2: Implement SQLite Invites Repository with Legacy JSON Auto-Migration

**Files:**
- Create: `orbit/orbit-cli/pkg/serverstore/sqlite/invites.go`
- Create: `orbit/orbit-cli/pkg/serverstore/migrate_json.go`
- Test: `orbit/orbit-cli/pkg/serverstore/sqlite/invites_test.go`
- Test: `orbit/orbit-cli/pkg/serverstore/migrate_json_test.go`

**Interfaces:**
- Produces:
  - `(db *DB) Invites() serverstore.InviteStore`
  - `serverstore.MigrateLegacyJSON(ctx context.Context, legacyPath string, target serverstore.InviteStore) (int, error)`

- [ ] **Step 1: Write tests for Invite CRUD and legacy JSON migration**

```go
func TestInvites_CRUD(t *testing.T) {
	db := sqlite.NewTestDB(t)
	store := db.Invites()
	ctx := context.Background()

	rec := &invite.InviteRecord{
		ID:          "inv_test123",
		Token:       "tok_secret456",
		Email:       "dev@manova.space",
		Scope:       "orbit",
		CreatedAt:   time.Now().UTC().Truncate(time.Second),
		ExpiresAt:   time.Now().UTC().Add(24 * time.Hour).Truncate(time.Second),
	}

	if err := store.SaveInvite(ctx, rec); err != nil {
		t.Fatalf("SaveInvite failed: %v", err)
	}

	fetched, err := store.GetInvite(ctx, "inv_test123")
	if err != nil || fetched == nil || fetched.Email != "dev@manova.space" {
		t.Fatalf("GetInvite by ID failed: %v, fetched: %+v", err, fetched)
	}

	fetchedByPrefix, err := store.GetInvite(ctx, "inv_te")
	if err != nil || fetchedByPrefix == nil {
		t.Fatalf("GetInvite by prefix failed: %v", err)
	}

	revoked, err := store.RevokeInvite(ctx, "inv_test123")
	if err != nil || !revoked.Revoked {
		t.Fatalf("RevokeInvite failed: %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -v ./pkg/serverstore/...`
Expected: FAIL

- [ ] **Step 3: Implement `invites.go` and `migrate_json.go`**

Implement parameterized queries, prefix matching (`WHERE id LIKE ? || '%'`), token lookup, and JSON file parsing for legacy migration.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -v ./pkg/serverstore/...`
Expected: PASS

---

### Task 3: Implement SQLite Challenge and Admin Grant Repositories & Wire to `pkg/owner`

**Files:**
- Create: `orbit/orbit-cli/pkg/serverstore/sqlite/challenges.go`
- Create: `orbit/orbit-cli/pkg/serverstore/sqlite/grants.go`
- Test: `orbit/orbit-cli/pkg/serverstore/sqlite/challenges_test.go`
- Test: `orbit/orbit-cli/pkg/serverstore/sqlite/grants_test.go`
- Modify: `orbit/orbit-cli/pkg/owner/challenge.go`
- Modify: `orbit/orbit-cli/pkg/owner/grants.go`

**Interfaces:**
- Consumes: `owner.Challenge`, `owner.AdminGrant`
- Produces:
  - `(db *DB) Challenges() serverstore.ChallengeStore`
  - `(db *DB) Grants() serverstore.GrantStore`
  - `owner.NewPersistentChallengeManager(store serverstore.ChallengeStore)`

- [ ] **Step 1: Write tests for Challenge verification, attempt counting, and lockout**

```go
func TestChallenges_Lifecycle(t *testing.T) {
	db := sqlite.NewTestDB(t)
	store := db.Challenges()
	ctx := context.Background()

	ch := &owner.Challenge{
		ID:        "ch_123",
		Email:     "admin@manova.space",
		Salt:      "testsalt",
		OTPHash:   owner.HashOTP("123456", "testsalt"),
		ExpiresAt: time.Now().UTC().Add(10 * time.Minute),
	}

	if err := store.SaveChallenge(ctx, ch); err != nil {
		t.Fatalf("SaveChallenge failed: %v", err)
	}

	attempts, err := store.IncrementAttempts(ctx, "ch_123")
	if err != nil || attempts != 1 {
		t.Fatalf("IncrementAttempts failed: %v, attempts=%d", err, attempts)
	}

	if err := store.MarkVerified(ctx, "ch_123"); err != nil {
		t.Fatalf("MarkVerified failed: %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -v ./pkg/serverstore/sqlite`
Expected: FAIL

- [ ] **Step 3: Implement `challenges.go`, `grants.go`, and update `pkg/owner` managers to use the store**

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -v ./pkg/serverstore/sqlite ./pkg/owner`
Expected: PASS

---

### Task 4: Implement Hardened Reverse Proxy Validation & Multi-Layer Rate Limiting

**Files:**
- Create: `orbit/orbit-cli/pkg/onboard/ratelimit/limiter.go`
- Test: `orbit/orbit-cli/pkg/onboard/ratelimit/limiter_test.go`
- Modify: `orbit/orbit-cli/pkg/onboard/server.go`

**Interfaces:**
- Produces:
  - `ratelimit.NewLimiter(store serverstore.RateLimitStore, opts LimiterOptions)`
  - `(l *Limiter) AllowIP(r *http.Request) (bool, time.Duration)`
  - `(l *Limiter) AllowEmail(email, route string) (bool, time.Duration)`

- [ ] **Step 1: Write tests for trusted proxy IP extraction, spoofing prevention, and email rate limiting**

```go
func TestLimiter_ProxySpoofing(t *testing.T) {
	limiter := ratelimit.NewLimiter(mockStore, ratelimit.LimiterOptions{
		TrustedProxies: []string{"127.0.0.1/32"},
		LimitPerMin:    10,
	})

	// Untrusted direct connection sending spoofed header
	req := httptest.NewRequest("POST", "/claim", nil)
	req.RemoteAddr = "198.51.100.1:54321" // Public IP
	req.Header.Set("X-Forwarded-For", "127.0.0.1")

	ip := limiter.ExtractClientIP(req)
	if ip != "198.51.100.1" {
		t.Fatalf("expected remote addr to be used when untrusted proxy, got: %s", ip)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -v ./pkg/onboard/ratelimit`
Expected: FAIL

- [ ] **Step 3: Implement `limiter.go` with CIDR parsing and wire into `pkg/onboard/server.go`**

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -v ./pkg/onboard/ratelimit ./pkg/onboard`
Expected: PASS

---

### Task 5: Implement Structured `log/slog` & OpenTelemetry HTTP Middleware

**Files:**
- Create: `orbit/orbit-cli/pkg/onboard/middleware/requestid.go`
- Create: `orbit/orbit-cli/pkg/onboard/middleware/logging.go`
- Create: `orbit/orbit-cli/pkg/onboard/middleware/recovery.go`
- Test: `orbit/orbit-cli/pkg/onboard/middleware/middleware_test.go`
- Modify: `orbit/orbit-cli/pkg/onboard/server.go`

**Interfaces:**
- Produces:
  - `middleware.RequestID(next http.Handler) http.Handler`
  - `middleware.Logging(logger *slog.Logger) func(http.Handler) http.Handler`
  - `middleware.Recovery(logger *slog.Logger) func(http.Handler) http.Handler`

- [ ] **Step 1: Write test for RequestID injection, status code capture, and JSON slog logging**

```go
func TestMiddleware_RequestIDAndLogging(t *testing.T) {
	var logBuf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logBuf, nil))

	handler := middleware.RequestID(middleware.Logging(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})))

	req := httptest.NewRequest("GET", "/healthz", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Header().Get("X-Request-ID") == "" {
		t.Fatal("expected X-Request-ID header to be set")
	}

	var logEntry map[string]interface{}
	if err := json.Unmarshal(logBuf.Bytes(), &logEntry); err != nil {
		t.Fatalf("failed to parse slog json: %v", err)
	}
	if logEntry["path"] != "/healthz" || logEntry["status"] != float64(200) {
		t.Fatalf("unexpected log entry: %+v", logEntry)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -v ./pkg/onboard/middleware`
Expected: FAIL

- [ ] **Step 3: Implement middleware files and wire into `Server.Handler()`**

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -v ./pkg/onboard/middleware ./pkg/onboard`
Expected: PASS

---

### Task 6: Wire `cmd/orbit-server/main.go` with SQLite, Atomic Vault Writes, and End-to-End Tests

**Files:**
- Modify: `orbit/orbit-cli/cmd/orbit-server/main.go`
- Modify: `orbit/orbit-cli/pkg/owner/store.go` (atomic write via temp file and rename)
- Test: `orbit/orbit-cli/cmd/orbit-server/main_test.go`
- Test: `orbit/orbit-cli/pkg/onboard/server_test.go`

- [ ] **Step 1: Write end-to-end integration test for `orbit-server` lifecycle (DB initialization, legacy import, claim, challenge verify, and server shutdown)**

```go
func TestServer_E2E_SQLiteLifecycle(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "orbit.db")
	db, err := sqlite.NewDB(dbPath)
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
	}
	defer db.Close()

	secret := []byte("orbit-test-secret-key-32bytes-long")
	srv, err := onboard.NewServer(onboard.ServerConfig{
		Addr:        ":0",
		Secret:      secret,
		Store:       db,
		Provisioner: provisioner.NewDevProvisioner(),
	})
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	// 1. Create invite in DB
	tok, _ := invite.GenerateToken("dev@manova.space", "orbit", 24*time.Hour, secret)
	db.Invites().SaveInvite(context.Background(), &invite.InviteRecord{
		ID:        "inv_e2e",
		Token:     tok,
		Email:     "dev@manova.space",
		Scope:     "orbit",
		CreatedAt: time.Now().UTC(),
		ExpiresAt: time.Now().UTC().Add(24 * time.Hour),
	})

	// 2. Perform HTTP claim
	claimBody, _ := json.Marshal(provisioner.ClaimRequest{
		InviteToken: tok,
		DesiredUID:  "dev",
	})
	req := httptest.NewRequest("POST", "/v1/onboard/claim", bytes.NewReader(claimBody))
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected claim 200 OK, got %d: %s", rec.Code, rec.Body.String())
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -v ./cmd/orbit-server ./pkg/onboard`
Expected: FAIL

- [ ] **Step 3: Update `cmd/orbit-server/main.go` and `pkg/owner/store.go` with atomic file writes and SQLite integration**

- [ ] **Step 4: Run full test suite across the repository**

Run: `go test ./...` in `orbit/orbit-cli`
Expected: ALL PASS

- [ ] **Step 5: Verify build artifacts**

Run:
```bash
go build -o bin/orbit ./cmd/orbit
go build -o bin/orbit-server ./cmd/orbit-server
```
Expected: Clean build with zero warnings or errors.

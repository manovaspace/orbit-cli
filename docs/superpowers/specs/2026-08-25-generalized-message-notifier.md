# Generalized Message & Notifier System — Design Spec

**Goal:** Replace the narrow version-only edge poller with a unified `/api/feed` message feed that delivers typed, prioritized messages (releases, tips, broadcasts, alerts) to the CLI, tracked client-side with simple atomic file I/O.

**Architecture:** The Cloudflare Worker exposes `GET /api/feed` returning a JSON envelope containing the current version plus an ordered `messages[]` array. The Go CLI worker polls this feed, persists messages to `~/.manova/feed.json` (atomic rename), and on each command execution the `PersistentPostRun` hook reads the local feed and renders any unseen, non-expired messages. Client-side dismissal is tracked in `~/.manova/messages.json` via the same atomic-write pattern already used for edge state.

**Tech Stack:** Go 1.23+, Cloudflare Workers JS, `os.Rename` atomic writes, `encoding/json`.

---

## 1. Edge Protocol — `/api/feed`

### Endpoint
`GET https://get.manova.space/api/feed`

### Response Schema
```json
{
  "version": "v0.2.4",
  "published_at": "2026-08-24T21:23:48Z",
  "status": "operational",
  "messages": [
    {
      "id": "release-v0.2.4",
      "type": "release",
      "priority": "high",
      "title": "v0.2.4 available — Mandatory Zsh + OMZ enforcement",
      "body": "Zsh and Oh My Zsh are now hard requirements. Run 'manova doctor --fix' to auto-install.",
      "action": "manova self-update",
      "published_at": "2026-08-24T21:23:48Z",
      "expires_at": null
    }
  ]
}
```

### Message Types
| Type | Icon | Description |
|---|---|---|
| `release` | 🔔 | New CLI version available |
| `broadcast` | 📢 | Staff/admin announcement |
| `tip` | 💡 | Usage hint or workflow suggestion |
| `alert` | ⚠ | Urgent operational notice |

### Priority Levels
| Priority | Behaviour |
|---|---|
| `critical` | Always shown every run, never suppressible |
| `high` | Shown once per message ID |
| `normal` | Shown once per message ID |
| `low` | Shown once, silenced after seen |

### Legacy Compat
`/version` and `/api/version` kept for backward compat, returning the old schema.

---

## 2. Go Types — `pkg/notifier`

```go
type FeedResponse struct {
    Version     string    `json:"version"`
    PublishedAt time.Time `json:"published_at"`
    Status      string    `json:"status"`
    Messages    []Message `json:"messages"`
}

type Message struct {
    ID          string     `json:"id"`
    Type        string     `json:"type"`
    Priority    string     `json:"priority"`
    Title       string     `json:"title"`
    Body        string     `json:"body,omitempty"`
    Action      string     `json:"action,omitempty"`
    PublishedAt time.Time  `json:"published_at"`
    ExpiresAt   *time.Time `json:"expires_at,omitempty"`
}

type MessageStore struct {
    Seen      []string  `json:"seen"`
    UpdatedAt time.Time `json:"updated_at"`
}

type FeedState struct {
    LatestVersion string    `json:"latest_version"`
    LastCheckedAt time.Time `json:"last_checked_at"`
    ServerStatus  string    `json:"server_status"`
    LastError     string    `json:"last_error,omitempty"`
    Messages      []Message `json:"messages"`
}
```

---

## 3. Client Tracking — `~/.manova/messages.json`

- Atomic `os.Rename(tmpPath, dst)` — sufficient, no lockfile needed
- `critical` messages always shown; all others: shown once (by ID), then suppressed
- Expired messages dropped client-side
- Max 3 messages rendered per run

---

## 4. Display in `PersistentPostRun`

```
╭──────────────────────────────────────────────────────────╮
│ 🔔 v0.2.4 available — Mandatory Zsh + OMZ enforcement   │
│                                                          │
│   Zsh and Oh My Zsh are now hard requirements.           │
│   → manova self-update                                   │
╰──────────────────────────────────────────────────────────╯
```

---

## 5. Bug Fixes Bundled

| # | Bug | Fix |
|---|---|---|
| 1 | `manova doctor` exits code 1 on errors | Return `nil`; print summary only |
| 2 | Worker never auto-started on fresh install | PersistentPostRun bootstraps on first run |

---

## 6. Files Touched

| File | Change |
|---|---|
| `clients/manova/manova-infra/static/worker.js` | Add `/api/feed` route |
| `orbit/orbit-cli/pkg/notifier/types.go` | New: FeedResponse, Message, FeedState, MessageStore |
| `orbit/orbit-cli/pkg/notifier/feed.go` | FetchFeed(), PollFeed() |
| `orbit/orbit-cli/pkg/notifier/store.go` | ReadStore(), WriteStoreAtomic(), MarkSeen(), FilterVisible() |
| `orbit/orbit-cli/pkg/notifier/feed_test.go` | Unit tests |
| `orbit/orbit-cli/pkg/notifier/store_test.go` | Unit tests |
| `orbit/orbit-cli/pkg/worker/poller.go` | Shim to notifier |
| `orbit/orbit-cli/cmd/manova/main.go` | Replace banner with message renderer |
| `orbit/orbit-cli/cmd/manova/worker.go` | Wire run-once to notifier |
| `orbit/orbit-cli/cmd/manova/doctor.go` | Fix exit-code bug |

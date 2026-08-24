package worker

import (
	"sync"
	"time"
)

var (
	healMu      sync.Mutex
	healingBusy bool
)

// CheckWatchdog inspects the local edge version state file.
// If the file is missing, unreadable, or older than staleThreshold (defaults to DefaultStaleThreshold),
// it flags needsHealing = true so the CLI can trigger a background heal.
func CheckWatchdog(statePath string, staleThreshold time.Duration) (needsHealing bool, state *EdgeVersionState, err error) {
	if statePath == "" {
		statePath = DefaultStateFile
	}
	if staleThreshold <= 0 {
		staleThreshold = DefaultStaleThreshold
	}

	st, err := ReadState(statePath)
	if err != nil || st == nil {
		return true, nil, nil
	}

	if st.LastCheckedAt.IsZero() || time.Since(st.LastCheckedAt) > staleThreshold {
		return true, st, nil
	}

	return false, st, nil
}

// HealWorker synchronously attempts to restart the daemon and performs an immediate edge poll.
func HealWorker(execPath, endpoint, statePath string) error {
	if endpoint == "" {
		endpoint = DefaultEdgeURL
	}
	if statePath == "" {
		statePath = DefaultStateFile
	}

	// Attempt daemon startup / restart
	_, _ = StartDaemon(execPath)

	// Immediately record fresh state
	_, err := PollOnce(endpoint, statePath)
	return err
}

// HealWorkerBackground executes worker healing asynchronously with single-flight deduplication.
func HealWorkerBackground(execPath string) {
	healMu.Lock()
	if healingBusy {
		healMu.Unlock()
		return
	}
	healingBusy = true
	healMu.Unlock()

	go func() {
		defer func() {
			healMu.Lock()
			healingBusy = false
			healMu.Unlock()
		}()
		_ = HealWorker(execPath, DefaultEdgeURL, DefaultStateFile)
	}()
}

package domain

import "fmt"

// Free-space thresholds S0 recorded for the diagnostics: below these values the
// workspace cannot run the dev stack, Docker builds or the Go toolchain safely.
const (
	// MinFreeDiskBytes is the warn threshold for every inspected volume.
	MinFreeDiskBytes uint64 = 10 * 1000 * 1000 * 1000
	// MinFreeMemoryBytes is the warn threshold for MemAvailable.
	MinFreeMemoryBytes uint64 = 1 * 1000 * 1000 * 1000
)

// EvaluateResources turns free disk and memory into diagnostic results. An
// empty report means the values could not be read and nothing is claimed.
func EvaluateResources(resources Resources) []DiagnosticResult {
	var results []DiagnosticResult
	for _, disk := range resources.Disks {
		result := DiagnosticResult{
			Category: "System",
			Name:     "Free disk (" + disk.Path + ")",
			Status:   StatusOK,
			Message:  formatBytes(disk.FreeBytes) + " free",
		}
		if disk.FreeBytes < MinFreeDiskBytes {
			result.Status = StatusWarning
			result.Message = fmt.Sprintf("%s free, below the %s minimum", formatBytes(disk.FreeBytes), formatBytes(MinFreeDiskBytes))
			result.FixSuggestion = "Free disk space: prune Docker images, `go clean -modcache`, or remove stale worktrees."
		}
		results = append(results, result)
	}
	if resources.MemAvailableBytes > 0 {
		result := DiagnosticResult{
			Category: "System",
			Name:     "Free memory",
			Status:   StatusOK,
			Message:  formatBytes(resources.MemAvailableBytes) + " available",
		}
		if resources.MemAvailableBytes < MinFreeMemoryBytes {
			result.Status = StatusWarning
			result.Message = fmt.Sprintf("%s available, below the %s minimum", formatBytes(resources.MemAvailableBytes), formatBytes(MinFreeMemoryBytes))
			result.FixSuggestion = "Stop the dev stack (`o dev down`) or close memory-heavy processes."
		}
		results = append(results, result)
	}
	return results
}

func formatBytes(bytes uint64) string {
	const (
		megabyte = 1000 * 1000
		gigabyte = 1000 * megabyte
		terabyte = 1000 * gigabyte
	)
	switch {
	case bytes >= terabyte:
		return fmt.Sprintf("%.1f TB", float64(bytes)/float64(terabyte))
	case bytes >= gigabyte:
		return fmt.Sprintf("%.1f GB", float64(bytes)/float64(gigabyte))
	case bytes >= megabyte:
		return fmt.Sprintf("%.0f MB", float64(bytes)/float64(megabyte))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}

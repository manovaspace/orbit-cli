// Package domain holds the uninstall result types.
package domain

// FileInfo is the subset of os.FileInfo the uninstall use cases need.
type FileInfo struct {
	IsDir bool
}

// Result is what `orbit uninstall` produced.
type Result struct {
	RemovedBinaries    []string `json:"removedBinaries"`
	RemovedCompletions []string `json:"removedCompletions"`
	RemovedManPages    []string `json:"removedManPages"`
	PurgedState        []string `json:"purgedState"`
	NoBinaries         bool     `json:"noBinaries"`
}

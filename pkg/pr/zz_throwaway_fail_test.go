package pr

import "testing"

// Throwaway: proves the merge gate refuses a PR with a red check. Never merge.
func TestThrowawayRedForMergeGate(t *testing.T) {
	t.Fatal("deliberate failure for the merge gate check")
}

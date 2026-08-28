package assets

import (
	"context"
	"fmt"
	"path/filepath"
)

func PullRepos(ctx context.Context, workspaceRoot string, relPaths []string, store Store, opts PullOptions) error {
	var first error
	for _, rel := range relPaths {
		root := filepath.Join(workspaceRoot, rel)
		if !HasManifest(root) {
			continue
		}
		if err := Pull(ctx, root, store, opts); err != nil {
			if first == nil {
				first = fmt.Errorf("%s: %w", rel, err)
			}
		}
	}
	return first
}

func PushRepos(ctx context.Context, workspaceRoot string, relPaths []string, store Store) error {
	var first error
	for _, rel := range relPaths {
		root := filepath.Join(workspaceRoot, rel)
		if !HasManifest(root) {
			continue
		}
		if err := Push(ctx, root, store); err != nil {
			if first == nil {
				first = fmt.Errorf("%s: %w", rel, err)
			}
		}
	}
	return first
}

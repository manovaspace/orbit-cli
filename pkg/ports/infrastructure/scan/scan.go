// Package scan walks a workspace for the files `o port check` reads and maps
// each file to its repository.
package scan

import (
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/manovaspace/orbit-cli/pkg/manifest"
	"github.com/manovaspace/orbit-cli/pkg/ports/application"
	"github.com/manovaspace/orbit-cli/pkg/ports/domain"
)

// Roots are the workspace directories walked, relative to the root.
var Roots = []string{"orbit", "clients", "manovaspace", "handbook"}

// skipDirs are never descended into.
var skipDirs = map[string]bool{
	"node_modules": true, ".git": true, "dist": true, "build": true,
	".next": true, "vendor": true, "testdata": true,
}

// Source is the disk-backed application.Source.
type Source struct{}

// New builds a Source.
func New() *Source { return &Source{} }

// Files returns every scannable file under the workspace roots that exist.
// Repository ownership comes from workspace.yaml when the root has one, else
// from the layout (orbit/<repo>, manovaspace/<repo>, clients/<group>/<repo>,
// handbook).
func (s *Source) Files(root string) ([]application.SourceFile, error) {
	owner := repoOwner(root)
	var out []application.SourceFile
	for _, top := range Roots {
		realDir, err := filepath.EvalSymlinks(filepath.Join(root, top))
		if err != nil {
			continue
		}
		info, err := os.Stat(realDir)
		if err != nil || !info.IsDir() {
			continue
		}
		err = filepath.WalkDir(realDir, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				if p != realDir && skipDirs[d.Name()] {
					return filepath.SkipDir
				}
				return nil
			}
			if !d.Type().IsRegular() {
				return nil
			}
			inner, err := filepath.Rel(realDir, p)
			if err != nil {
				return nil
			}
			rel := path.Join(top, filepath.ToSlash(inner))
			if !domain.Scannable(rel) {
				return nil
			}
			out = append(out, application.SourceFile{Rel: rel, Repo: owner(rel)})
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Rel < out[j].Rel })
	return out, nil
}

// Read returns the content of root/rel.
func (s *Source) Read(root, rel string) (string, error) {
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// repoOwner returns a function that maps a workspace-relative path to its
// repository path. With a manifest the longest matching manifest path wins.
func repoOwner(root string) func(rel string) string {
	var repos []string
	if loaded, err := manifest.Load(filepath.Join(root, "workspace.yaml")); err == nil {
		for _, target := range loaded.AllRepos() {
			repos = append(repos, strings.Trim(filepath.ToSlash(target.Path), "/"))
		}
	}
	return func(rel string) string {
		if len(repos) > 0 {
			best := ""
			for _, r := range repos {
				if (rel == r || strings.HasPrefix(rel, r+"/")) && len(r) > len(best) {
					best = r
				}
			}
			if best != "" {
				return best
			}
		}
		return layoutRepo(rel)
	}
}

// layoutRepo derives the repository from the directory depth each root uses.
func layoutRepo(rel string) string {
	parts := strings.Split(rel, "/")
	switch parts[0] {
	case "handbook":
		return "handbook"
	case "orbit", "manovaspace":
		if len(parts) >= 3 {
			return parts[0] + "/" + parts[1]
		}
	case "clients":
		if len(parts) >= 4 {
			return parts[0] + "/" + parts[1] + "/" + parts[2]
		}
	}
	return ""
}

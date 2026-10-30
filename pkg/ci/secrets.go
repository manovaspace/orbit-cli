package ci

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/manovaspace/orbit-cli/pkg/manifest"
)

// DeployKeySecret is the repository secret that carries a per-repo deploy key.
const DeployKeySecret = "DEPLOY_SSH_KEY"

// SyncSecrets sets every secret listed in repo.CISecrets. Values come from
// source, except DEPLOY_SSH_KEY which is read from keysDir/<repo name>. All
// values are validated before anything is set, so a failure leaves the
// repository untouched. Values are written to stdin, never argv.
func SyncSecrets(repo manifest.RepoTarget, source map[string]string, keysDir string, run ExecStdin) error {
	slug, ok := githubSlug(repo.RemoteURL)
	if !ok {
		return fmt.Errorf("repository %s has no GitHub remote", repo.Name)
	}

	values := make(map[string]string, len(repo.CISecrets))
	for _, name := range repo.CISecrets {
		var value string
		if name == DeployKeySecret {
			key, err := os.ReadFile(filepath.Join(keysDir, repo.Name))
			if err != nil {
				return fmt.Errorf("secret %s for %s: %w", name, repo.Name, err)
			}
			value = string(key)
		} else {
			var found bool
			value, found = source[name]
			if !found || value == "" {
				return fmt.Errorf("secret %s for %s has no value", name, repo.Name)
			}
		}
		values[name] = value
	}

	for _, name := range repo.CISecrets {
		if _, err := run(values[name], "gh", "secret", "set", name, "-R", slug); err != nil {
			return fmt.Errorf("set secret %s for %s: %w", name, slug, err)
		}
	}
	return nil
}

// LoadSource reads a KEY=value file. Blank lines and lines starting with # are
// ignored. A file readable by group or others is refused.
func LoadSource(path string) (map[string]string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("%s must not be readable by group or others (chmod 600)", path)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	source := map[string]string{}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name, value, ok := strings.Cut(line, "=")
		if !ok {
			return nil, fmt.Errorf("%s: line %q is not KEY=value", path, line)
		}
		source[strings.TrimSpace(name)] = strings.TrimSpace(value)
	}
	return source, nil
}

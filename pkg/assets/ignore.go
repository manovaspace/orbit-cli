package assets

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func EnsureGitignore(repoRoot, relPath string) error {
	relPath = filepath.ToSlash(relPath)
	gi := filepath.Join(repoRoot, ".gitignore")
	existing := map[string]struct{}{}

	if data, err := os.ReadFile(gi); err == nil {
		sc := bufio.NewScanner(strings.NewReader(string(data)))
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line != "" && !strings.HasPrefix(line, "#") {
				existing[line] = struct{}{}
			}
		}
		if _, ok := existing[relPath]; ok {
			return nil
		}
		f, err := os.OpenFile(gi, os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			return fmt.Errorf("open .gitignore: %w", err)
		}
		defer f.Close()
		if !strings.HasSuffix(string(data), "\n") && len(data) > 0 {
			if _, err := f.WriteString("\n"); err != nil {
				return err
			}
		}
		_, err = fmt.Fprintf(f, "%s\n", relPath)
		return err
	}

	body := "# orbit-assets — synced by `orbit assets`, not git\n" + relPath + "\n"
	return os.WriteFile(gi, []byte(body), 0644)
}

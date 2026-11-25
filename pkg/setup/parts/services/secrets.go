package services

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io/fs"
	"math/big"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/manovaspace/orbit-cli/pkg/clierr"
)

// passwordAlphabet is the character set of generated passwords: letters and digits only, so a
// password never needs quoting in SQL or in an env file.
const passwordAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"

// passwordLength is the length of every generated role password.
const passwordLength = 32

// passwordKey is the local.env key that holds the password of role.
func passwordKey(role string) string {
	return "PG_" + strings.ToUpper(role) + "_PASSWORD"
}

// generatePassword returns passwordLength characters drawn uniformly from passwordAlphabet.
func generatePassword() (string, error) {
	out := make([]byte, passwordLength)
	size := big.NewInt(int64(len(passwordAlphabet)))
	for i := range out {
		n, err := rand.Int(rand.Reader, size)
		if err != nil {
			return "", clierr.Wrap(err, clierr.Failed("cannot generate a password"))
		}
		out[i] = passwordAlphabet[n.Int64()]
	}
	return string(out), nil
}

// LocalSecretsPath returns the default local secrets file path, $XDG_CONFIG_HOME/orbit/local.env
// or ~/.config/orbit/local.env.
func LocalSecretsPath() (string, error) {
	return orbitPath("XDG_CONFIG_HOME", ".config", "local.env")
}

// LocalSecrets reads the KEY=value lines of the local secrets file at path. Blank lines and lines
// starting with # are skipped. A missing file yields an empty map. A line without "=" is a usage
// error that names the line number, never its content.
func LocalSecrets(path string) (map[string]string, error) {
	out := map[string]string{}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return out, nil
	}
	if err != nil {
		return nil, clierr.Wrap(err, clierr.Failed(fmt.Sprintf("cannot read %s", path)))
	}
	for i, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return nil, clierr.Usage(fmt.Sprintf("%s line %d is not KEY=value", path, i+1))
		}
		out[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}
	return out, nil
}

// SaveLocalSecrets writes values to path atomically: the directory is created 0700, the file is
// 0600 and written through a temporary file that is then renamed over path. Keys and values must
// be single-line; a key containing "=" or whitespace is refused.
func SaveLocalSecrets(path string, values map[string]string) error {
	keys := make([]string, 0, len(values))
	for k, v := range values {
		if k == "" || strings.ContainsAny(k, "=\n\r \t") {
			return clierr.Usage(fmt.Sprintf("secret key %q is not a valid name", k))
		}
		if strings.ContainsAny(v, "\n\r") {
			return clierr.Usage(fmt.Sprintf("secret %s must be a single line", k))
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		b.WriteString(k + "=" + values[k] + "\n")
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return clierr.Wrap(err, clierr.Failed(fmt.Sprintf("cannot create %s", dir)))
	}
	tmp, err := os.CreateTemp(dir, ".local-*.env")
	if err != nil {
		return clierr.Wrap(err, clierr.Failed(fmt.Sprintf("cannot create temp file in %s", dir)))
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return clierr.Wrap(err, clierr.Failed("cannot set secrets file mode"))
	}
	if _, err := tmp.WriteString(b.String()); err != nil {
		_ = tmp.Close()
		return clierr.Wrap(err, clierr.Failed("cannot write secrets file"))
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return clierr.Wrap(err, clierr.Failed("cannot sync secrets file"))
	}
	if err := tmp.Close(); err != nil {
		return clierr.Wrap(err, clierr.Failed("cannot close secrets file"))
	}
	if err := os.Rename(tmpName, path); err != nil {
		return clierr.Wrap(err, clierr.Failed(fmt.Sprintf("cannot replace %s", path)))
	}
	return nil
}

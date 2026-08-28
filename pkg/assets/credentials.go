package assets

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type R2Config struct {
	AccountID       string
	AccessKeyID     string
	SecretAccessKey string
	Bucket          string
	Endpoint        string
}

func DefaultR2EnvPath() string {
	if p := strings.TrimSpace(os.Getenv("ORBIT_R2_ENV")); p != "" {
		return p
	}
	configDir := os.Getenv("XDG_CONFIG_HOME")
	if configDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return filepath.Join(".config", "orbit", "r2.env")
		}
		configDir = filepath.Join(home, ".config")
	}
	return filepath.Join(configDir, "orbit", "r2.env")
}

func LoadR2Env(path string) (R2Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return R2Config{}, err
	}
	defer f.Close()

	vals := map[string]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		vals[strings.TrimSpace(key)] = strings.TrimSpace(strings.Trim(val, `"'`))
	}
	if err := sc.Err(); err != nil {
		return R2Config{}, err
	}

	cfg := R2Config{
		AccountID:       vals["R2_ACCOUNT_ID"],
		AccessKeyID:     vals["R2_ACCESS_KEY_ID"],
		SecretAccessKey: vals["R2_SECRET_ACCESS_KEY"],
		Bucket:          vals["R2_BUCKET"],
		Endpoint:        vals["R2_ENDPOINT"],
	}
	if cfg.Bucket == "" {
		cfg.Bucket = "manova-assets"
	}
	if cfg.AccountID == "" || cfg.AccessKeyID == "" || cfg.SecretAccessKey == "" {
		return R2Config{}, fmt.Errorf("%s: R2_ACCOUNT_ID, R2_ACCESS_KEY_ID, and R2_SECRET_ACCESS_KEY are required", path)
	}
	if cfg.Endpoint == "" {
		cfg.Endpoint = fmt.Sprintf("https://%s.r2.cloudflarestorage.com", cfg.AccountID)
	}
	return cfg, nil
}

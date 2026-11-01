package application

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"strconv"
	"strings"
	"time"

	"github.com/manovaspace/orbit-cli/pkg/clierr"
	"github.com/manovaspace/orbit-cli/pkg/config/domain"
	"gopkg.in/yaml.v3"
)

// Show returns the resolved configuration with secrets masked.
func (s *Service) Show(path string) (*domain.Config, error) {
	res, err := s.Resolve(ResolveOptions{ConfigPath: path})
	if err != nil {
		return nil, clierr.Wrap(err, clierr.Failed("failed to resolve configuration"))
	}
	return res.Config.Masked(), nil
}

// Get returns one resolved entry.
func (s *Service) Get(path, key string) (domain.ConfigEntry, error) {
	res, err := s.Resolve(ResolveOptions{ConfigPath: path})
	if err != nil {
		return domain.ConfigEntry{}, clierr.Wrap(err, clierr.Failed("failed to resolve configuration"))
	}
	entry, err := res.Config.Get(key)
	if err != nil {
		return domain.ConfigEntry{}, clierr.Usage(err.Error())
	}
	return entry, nil
}

// List returns every resolved entry and its source.
func (s *Service) List(path string) ([]domain.ConfigEntry, error) {
	res, err := s.Resolve(ResolveOptions{ConfigPath: path})
	if err != nil {
		return nil, clierr.Wrap(err, clierr.Failed("failed to resolve configuration"))
	}
	return res.ListEntries(), nil
}

// Set validates, persists and reports one key-value pair.
func (s *Service) Set(path, key, value string) (SetResult, error) {
	cfg, err := s.loadOrDefault(path)
	if err != nil {
		return SetResult{}, clierr.Wrap(err, clierr.Failed("failed to load configuration"))
	}
	if err := cfg.Set(key, value); err != nil {
		return SetResult{}, clierr.Usage(err.Error())
	}

	result := SetResult{
		Key:           key,
		Value:         value,
		Path:          path,
		SecretWarning: domain.IsSecretKey(key),
	}
	if result.SecretWarning {
		fmt.Fprintf(s.out(), "%s Warning: '%s' resembles a secret. Storing secrets in config.yaml is discouraged.\n", s.UI.Warn, key)
	}
	if err := s.Store.SetInFile(path, key, value); err != nil {
		return result, clierr.Wrap(err, clierr.Failed("failed to persist configuration"))
	}
	fmt.Fprintf(s.out(), "%s Set %s = %s (%s)\n", s.UI.OK, s.bold(key), s.code(value), s.subtle(path))
	return result, nil
}

// Unset resets one key in the file.
func (s *Service) Unset(path, key string) (UnsetResult, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return UnsetResult{}, clierr.Usage("key cannot be empty")
	}

	cfg, err := s.loadOrDefault(path)
	if err != nil {
		return UnsetResult{}, clierr.Wrap(err, clierr.Failed("failed to load configuration"))
	}
	if err := cfg.Unset(key); err != nil {
		return UnsetResult{}, clierr.Usage(err.Error())
	}
	if err := s.Store.UnsetInFile(path, key); err != nil {
		return UnsetResult{}, clierr.Wrap(err, clierr.Failed(fmt.Sprintf("failed to unset %s in %q", key, path)))
	}

	fmt.Fprintf(s.out(), "%s Unset %s (%s)\n", s.UI.OK, s.bold(key), s.subtle(path))
	return UnsetResult{Key: key, Path: path}, nil
}

// Init creates the default configuration file unless it already exists.
func (s *Service) Init(path string, force bool) (InitResult, error) {
	exists, err := s.Store.Exists(path)
	if err != nil {
		return InitResult{}, clierr.Wrap(err, clierr.Failed("failed to inspect configuration file"))
	}
	if exists && !force {
		fmt.Fprintf(s.out(), "%s Configuration file already exists at %s (use --force to overwrite)\n",
			s.UI.Info,
			s.subtle(path),
		)
		return InitResult{Path: path, Existed: true}, nil
	}

	if err := s.Store.Save(path, domain.DefaultConfig()); err != nil {
		return InitResult{}, clierr.Wrap(err, clierr.Failed(fmt.Sprintf("failed to initialize configuration file %q", path)))
	}
	fmt.Fprintf(s.out(), "%s Configuration file initialized at %s (mode 0600)\n", s.UI.OK, s.bold(path))
	return InitResult{Path: path, Created: true}, nil
}

func (s *Service) loadOrDefault(path string) (*domain.Config, error) {
	cfg, err := s.Store.Load(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return domain.DefaultConfig(), nil
		}
		return nil, err
	}
	return cfg, nil
}

// Resolve layers file, environment and flags exactly as the CLI documents.
func (s *Service) Resolve(opts ResolveOptions) (*domain.ResolvedConfig, error) {
	cfg := domain.DefaultConfig()

	// Tier 1: Canonical Defaults
	entriesMap := map[string]*domain.ConfigEntry{
		"server.url":           {Key: "server.url", Value: cfg.Server.URL, Type: "string", Source: domain.SourceDefault, SourceRef: "default"},
		"server.timeout":       {Key: "server.timeout", Value: cfg.Server.Timeout.String(), Type: "duration", Source: domain.SourceDefault, SourceRef: "default"},
		"staff.url":            {Key: "staff.url", Value: cfg.Staff.URL, Type: "string", Source: domain.SourceDefault, SourceRef: "default"},
		"assets.bucket":        {Key: "assets.bucket", Value: cfg.Assets.Bucket, Type: "string", Source: domain.SourceDefault, SourceRef: "default"},
		"assets.endpoint":      {Key: "assets.endpoint", Value: cfg.Assets.Endpoint, Type: "string", Source: domain.SourceDefault, SourceRef: "default"},
		"assets.auto_pull":     {Key: "assets.auto_pull", Value: strconv.FormatBool(cfg.Assets.AutoPull), Type: "bool", Source: domain.SourceDefault, SourceRef: "default"},
		"defaults.scope":       {Key: "defaults.scope", Value: cfg.Defaults.Scope, Type: "string", Source: domain.SourceDefault, SourceRef: "default"},
		"defaults.expiry_days": {Key: "defaults.expiry_days", Value: strconv.Itoa(cfg.Defaults.ExpiryDays), Type: "int", Source: domain.SourceDefault, SourceRef: "default"},
		"ui.color":             {Key: "ui.color", Value: strconv.FormatBool(cfg.UI.Color), Type: "bool", Source: domain.SourceDefault, SourceRef: "default"},
		"ui.output":            {Key: "ui.output", Value: cfg.UI.Output, Type: "string", Source: domain.SourceDefault, SourceRef: "default"},
	}
	var customEntries []domain.ConfigEntry

	// Tier 2: User Config File
	path := opts.ConfigPath
	if path == "" {
		path = s.Store.DefaultPath()
	}

	if data, err := s.Store.Read(path); err == nil {
		var rawMap map[string]interface{}
		_ = yaml.Unmarshal(data, &rawMap)

		var fileCfg domain.Config
		fileCfg.Version = cfg.Version
		fileCfg.Server = cfg.Server
		fileCfg.Staff = cfg.Staff
		fileCfg.Assets = cfg.Assets
		fileCfg.Defaults = cfg.Defaults
		fileCfg.UI = cfg.UI
		fileCfg.Custom = make(map[string]interface{})

		if err := yaml.Unmarshal(data, &fileCfg); err == nil {
			if sMap, ok := rawMap["server"].(map[string]interface{}); ok {
				if _, ok := sMap["url"]; ok {
					cfg.Server.URL = fileCfg.Server.URL
					entriesMap["server.url"] = &domain.ConfigEntry{Key: "server.url", Value: cfg.Server.URL, Type: "string", Source: domain.SourceUserFile, SourceRef: path}
				}
				if _, ok := sMap["timeout"]; ok {
					cfg.Server.Timeout = fileCfg.Server.Timeout
					entriesMap["server.timeout"] = &domain.ConfigEntry{Key: "server.timeout", Value: cfg.Server.Timeout.String(), Type: "duration", Source: domain.SourceUserFile, SourceRef: path}
				}
			}
			if stMap, ok := rawMap["staff"].(map[string]interface{}); ok {
				if _, ok := stMap["url"]; ok {
					cfg.Staff.URL = fileCfg.Staff.URL
					entriesMap["staff.url"] = &domain.ConfigEntry{Key: "staff.url", Value: cfg.Staff.URL, Type: "string", Source: domain.SourceUserFile, SourceRef: path}
				}
			}
			if aMap, ok := rawMap["assets"].(map[string]interface{}); ok {
				if _, ok := aMap["bucket"]; ok {
					cfg.Assets.Bucket = fileCfg.Assets.Bucket
					entriesMap["assets.bucket"] = &domain.ConfigEntry{Key: "assets.bucket", Value: cfg.Assets.Bucket, Type: "string", Source: domain.SourceUserFile, SourceRef: path}
				}
				if _, ok := aMap["endpoint"]; ok {
					cfg.Assets.Endpoint = fileCfg.Assets.Endpoint
					entriesMap["assets.endpoint"] = &domain.ConfigEntry{Key: "assets.endpoint", Value: cfg.Assets.Endpoint, Type: "string", Source: domain.SourceUserFile, SourceRef: path}
				}
				if _, ok := aMap["auto_pull"]; ok {
					cfg.Assets.AutoPull = fileCfg.Assets.AutoPull
					entriesMap["assets.auto_pull"] = &domain.ConfigEntry{Key: "assets.auto_pull", Value: strconv.FormatBool(cfg.Assets.AutoPull), Type: "bool", Source: domain.SourceUserFile, SourceRef: path}
				} else if _, ok := aMap["autopull"]; ok {
					cfg.Assets.AutoPull = fileCfg.Assets.AutoPull
					entriesMap["assets.auto_pull"] = &domain.ConfigEntry{Key: "assets.auto_pull", Value: strconv.FormatBool(cfg.Assets.AutoPull), Type: "bool", Source: domain.SourceUserFile, SourceRef: path}
				}
			}
			if dMap, ok := rawMap["defaults"].(map[string]interface{}); ok {
				if _, ok := dMap["scope"]; ok {
					cfg.Defaults.Scope = fileCfg.Defaults.Scope
					entriesMap["defaults.scope"] = &domain.ConfigEntry{Key: "defaults.scope", Value: cfg.Defaults.Scope, Type: "string", Source: domain.SourceUserFile, SourceRef: path}
				}
				if _, ok := dMap["expiry_days"]; ok {
					cfg.Defaults.ExpiryDays = fileCfg.Defaults.ExpiryDays
					entriesMap["defaults.expiry_days"] = &domain.ConfigEntry{Key: "defaults.expiry_days", Value: strconv.Itoa(cfg.Defaults.ExpiryDays), Type: "int", Source: domain.SourceUserFile, SourceRef: path}
				} else if _, ok := dMap["expirydays"]; ok {
					cfg.Defaults.ExpiryDays = fileCfg.Defaults.ExpiryDays
					entriesMap["defaults.expiry_days"] = &domain.ConfigEntry{Key: "defaults.expiry_days", Value: strconv.Itoa(cfg.Defaults.ExpiryDays), Type: "int", Source: domain.SourceUserFile, SourceRef: path}
				}
			}
			if uMap, ok := rawMap["ui"].(map[string]interface{}); ok {
				if _, ok := uMap["color"]; ok {
					cfg.UI.Color = fileCfg.UI.Color
					entriesMap["ui.color"] = &domain.ConfigEntry{Key: "ui.color", Value: strconv.FormatBool(cfg.UI.Color), Type: "bool", Source: domain.SourceUserFile, SourceRef: path}
				}
				if _, ok := uMap["output"]; ok {
					cfg.UI.Output = fileCfg.UI.Output
					entriesMap["ui.output"] = &domain.ConfigEntry{Key: "ui.output", Value: cfg.UI.Output, Type: "string", Source: domain.SourceUserFile, SourceRef: path}
				}
			}
			if fileCfg.Custom != nil {
				cfg.Custom = fileCfg.Custom
				domain.FlattenCustomMap("custom", cfg.Custom, &customEntries)
				for i := range customEntries {
					customEntries[i].Source = domain.SourceUserFile
					customEntries[i].SourceRef = path
				}
			}
		}
	}

	// Tier 3: Environment Variables
	if val := strings.TrimSpace(s.getenv("ORBIT_SERVER")); val != "" {
		cfg.Server.URL = val
		entriesMap["server.url"] = &domain.ConfigEntry{Key: "server.url", Value: val, Type: "string", Source: domain.SourceEnv, SourceRef: "$ORBIT_SERVER"}
	}
	if val := strings.TrimSpace(s.getenv("ORBIT_SERVER_TIMEOUT")); val != "" {
		d, err := time.ParseDuration(val)
		if err != nil {
			return nil, fmt.Errorf("invalid ORBIT_SERVER_TIMEOUT %q: %w", val, err)
		}
		cfg.Server.Timeout = d
		entriesMap["server.timeout"] = &domain.ConfigEntry{Key: "server.timeout", Value: d.String(), Type: "duration", Source: domain.SourceEnv, SourceRef: "$ORBIT_SERVER_TIMEOUT"}
	}
	if val := strings.TrimSpace(s.getenv("ORBIT_STAFF_URL")); val != "" {
		cfg.Staff.URL = val
		entriesMap["staff.url"] = &domain.ConfigEntry{Key: "staff.url", Value: val, Type: "string", Source: domain.SourceEnv, SourceRef: "$ORBIT_STAFF_URL"}
	} else if val := strings.TrimSpace(s.getenv("ORBIT_STAFF")); val != "" {
		cfg.Staff.URL = val
		entriesMap["staff.url"] = &domain.ConfigEntry{Key: "staff.url", Value: val, Type: "string", Source: domain.SourceEnv, SourceRef: "$ORBIT_STAFF"}
	}
	if val := strings.TrimSpace(s.getenv("ORBIT_ASSETS_BUCKET")); val != "" {
		cfg.Assets.Bucket = val
		entriesMap["assets.bucket"] = &domain.ConfigEntry{Key: "assets.bucket", Value: val, Type: "string", Source: domain.SourceEnv, SourceRef: "$ORBIT_ASSETS_BUCKET"}
	}
	if val := strings.TrimSpace(s.getenv("ORBIT_ASSETS_ENDPOINT")); val != "" {
		cfg.Assets.Endpoint = val
		entriesMap["assets.endpoint"] = &domain.ConfigEntry{Key: "assets.endpoint", Value: val, Type: "string", Source: domain.SourceEnv, SourceRef: "$ORBIT_ASSETS_ENDPOINT"}
	}
	if val := strings.TrimSpace(s.getenv("ORBIT_ASSETS_AUTO_PULL")); val != "" {
		b, err := strconv.ParseBool(val)
		if err != nil {
			return nil, fmt.Errorf("invalid ORBIT_ASSETS_AUTO_PULL %q: %w", val, err)
		}
		cfg.Assets.AutoPull = b
		entriesMap["assets.auto_pull"] = &domain.ConfigEntry{Key: "assets.auto_pull", Value: strconv.FormatBool(b), Type: "bool", Source: domain.SourceEnv, SourceRef: "$ORBIT_ASSETS_AUTO_PULL"}
	}
	if val := strings.TrimSpace(s.getenv("ORBIT_DEFAULTS_SCOPE")); val != "" {
		cfg.Defaults.Scope = val
		entriesMap["defaults.scope"] = &domain.ConfigEntry{Key: "defaults.scope", Value: val, Type: "string", Source: domain.SourceEnv, SourceRef: "$ORBIT_DEFAULTS_SCOPE"}
	}
	if val := strings.TrimSpace(s.getenv("ORBIT_DEFAULTS_EXPIRY_DAYS")); val != "" {
		days, err := strconv.Atoi(val)
		if err != nil || days <= 0 {
			return nil, fmt.Errorf("invalid ORBIT_DEFAULTS_EXPIRY_DAYS %q: must be positive integer (> 0)", val)
		}
		cfg.Defaults.ExpiryDays = days
		entriesMap["defaults.expiry_days"] = &domain.ConfigEntry{Key: "defaults.expiry_days", Value: strconv.Itoa(days), Type: "int", Source: domain.SourceEnv, SourceRef: "$ORBIT_DEFAULTS_EXPIRY_DAYS"}
	}
	if val := strings.TrimSpace(s.getenv("ORBIT_UI_COLOR")); val != "" {
		b, err := strconv.ParseBool(val)
		if err != nil {
			return nil, fmt.Errorf("invalid ORBIT_UI_COLOR %q: %w", val, err)
		}
		cfg.UI.Color = b
		entriesMap["ui.color"] = &domain.ConfigEntry{Key: "ui.color", Value: strconv.FormatBool(b), Type: "bool", Source: domain.SourceEnv, SourceRef: "$ORBIT_UI_COLOR"}
	}
	if val := strings.TrimSpace(s.getenv("ORBIT_UI_OUTPUT")); val != "" {
		cfg.UI.Output = val
		entriesMap["ui.output"] = &domain.ConfigEntry{Key: "ui.output", Value: val, Type: "string", Source: domain.SourceEnv, SourceRef: "$ORBIT_UI_OUTPUT"}
	}

	// Tier 4: Explicit CLI Flags
	if opts.ServerFlag != "" {
		cfg.Server.URL = opts.ServerFlag
		entriesMap["server.url"] = &domain.ConfigEntry{Key: "server.url", Value: opts.ServerFlag, Type: "string", Source: domain.SourceFlag, SourceRef: "--server"}
	}
	if opts.StaffURLFlag != "" {
		cfg.Staff.URL = opts.StaffURLFlag
		entriesMap["staff.url"] = &domain.ConfigEntry{Key: "staff.url", Value: opts.StaffURLFlag, Type: "string", Source: domain.SourceFlag, SourceRef: "--staff-url"}
	}
	if opts.AssetsBucketFlag != "" {
		cfg.Assets.Bucket = opts.AssetsBucketFlag
		entriesMap["assets.bucket"] = &domain.ConfigEntry{Key: "assets.bucket", Value: opts.AssetsBucketFlag, Type: "string", Source: domain.SourceFlag, SourceRef: "--bucket"}
	}
	if opts.ScopeFlag != "" {
		cfg.Defaults.Scope = opts.ScopeFlag
		entriesMap["defaults.scope"] = &domain.ConfigEntry{Key: "defaults.scope", Value: opts.ScopeFlag, Type: "string", Source: domain.SourceFlag, SourceRef: "--scope"}
	}

	// Step 5: Validation
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	// Step 6: Return
	coreKeys := []string{
		"server.url",
		"server.timeout",
		"staff.url",
		"assets.bucket",
		"assets.endpoint",
		"assets.auto_pull",
		"defaults.scope",
		"defaults.expiry_days",
		"ui.color",
		"ui.output",
	}

	entries := make([]domain.ConfigEntry, 0, len(coreKeys)+len(customEntries))
	for _, k := range coreKeys {
		if e, ok := entriesMap[k]; ok && e != nil {
			entries = append(entries, *e)
		}
	}
	entries = append(entries, customEntries...)

	return &domain.ResolvedConfig{
		Config:  cfg,
		Entries: entries,
	}, nil
}

func (s *Service) out() io.Writer {
	if s.Out == nil {
		return io.Discard
	}
	return s.Out
}

func (s *Service) bold(text string) string {
	if s.UI.Bold == nil {
		return text
	}
	return s.UI.Bold(text)
}

func (s *Service) subtle(text string) string {
	if s.UI.Subtle == nil {
		return text
	}
	return s.UI.Subtle(text)
}

func (s *Service) code(text string) string {
	if s.UI.Code == nil {
		return text
	}
	return s.UI.Code(text)
}

func (s *Service) getenv(key string) string {
	if s.Env == nil {
		return ""
	}
	return s.Env.Getenv(key)
}

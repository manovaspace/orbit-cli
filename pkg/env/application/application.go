// Package application implements the environment check and setup use cases
// over filesystem, schema and MCP-environment ports.
package application

import (
	"context"
	"fmt"
	"io"
	"path/filepath"

	"github.com/manovaspace/orbit-cli/pkg/clierr"
	"github.com/manovaspace/orbit-cli/pkg/env/domain"
)

// FileInfo is the subset of a file's metadata the use cases need.
type FileInfo struct {
	IsDir bool
	Perm  uint32 // unix permission bits, for example 0o600
}

// FS is the filesystem the environment commands read and write.
type FS interface {
	ReadFile(path string) ([]byte, error)
	WriteFile(path string, data []byte, perm uint32) error
	Chmod(path string, perm uint32) error
	MkdirAll(path string) error
	// Stat reports a missing path as exists=false with a nil error.
	Stat(path string) (info FileInfo, exists bool, err error)
}

// SchemaFinder walks a directory tree for .env.schema.yaml contracts.
type SchemaFinder interface {
	Find(root string) ([]string, error)
}

// SchemaLoader reads one schema contract.
type SchemaLoader interface {
	Load(path string) (*domain.EnvSchema, error)
}

// MCPEnv ensures the workspace .cursor/mcp.env file exists with 0600 mode.
type MCPEnv interface {
	Ensure(workspaceRoot string) error
}

// UI carries the decoration cmd/orbit already renders.
type UI struct {
	Title     func(string) string
	Bold      func(string) string
	OK        string
	Warn      string
	Error     string
	Success   func(string) string
	Warning   func(string) string
	ErrorText func(string) string
	Subtle    func(string) string
	Info      func(string) string
}

// Service is the environment use-case layer.
type Service struct {
	FS      FS
	Schemas SchemaFinder
	Loader  SchemaLoader
	MCP     MCPEnv
	Out     io.Writer
	UI      UI
}

// CheckInput is one `env check` invocation. SearchPath is already absolute.
type CheckInput struct {
	SearchPath    string
	WorkspaceRoot string
}

// SchemaReport is the outcome for one schema contract.
type SchemaReport struct {
	Path          string                   `json:"path"`
	Directory     string                   `json:"directory"`
	VariableCount int                      `json:"variables"`
	Valid         bool                     `json:"valid"`
	LoadError     string                   `json:"loadError,omitempty"`
	Errors        []domain.ValidationError `json:"errors,omitempty"`
}

// MCPEnvReport is the permission state of .cursor/mcp.env.
type MCPEnvReport struct {
	Path        string `json:"path"`
	Exists      bool   `json:"exists"`
	Permissions uint32 `json:"permissions,omitempty"`
	Secure      bool   `json:"secure"`
	Error       string `json:"error,omitempty"`
}

// CheckResult is what `env check` produced.
type CheckResult struct {
	SearchPath string         `json:"searchPath"`
	Verified   bool           `json:"verified"`
	Schemas    []SchemaReport `json:"schemas"`
	ValidCount int            `json:"validCount"`
	ErrorCount int            `json:"errorCount"`
	MCPEnv     *MCPEnvReport  `json:"mcpEnv,omitempty"`
}

// Check validates every contract under SearchPath and reports findings.
func (s *Service) Check(_ context.Context, in CheckInput) (CheckResult, error) {
	out := s.out()
	fmt.Fprintln(out, s.title("Orbit Environment Validator"))
	fmt.Fprintf(out, "  Search Path: %s\n\n", s.subtle(in.SearchPath))

	result := CheckResult{SearchPath: in.SearchPath, Schemas: []SchemaReport{}}

	schemas, err := s.Schemas.Find(in.SearchPath)
	if err != nil {
		return result, clierr.Wrap(err, clierr.Unverified("schema discovery failed"))
	}

	if len(schemas) == 0 {
		fmt.Fprintln(out, "Environment unverified: no .env.schema.yaml contracts found.")
	}

	for _, schemaPath := range schemas {
		dir := filepath.Dir(schemaPath)
		relDir := relativeDir(in.WorkspaceRoot, dir)

		report := SchemaReport{Path: schemaPath, Directory: relDir}
		schema, err := s.Loader.Load(schemaPath)
		if err != nil {
			fmt.Fprintf(out, "  %s  %s: failed to load schema: %v\n", s.UI.Error, s.bold(relDir), err)
			result.ErrorCount++
			report.LoadError = err.Error()
			result.Schemas = append(result.Schemas, report)
			continue
		}
		report.VariableCount = len(schema.Variables)

		envPath := filepath.Join(dir, ".env")
		valErrors := ValidateFile(s.FS, envPath, schema)

		if len(valErrors) == 0 {
			result.ValidCount++
			report.Valid = true
			fmt.Fprintf(out, "  %s  %s: %s (%d variables verified)\n",
				s.UI.OK,
				s.bold(relDir),
				s.success(".env valid"),
				len(schema.Variables),
			)
		} else {
			result.ErrorCount += len(valErrors)
			report.Errors = valErrors
			fmt.Fprintf(out, "  %s  %s: %s\n",
				s.UI.Error,
				s.bold(relDir),
				s.errorText(fmt.Sprintf("%d validation issue(s)", len(valErrors))),
			)
			for _, ve := range valErrors {
				var badge string
				if ve.Type == "missing_required" || ve.Type == "file_not_found" {
					badge = s.errorText("[MISSING]")
				} else {
					badge = s.warning("[INVALID]")
				}
				if ve.Variable != "" {
					fmt.Fprintf(out, "       %s %s: %s\n", badge, s.bold(ve.Variable), ve.Message)
				} else {
					fmt.Fprintf(out, "       %s %s\n", badge, ve.Message)
				}
			}
		}
		result.Schemas = append(result.Schemas, report)
	}

	mcp := s.inspectMCPEnv(in.WorkspaceRoot, &result.ErrorCount)
	result.MCPEnv = mcp
	if mcp != nil && mcp.Exists {
		if mcp.Secure {
			fmt.Fprintf(out, "\n  %s  %s: permissions 0600 secure\n", s.UI.OK, s.bold(".cursor/mcp.env"))
		} else {
			fmt.Fprintf(out, "\n  %s  %s: permissions are %04o (recommended: 0600)\n",
				s.UI.Warn, s.bold(".cursor/mcp.env"), mcp.Permissions)
		}
	} else if mcp != nil && mcp.Error != "" {
		fmt.Fprintf(out, "MCP permission inspection failed: %s\n", mcp.Error)
	}

	result.Verified = len(schemas) > 0
	if result.Verified {
		fmt.Fprintf(out, "\n%s  %s\n",
			s.success(fmt.Sprintf("✔ %d schemas valid", result.ValidCount)),
			s.errorText(fmt.Sprintf("✖ %d errors across all env files", result.ErrorCount)),
		)
	} else {
		fmt.Fprintf(out, "\nEnvironment unverified; %d permission or inspection error(s).\n", result.ErrorCount)
	}

	if result.ErrorCount > 0 {
		return result, clierr.Failed(fmt.Sprintf("environment validation failed with %d error(s)", result.ErrorCount))
	}
	if !result.Verified {
		return result, clierr.Unverified("environment unverified: no .env.schema.yaml contracts found")
	}
	return result, nil
}

// SetupInput is one `env setup` invocation. SearchPath is already absolute.
type SetupInput struct {
	SearchPath    string
	WorkspaceRoot string
	Force         bool
}

// SetupFile is the outcome for one generated or skipped .env file.
type SetupFile struct {
	Path      string `json:"path"`
	Directory string `json:"directory"`
	Status    string `json:"status"` // generated, skipped, failed
	Error     string `json:"error,omitempty"`
}

// SetupResult is what `env setup` produced.
type SetupResult struct {
	SearchPath string        `json:"searchPath"`
	Generated  int           `json:"generated"`
	Skipped    int           `json:"skipped"`
	ErrorCount int           `json:"errorCount"`
	Files      []SetupFile   `json:"files"`
	MCPEnv     *MCPEnvReport `json:"mcpEnv,omitempty"`
}

// Setup generates .env files from schema defaults with 0600 permissions.
func (s *Service) Setup(_ context.Context, in SetupInput) (SetupResult, error) {
	out := s.out()
	fmt.Fprintln(out, s.title("Orbit Environment Setup"))

	result := SetupResult{SearchPath: in.SearchPath, Files: []SetupFile{}}

	schemas, err := s.Schemas.Find(in.SearchPath)
	if err != nil {
		return result, clierr.Wrap(err, clierr.Unverified("schema discovery failed"))
	}
	if len(schemas) == 0 {
		return result, clierr.Unverified("environment setup unverified: no .env.schema.yaml contracts found")
	}

	for _, schemaPath := range schemas {
		dir := filepath.Dir(schemaPath)
		relDir := relativeDir(in.WorkspaceRoot, dir)

		envPath := filepath.Join(dir, ".env")
		if _, exists, err := s.FS.Stat(envPath); err == nil && exists && !in.Force {
			result.Skipped++
			result.Files = append(result.Files, SetupFile{Path: envPath, Directory: relDir, Status: "skipped"})
			fmt.Fprintf(out, "  %s  %s: %s\n", s.UI.OK, s.bold(relDir), s.subtle(".env already exists (use --force to overwrite)"))
			continue
		}

		schema, err := s.Loader.Load(schemaPath)
		if err != nil {
			result.ErrorCount++
			result.Files = append(result.Files, SetupFile{Path: envPath, Directory: relDir, Status: "failed", Error: err.Error()})
			fmt.Fprintf(out, "  %s  %s: error reading schema: %v\n", s.UI.Error, s.bold(relDir), err)
			continue
		}

		if err := s.generate(envPath, schema); err != nil {
			result.ErrorCount++
			result.Files = append(result.Files, SetupFile{Path: envPath, Directory: relDir, Status: "failed", Error: err.Error()})
			fmt.Fprintf(out, "  %s  %s: generation failed: %v\n", s.UI.Error, s.bold(relDir), err)
		} else {
			result.Generated++
			result.Files = append(result.Files, SetupFile{Path: envPath, Directory: relDir, Status: "generated"})
			fmt.Fprintf(out, "  %s  %s: %s\n", s.UI.OK, s.bold(relDir), s.success("generated .env (chmod 0600)"))
		}
	}

	if err := s.MCP.Ensure(in.WorkspaceRoot); err != nil {
		result.ErrorCount++
		result.MCPEnv = &MCPEnvReport{Path: filepath.Join(in.WorkspaceRoot, ".cursor", "mcp.env"), Error: err.Error()}
		fmt.Fprintf(out, "  %s  .cursor/mcp.env setup warning: %v\n", s.UI.Warn, err)
	} else {
		result.MCPEnv = &MCPEnvReport{Path: filepath.Join(in.WorkspaceRoot, ".cursor", "mcp.env"), Exists: true, Permissions: 0o600, Secure: true}
		fmt.Fprintf(out, "  %s  %s: %s\n", s.UI.OK, s.bold(".cursor/mcp.env"), s.success("ready (chmod 0600)"))
	}

	fmt.Fprintf(out, "\n%s  %s\n",
		s.success(fmt.Sprintf("✔ %d .env files generated", result.Generated)),
		s.info(fmt.Sprintf("ℹ %d already existed", result.Skipped)),
	)

	if result.ErrorCount > 0 {
		return result, clierr.Failed(fmt.Sprintf("environment setup failed with %d error(s)", result.ErrorCount))
	}
	return result, nil
}

// ValidateFile reads envPath and validates it against schema. It is exported so
// commands outside the env group can keep their contract checks.
func ValidateFile(fs FS, envPath string, schema *domain.EnvSchema) []domain.ValidationError {
	data, err := fs.ReadFile(envPath)
	if err != nil {
		return domain.MissingEnvFileErrors(envPath, fmt.Errorf("failed to read env file: %w", err), schema)
	}
	values, parseErr := domain.ParseEnvContent(string(data))
	if parseErr != nil {
		return domain.MissingEnvFileErrors(envPath, fmt.Errorf("%v", parseErr), schema)
	}
	return domain.ValidateValues(values, schema)
}

func (s *Service) generate(envPath string, schema *domain.EnvSchema) error {
	values, comments := domain.ValuesFromSchema(schema, nil)
	return s.write(envPath, []byte(domain.RenderEnv(values, comments)))
}

func (s *Service) write(path string, data []byte) error {
	if err := s.FS.MkdirAll(filepath.Dir(path)); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", filepath.Dir(path), err)
	}
	if err := s.FS.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("failed to write env file %s: %w", path, err)
	}
	if err := s.FS.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("failed to chmod env file %s: %w", path, err)
	}
	return nil
}

func (s *Service) inspectMCPEnv(workspaceRoot string, errorCount *int) *MCPEnvReport {
	path := filepath.Join(workspaceRoot, ".cursor", "mcp.env")
	info, exists, err := s.FS.Stat(path)
	if err != nil {
		*errorCount = *errorCount + 1
		return &MCPEnvReport{Path: path, Error: err.Error()}
	}
	if !exists {
		return &MCPEnvReport{Path: path}
	}
	report := &MCPEnvReport{Path: path, Exists: true, Permissions: info.Perm, Secure: info.Perm == 0o600}
	if !report.Secure {
		*errorCount = *errorCount + 1
	}
	return report
}

func relativeDir(workspaceRoot, dir string) string {
	relDir, _ := filepath.Rel(workspaceRoot, dir)
	if relDir == "" || relDir == "." {
		relDir = filepath.Base(dir)
	}
	return relDir
}

func (s *Service) out() io.Writer {
	if s.Out == nil {
		return io.Discard
	}
	return s.Out
}

func (s *Service) title(text string) string {
	if s.UI.Title == nil {
		return text
	}
	return s.UI.Title(text)
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

func (s *Service) success(text string) string {
	if s.UI.Success == nil {
		return text
	}
	return s.UI.Success(text)
}

func (s *Service) warning(text string) string {
	if s.UI.Warning == nil {
		return text
	}
	return s.UI.Warning(text)
}

func (s *Service) errorText(text string) string {
	if s.UI.ErrorText == nil {
		return text
	}
	return s.UI.ErrorText(text)
}

func (s *Service) info(text string) string {
	if s.UI.Info == nil {
		return text
	}
	return s.UI.Info(text)
}

package doctor

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Masterminds/semver/v3"
)

// checkProjectToolchains inherits missing runtime declarations up to the repo boundary.
func checkProjectToolchains() []DiagnosticResult {
	root, err := os.Getwd()
	if err != nil {
		return []DiagnosticResult{{Category: "Toolchain", Name: "Manifest requirements", Status: StatusError, Message: "Cannot resolve project directory"}}
	}
	requirements := map[string]string{}
	var results []DiagnosticResult
	foundPackage, foundModule := false, false
	foundBunPin := false
	for dir := root; ; dir = filepath.Dir(dir) {
		if !foundPackage {
			data, err := os.ReadFile(filepath.Join(dir, "package.json"))
			if err == nil {
				var pkg struct {
					PackageManager string            `json:"packageManager"`
					Engines        map[string]string `json:"engines"`
				}
				if err := json.Unmarshal(data, &pkg); err != nil {
					results = append(results, DiagnosticResult{Category: "Runtime", Name: "package.json", Status: StatusError, Message: "Cannot parse project runtime requirements"})
				} else {
					if requirements["node"] == "" {
						requirements["node"] = pkg.Engines["node"]
					}
					if requirements["bun"] == "" {
						requirements["bun"] = pkg.Engines["bun"]
					}
					if !foundBunPin && strings.HasPrefix(pkg.PackageManager, "bun@") {
						foundBunPin = true
						pin := strings.Split(strings.TrimPrefix(pkg.PackageManager, "bun@"), "+")[0]
						if v, err := semver.NewVersion(pin); err == nil {
							if engine := requirements["bun"]; engine != "" {
								constraint, err := semver.NewConstraint(engine)
								if err != nil || !constraint.Check(v) {
									results = append(results, DiagnosticResult{Category: "Runtime", Name: "Bun manifest conflict", Status: StatusError, Message: "packageManager Bun pin conflicts with engines.bun"})
								}
								requirements["bun"] = ">=" + pin + ", " + engine
							} else {
								requirements["bun"] = ">=" + pin
							}
						} else {
							results = append(results, DiagnosticResult{Category: "Runtime", Name: "Bun manifest pin", Status: StatusError, Message: "Cannot parse packageManager Bun version"})
						}
					}
				}
			} else if !os.IsNotExist(err) {
				results = append(results, DiagnosticResult{Category: "Runtime", Name: "package.json", Status: StatusError, Message: "Cannot read project runtime requirements"})
			}
		}
		names := []string{"go.work"}
		if !foundModule {
			names = append(names, "go.mod")
		}
		for _, name := range names {
			if name == "go.work" && os.Getenv("GOWORK") == "off" {
				continue
			}
			data, err := os.ReadFile(filepath.Join(dir, name))
			if err == nil {
				if name == "go.mod" {
					foundModule = true
				}
				for _, line := range strings.Split(string(data), "\n") {
					fields := strings.Fields(line)
					if len(fields) >= 2 && (fields[0] == "go" || fields[0] == "toolchain") {
						floor := strings.TrimPrefix(fields[1], "go")
						if _, err := semver.NewVersion(floor); err != nil {
							results = append(results, DiagnosticResult{Category: "Toolchain", Name: name, Status: StatusError, Message: "Cannot parse Go toolchain requirement"})
							continue
						}
						current := strings.TrimPrefix(requirements["go"], ">=")
						if current == "" || CompareVersions(floor, current) > 0 {
							requirements["go"] = ">=" + floor
						}
					}
				}
			} else if !os.IsNotExist(err) {
				results = append(results, DiagnosticResult{Category: "Toolchain", Name: name, Status: StatusError, Message: "Cannot read Go toolchain requirement"})
			}
		}
		// Project package requirements stop at repository boundaries. An ancestor
		// workspace package.json does not govern an independent Go repository.
		if _, err := os.Lstat(filepath.Join(dir, ".git")); err == nil {
			foundPackage = true
		}
		if dir == filepath.Dir(dir) {
			break
		}
		if _, err := os.Stat(filepath.Join(dir, "workspace.yaml")); err == nil {
			break
		}
	}
	for _, tool := range []struct {
		name, label, category string
		args                  []string
	}{
		{"go", "Go Compiler", "Toolchain", []string{"version"}},
		{"node", "Node.js", "Runtime", []string{"-v"}},
		{"bun", "Bun", "Runtime", []string{"-v"}},
	} {
		output, err := runCommand(defaultCommandTimeout, tool.name, tool.args...)
		result := evaluateRequirement(output, err, tool.name, tool.label, tool.category, requirements[tool.name])
		results = append(results, result)
		if requirements[tool.name] == "" {
			results = append(results, DiagnosticResult{Category: tool.category, Name: tool.label + " requirement", Status: StatusUnverified, Message: "Version compatibility unverified: no project manifest requirement"})
		}
	}
	return results
}

func evaluateRequirement(output string, probeErr error, tool, label, category, requirement string) DiagnosticResult {
	result := DiagnosticResult{Category: category, Name: label}
	if probeErr != nil {
		result.Status = StatusError
		result.Message = label + " unavailable or local version probe failed"
		result.FixSuggestion = "Install a runtime compatible with the project manifests and verify PATH"
		return result
	}
	version, err := ParseSemver(output, tool)
	if err != nil {
		result.Status = StatusUnverified
		result.Message = label + " version unverified: cannot parse version probe"
		return result
	}
	result.Status = StatusOK
	result.Message = fmt.Sprintf("%s v%s installed", label, version)
	if requirement != "" {
		constraint, err := semver.NewConstraint(requirement)
		v, versionErr := semver.NewVersion(version)
		if err != nil || versionErr != nil {
			result.Status = StatusError
			result.Message = label + " manifest requirement invalid"
			return result
		}
		if !constraint.Check(v) {
			result.Status = StatusError
			result.Message = fmt.Sprintf("%s v%s does not satisfy project requirement %s", label, version, requirement)
			result.FixSuggestion = "Install a runtime satisfying the project manifest requirement"
		} else {
			result.Message += " (project requirement " + requirement + ")"
		}
	}
	return result
}

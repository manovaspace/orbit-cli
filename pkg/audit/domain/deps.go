package domain

import (
	"regexp"
	"strings"
)

// Resolved is one resolved package entry of a bun.lock file. Key is the
// lockfile key, which carries the owning workspace for nested resolutions.
type Resolved struct {
	Key     string
	Name    string
	Version string
}

var bunLockEntry = regexp.MustCompile(`^\s*"([^"]+)":\s*\[\s*"([^"]+)"`)

// ParseBunLock reads the resolved versions of pkg from a bun.lock file. Bun
// records the resolved package in the first element of the entry array, and
// nested workspace resolutions appear under "<owner>/<package>" keys.
func ParseBunLock(text, pkg string) []Resolved {
	var out []Resolved
	for _, line := range strings.Split(text, "\n") {
		match := bunLockEntry.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		key := match[1]
		if key != pkg && !strings.HasSuffix(key, "/"+pkg) {
			continue
		}
		name, version, ok := splitPackageSpec(match[2])
		if !ok || name != pkg {
			continue
		}
		out = append(out, Resolved{Key: key, Name: name, Version: version})
	}
	return out
}

// splitPackageSpec splits "name@version"; the name may itself start with "@".
func splitPackageSpec(value string) (name, version string, ok bool) {
	at := strings.LastIndex(value, "@")
	if at <= 0 || at == len(value)-1 {
		return "", "", false
	}
	return value[:at], value[at+1:], true
}

// Import is one named import of one module.
type Import struct {
	Path   string
	Symbol string
}

// NamespaceImport is an `import * as X from "path"` statement.
type NamespaceImport struct {
	Path  string
	Local string
}

var (
	namedImport = regexp.MustCompile(`(?s)import\s+(?:type\s+)?\{([^}]*)\}\s+from\s+["']([^"']+)["']`)
	starImport  = regexp.MustCompile(`(?m)import\s+\*\s+as\s+([A-Za-z0-9_$]+)\s+from\s+["']([^"']+)["']`)
	exportList  = regexp.MustCompile(`(?m)export\s+(?:type\s+)?\{([^}]*)\}`)
	exportDecl  = regexp.MustCompile(`(?m)export\s+(?:default\s+)?(?:async\s+)?(?:function|const|let|var|class|type|interface|enum)\s+([A-Za-z0-9_$]+)`)
)

// ParseImports reads the named and namespace imports of a source file.
func ParseImports(source string) ([]Import, []NamespaceImport) {
	var named []Import
	for _, match := range namedImport.FindAllStringSubmatch(source, -1) {
		path := match[2]
		for _, symbol := range strings.Split(match[1], ",") {
			symbol = strings.TrimSpace(symbol)
			if symbol == "" {
				continue
			}
			symbol = strings.TrimPrefix(symbol, "type ")
			if alias := strings.Split(symbol, " as "); len(alias) == 2 {
				symbol = strings.TrimSpace(alias[0])
			}
			named = append(named, Import{Path: path, Symbol: strings.TrimSpace(symbol)})
		}
	}
	var namespaces []NamespaceImport
	for _, match := range starImport.FindAllStringSubmatch(source, -1) {
		namespaces = append(namespaces, NamespaceImport{Local: match[1], Path: match[2]})
	}
	return named, namespaces
}

// ParseKitExports reads the exported names of a package's dist/index.d.ts.
func ParseKitExports(declarations string) []string {
	var names []string
	for _, match := range exportList.FindAllStringSubmatch(declarations, -1) {
		names = append(names, splitExportList(match[1])...)
	}
	for _, match := range exportDecl.FindAllStringSubmatch(declarations, -1) {
		names = append(names, match[1])
	}
	return uniqueNames(names)
}

// ParseLocalExports reads the names a local source file exports.
func ParseLocalExports(source string) []string {
	var names []string
	for _, match := range exportList.FindAllStringSubmatch(source, -1) {
		names = append(names, splitExportList(match[1])...)
	}
	for _, match := range exportDecl.FindAllStringSubmatch(source, -1) {
		names = append(names, match[1])
	}
	return uniqueNames(names)
}

func splitExportList(list string) []string {
	var names []string
	for _, entry := range strings.Split(list, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		entry = strings.TrimPrefix(entry, "type ")
		// "A as B" exposes B; that is the public name.
		if alias := strings.Split(entry, " as "); len(alias) == 2 {
			entry = strings.TrimSpace(alias[1])
		}
		names = append(names, strings.TrimSpace(entry))
	}
	return names
}

func uniqueNames(values []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, value := range values {
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

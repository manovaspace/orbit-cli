package domain

import (
	"bufio"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// ParseSchema parses YAML bytes into an EnvSchema.
func ParseSchema(data []byte) (*EnvSchema, error) {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		return nil, fmt.Errorf("schema content is empty")
	}

	var schema EnvSchema
	if err := yaml.Unmarshal([]byte(trimmed), &schema); err != nil {
		return nil, fmt.Errorf("failed to parse schema YAML: invalid schema syntax or field types")
	}

	if schema.Version == "" {
		return nil, fmt.Errorf("missing required schema version")
	}

	return &schema, nil
}

// ParseEnvContent parses the raw content of an environment file into key-value pairs.
// It ignores empty lines and comments, and handles optional 'export ' prefixes and quotes.
func ParseEnvContent(content string) (map[string]string, error) {
	env := make(map[string]string)
	scanner := bufio.NewScanner(strings.NewReader(content))
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())

		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		if strings.HasPrefix(line, "export ") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		}

		idx := strings.Index(line, "=")
		if idx == -1 {
			return nil, fmt.Errorf("line %d: invalid syntax (missing '=')", lineNum)
		}

		key := strings.TrimSpace(line[:idx])
		if key == "" {
			return nil, fmt.Errorf("line %d: empty variable name", lineNum)
		}

		rawVal := strings.TrimSpace(line[idx+1:])
		env[key] = unquoteEnvValue(rawVal)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading env content: %w", err)
	}

	return env, nil
}

// unquoteEnvValue handles unquoting double-quoted and single-quoted values,
// as well as stripping unquoted inline comments.
func unquoteEnvValue(raw string) string {
	if len(raw) == 0 {
		return ""
	}

	if strings.HasPrefix(raw, "\"") && strings.HasSuffix(raw, "\"") && len(raw) >= 2 {
		inner := raw[1 : len(raw)-1]
		var sb strings.Builder
		sb.Grow(len(inner))
		for i := 0; i < len(inner); i++ {
			if inner[i] == '\\' && i+1 < len(inner) {
				next := inner[i+1]
				switch next {
				case 'n':
					sb.WriteByte('\n')
					i++
				case 'r':
					sb.WriteByte('\r')
					i++
				case 't':
					sb.WriteByte('\t')
					i++
				case '"':
					sb.WriteByte('"')
					i++
				case '\\':
					sb.WriteByte('\\')
					i++
				case '$':
					sb.WriteByte('$')
					i++
				case '`':
					sb.WriteByte('`')
					i++
				default:
					sb.WriteByte(next)
					i++
				}
			} else {
				sb.WriteByte(inner[i])
			}
		}
		return sb.String()
	}

	if strings.HasPrefix(raw, "'") && strings.HasSuffix(raw, "'") && len(raw) >= 2 {
		return raw[1 : len(raw)-1]
	}

	if commentIdx := strings.Index(raw, " #"); commentIdx != -1 {
		raw = strings.TrimSpace(raw[:commentIdx])
	} else if strings.HasPrefix(raw, "#") {
		return ""
	}

	return strings.TrimSpace(raw)
}

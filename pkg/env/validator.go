package env

import (
	"bufio"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
)

// ParseEnvFile reads and parses an environment file (.env) into key-value pairs.
func ParseEnvFile(path string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read env file: %w", err)
	}

	return ParseEnvContent(string(data))
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

		// Skip empty lines and comment lines
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Handle 'export ' prefix
		if strings.HasPrefix(line, "export ") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		}

		idx := strings.Index(line, "=")
		if idx == -1 {
			return nil, fmt.Errorf("line %d: invalid syntax (missing '='): %s", lineNum, line)
		}

		key := strings.TrimSpace(line[:idx])
		if key == "" {
			return nil, fmt.Errorf("line %d: empty variable name", lineNum)
		}

		rawVal := strings.TrimSpace(line[idx+1:])
		val := unquoteEnvValue(rawVal)
		env[key] = val
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

	// Double quotes
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

	// Single quotes (literal content)
	if strings.HasPrefix(raw, "'") && strings.HasSuffix(raw, "'") && len(raw) >= 2 {
		return raw[1 : len(raw)-1]
	}

	// Unquoted: strip trailing inline comments (e.g. KEY=val # comment)
	if commentIdx := strings.Index(raw, " #"); commentIdx != -1 {
		raw = strings.TrimSpace(raw[:commentIdx])
	} else if strings.HasPrefix(raw, "#") {
		return ""
	}

	return strings.TrimSpace(raw)
}

// Validate checks an environment file against a given schema and returns all validation errors found.
func Validate(envPath string, schema *EnvSchema) []ValidationError {
	if schema == nil {
		return nil
	}

	values, err := ParseEnvFile(envPath)
	if err != nil {
		var errs []ValidationError
		if os.IsNotExist(err) {
			errs = append(errs, ValidationError{
				Variable: "",
				Message:  fmt.Sprintf("env file not found: %s", envPath),
				Type:     "file_not_found",
			})
		} else {
			errs = append(errs, ValidationError{
				Variable: "",
				Message:  fmt.Sprintf("failed to parse env file: %v", err),
				Type:     "syntax_error",
			})
		}
		for _, v := range schema.Variables {
			if v.Required {
				errs = append(errs, ValidationError{
					Variable: v.Name,
					Message:  fmt.Sprintf("required variable %q is missing", v.Name),
					Type:     "missing_required",
				})
			}
		}
		return errs
	}

	return ValidateValues(values, schema)
}

// ValidateEnv is an alias for Validate.
func ValidateEnv(envPath string, schema *EnvSchema) []ValidationError {
	return Validate(envPath, schema)
}

// ValidateValues validates a map of environment key-value pairs against a given schema.
func ValidateValues(values map[string]string, schema *EnvSchema) []ValidationError {
	if schema == nil {
		return nil
	}

	var errors []ValidationError

	for _, v := range schema.Variables {
		val, exists := values[v.Name]
		valTrimmed := strings.TrimSpace(val)

		if !exists || valTrimmed == "" {
			if v.Required {
				errors = append(errors, ValidationError{
					Variable: v.Name,
					Message:  fmt.Sprintf("required variable %q is missing or empty", v.Name),
					Type:     "missing_required",
				})
			}
			continue
		}

		// Type-specific validations for non-empty values
		switch v.Type {
		case TypeInteger, "int":
			if _, err := strconv.Atoi(valTrimmed); err != nil {
				errors = append(errors, ValidationError{
					Variable: v.Name,
					Message:  fmt.Sprintf("variable %q must be an integer, got %q", v.Name, valTrimmed),
					Type:     "invalid_integer",
				})
			}

		case TypeURL:
			u, err := url.ParseRequestURI(valTrimmed)
			if err != nil || u.Scheme == "" || (u.Host == "" && u.Scheme != "file") {
				errors = append(errors, ValidationError{
					Variable: v.Name,
					Message:  fmt.Sprintf("variable %q must be a valid URL, got %q", v.Name, valTrimmed),
					Type:     "invalid_url",
				})
			}

		case TypeBoolean, "bool":
			valLower := strings.ToLower(valTrimmed)
			switch valLower {
			case "true", "false", "1", "0", "t", "f", "yes", "no":
				// Valid boolean
			default:
				errors = append(errors, ValidationError{
					Variable: v.Name,
					Message:  fmt.Sprintf("variable %q must be a boolean (true/false/1/0), got %q", v.Name, valTrimmed),
					Type:     "invalid_boolean",
				})
			}

		case TypeSecret:
			// Non-empty secret is valid
		case TypeString, "":
			// Any string is valid
		default:
			// Unrecognized type, log or accept
		}
	}

	return errors
}

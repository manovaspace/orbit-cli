package domain

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// ValidateValues validates a map of environment key-value pairs against a given schema.
func ValidateValues(values map[string]string, schema *EnvSchema) []ValidationError {
	if schema == nil {
		return []ValidationError{{Type: "no_contract", Message: "environment unverified: no schema contract supplied"}}
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

		switch v.Type {
		case TypeInteger, "int":
			if _, err := strconv.Atoi(valTrimmed); err != nil {
				errors = append(errors, ValidationError{
					Variable: v.Name,
					Message:  fmt.Sprintf("variable %q must be an integer", v.Name),
					Type:     "invalid_integer",
				})
			}

		case TypeURL:
			u, err := url.ParseRequestURI(valTrimmed)
			if err != nil || u.Scheme == "" || (u.Host == "" && u.Scheme != "file") {
				errors = append(errors, ValidationError{
					Variable: v.Name,
					Message:  fmt.Sprintf("variable %q must be a valid URL", v.Name),
					Type:     "invalid_url",
				})
			}

		case TypeBoolean, "bool":
			valLower := strings.ToLower(valTrimmed)
			switch valLower {
			case "true", "false", "1", "0", "t", "f", "yes", "no":
			default:
				errors = append(errors, ValidationError{
					Variable: v.Name,
					Message:  fmt.Sprintf("variable %q must be a boolean (true/false/1/0)", v.Name),
					Type:     "invalid_boolean",
				})
			}

		case TypeSecret:
		case TypeString, "":
		default:
		}
	}

	return errors
}

// MissingEnvFileErrors reports an unreadable env file plus every required
// variable it would have supplied.
func MissingEnvFileErrors(envPath string, readErr error, schema *EnvSchema) []ValidationError {
	var errs []ValidationError
	if isNotExist(readErr) {
		errs = append(errs, ValidationError{
			Variable: "",
			Message:  fmt.Sprintf("env file not found: %s", envPath),
			Type:     "file_not_found",
		})
	} else {
		errs = append(errs, ValidationError{
			Variable: "",
			Message:  fmt.Sprintf("failed to parse env file: %v", readErr),
			Type:     "syntax_error",
		})
	}
	if schema == nil {
		return errs
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

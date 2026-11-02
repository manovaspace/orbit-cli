package domain

import (
	"strings"

	"gopkg.in/yaml.v3"
)

// Exception is one accepted deviation from a versions.env pin. The real file
// is handbook/agents/workspace/version-exceptions.yaml.
type Exception struct {
	Subject     string `yaml:"subject" json:"subject"`
	Allowed     string `yaml:"allowed" json:"allowed"`
	Reason      string `yaml:"reason" json:"reason"`
	ExpiresWhen string `yaml:"expires_when" json:"expires_when"`
}

// ParseExceptions reads the exception file. It accepts either the documented
// "exceptions:" mapping or a bare list.
func ParseExceptions(data []byte) ([]Exception, error) {
	var wrapper struct {
		Exceptions []Exception `yaml:"exceptions"`
	}
	if err := yaml.Unmarshal(data, &wrapper); err == nil && len(wrapper.Exceptions) > 0 {
		return wrapper.Exceptions, nil
	}
	var list []Exception
	if err := yaml.Unmarshal(data, &list); err != nil {
		return nil, err
	}
	return list, nil
}

// ApplyExceptions turns a matching ERROR into INFO with the exception reason.
// A missing or non-matching exception leaves the finding untouched.
func ApplyExceptions(findings []Finding, exceptions []Exception) []Finding {
	if len(exceptions) == 0 {
		return findings
	}
	out := make([]Finding, len(findings))
	for i, finding := range findings {
		if finding.Severity == SeverityError {
			for _, exception := range exceptions {
				if !strings.EqualFold(strings.TrimSpace(exception.Subject), strings.TrimSpace(finding.Subject)) {
					continue
				}
				if !allowedBy(exception.Allowed, finding) {
					continue
				}
				finding.Severity = SeverityInfo
				note := "excepted: " + exception.Reason
				if finding.Fix == "" {
					finding.Fix = note
				} else {
					finding.Fix += "; " + note
				}
				break
			}
		}
		out[i] = finding
	}
	return out
}

func allowedBy(allowed string, finding Finding) bool {
	allowed = strings.TrimSpace(allowed)
	if allowed == "" || allowed == "*" {
		return true
	}
	actual := strings.TrimSpace(finding.Actual)
	if allowed == actual {
		return true
	}
	trim := func(value string) string { return strings.TrimPrefix(value, "^") }
	if ExactPin(trim(allowed)) && ExactPin(trim(actual)) {
		return CompareVersions(trim(allowed), trim(actual)) == 0
	}
	return false
}

package doctor

// CheckStatus represents the status outcome of a diagnostic check.
type CheckStatus string

const (
	// StatusOK indicates the check passed successfully.
	StatusOK CheckStatus = "OK"
	// StatusWarning indicates a non-critical issue or optional tool missing.
	StatusWarning CheckStatus = "WARN"
	// StatusError indicates a critical requirement is missing or failing.
	StatusError CheckStatus = "ERROR"
)

// DiagnosticResult contains the category, name, status, message, and optional remediation suggestion for a check.
type DiagnosticResult struct {
	Category      string      `json:"category" yaml:"category"`
	Name          string      `json:"name" yaml:"name"`
	Status        CheckStatus `json:"status" yaml:"status"`
	Message       string      `json:"message" yaml:"message"`
	FixSuggestion string      `json:"fix_suggestion,omitempty" yaml:"fix_suggestion,omitempty"`
}

// DoctorReport aggregates diagnostic check results and provides query helpers.
type DoctorReport struct {
	Results []DiagnosticResult `json:"results" yaml:"results"`
}

// HasErrors returns true if any diagnostic result has StatusError.
func (r *DoctorReport) HasErrors() bool {
	if r == nil {
		return false
	}
	for _, res := range r.Results {
		if res.Status == StatusError {
			return true
		}
	}
	return false
}

// HasWarnings returns true if any diagnostic result has StatusWarning.
func (r *DoctorReport) HasWarnings() bool {
	if r == nil {
		return false
	}
	for _, res := range r.Results {
		if res.Status == StatusWarning {
			return true
		}
	}
	return false
}

// Add appends a diagnostic result to the report.
func (r *DoctorReport) Add(result DiagnosticResult) {
	r.Results = append(r.Results, result)
}

// AddAll appends multiple diagnostic results to the report.
func (r *DoctorReport) AddAll(results []DiagnosticResult) {
	r.Results = append(r.Results, results...)
}

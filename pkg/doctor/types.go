package doctor

// CheckStatus represents the status outcome of a diagnostic check.
type CheckStatus string

const (
	// StatusOK indicates the check passed successfully.
	StatusOK CheckStatus = "OK"
	// StatusPass is an alias for StatusOK.
	StatusPass CheckStatus = "OK"
	// StatusWarning indicates a non-critical issue or optional tool missing.
	StatusWarning CheckStatus = "WARN"
	// StatusWarn is an alias for StatusWarning.
	StatusWarn CheckStatus = "WARN"
	// StatusError indicates a critical requirement is missing or failing.
	StatusError CheckStatus = "ERROR"
)

// Diagnostic Category Constants
const (
	CategoryCore      = "Core"
	CategoryToolchain = "Toolchain"
	CategoryRuntime   = "Runtime"
	CategoryContainer = "Container"
	CategoryNetwork   = "Network"
	CategorySecurity  = "Security"
	CategoryAuth      = "Authentication"
	CategoryPorts     = "Ports"
	CategoryOptional  = "Optional Tools"
	CategoryDevTools  = "Dev Tools"
	CategoryWorkspace = "Workspace"
)

// DiagnosticResult contains the category, name, status, message, and optional remediation suggestion for a check.
type DiagnosticResult struct {
	Category      string      `json:"category" yaml:"category"`
	Name          string      `json:"name" yaml:"name"`
	Status        CheckStatus `json:"status" yaml:"status"`
	Message       string      `json:"message" yaml:"message"`
	FixSuggestion string      `json:"fix_suggestion,omitempty" yaml:"fix_suggestion,omitempty"`
	IsHealable    bool        `json:"is_healable,omitempty" yaml:"is_healable,omitempty"`
}

// CheckResult is an alias for DiagnosticResult for onboarding wizard diagnostics.
type CheckResult = DiagnosticResult

// DoctorReport aggregates diagnostic check results and provides query helpers.
type DoctorReport struct {
	Results []DiagnosticResult `json:"results" yaml:"results"`
}

// Report is an alias for DoctorReport.
type Report = DoctorReport

// NewReport creates and initializes an empty DoctorReport.
func NewReport() *DoctorReport {
	return &DoctorReport{
		Results: make([]DiagnosticResult, 0),
	}
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

// CountErrors returns the number of results with StatusError.
func (r *DoctorReport) CountErrors() int {
	if r == nil {
		return 0
	}
	count := 0
	for _, res := range r.Results {
		if res.Status == StatusError {
			count++
		}
	}
	return count
}

// CountWarnings returns the number of results with StatusWarning.
func (r *DoctorReport) CountWarnings() int {
	if r == nil {
		return 0
	}
	count := 0
	for _, res := range r.Results {
		if res.Status == StatusWarning {
			count++
		}
	}
	return count
}

// CountPassed returns the number of results with StatusOK / StatusPass.
func (r *DoctorReport) CountPassed() int {
	if r == nil {
		return 0
	}
	count := 0
	for _, res := range r.Results {
		if res.Status == StatusOK {
			count++
		}
	}
	return count
}

// HealableCount returns the count of failed/warning checks marked as healable.
func (r *DoctorReport) HealableCount() int {
	if r == nil {
		return 0
	}
	count := 0
	for _, res := range r.Results {
		if res.Status != StatusOK && res.IsHealable {
			count++
		}
	}
	return count
}

// Add appends a diagnostic result to the report.
func (r *DoctorReport) Add(result DiagnosticResult) {
	r.Results = append(r.Results, result)
}

// AddAll appends multiple diagnostic results to the report.
func (r *DoctorReport) AddAll(results []DiagnosticResult) {
	r.Results = append(r.Results, results...)
}

package application

// UI carries the decoration cmd/orbit already renders. Keeping it injected lets
// this package write progress lines without importing a styling library.
type UI struct {
	OK    string
	Warn  string
	Error string
	Arrow string

	Header    func(string) string
	Bold      func(string) string
	Success   func(string) string
	Warning   func(string) string
	ErrorText func(string) string
	Subtle    func(string) string
}

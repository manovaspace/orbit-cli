package onboard

import (
	"net/http"
	"strings"
)

var cliUserAgentNeedles = []string{
	"curl/",
	"wget/",
	"wget ",
	"powershell",
	"httpie",
	"go-http-client",
}

// wantsInstallHTML is true when GET / should render the copy-button landing
// page. CLI agents (including curl with Accept: text/html) always get the
// installer script so `curl | bash` cannot ingest HTML.
func wantsInstallHTML(r *http.Request) bool {
	if isCLIUserAgent(r.UserAgent()) {
		return false
	}
	accept := strings.ToLower(r.Header.Get("Accept"))
	if strings.Contains(accept, "text/plain") && !strings.Contains(accept, "text/html") {
		return false
	}
	return strings.Contains(accept, "text/html")
}

func isCLIUserAgent(ua string) bool {
	lower := strings.ToLower(ua)
	for _, needle := range cliUserAgentNeedles {
		if strings.Contains(lower, needle) {
			return true
		}
	}
	return false
}

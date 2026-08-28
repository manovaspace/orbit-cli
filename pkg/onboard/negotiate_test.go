package onboard

import (
	"net/http"
	"testing"
)

func TestWantsInstallHTML(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		accept string
		ua     string
		want   bool
	}{
		{name: "empty_defaults_to_script", want: false},
		{name: "curl_star", accept: "*/*", ua: "curl/8.5.0", want: false},
		{name: "curl_html_accept_still_script", accept: "text/html", ua: "curl/8.5.0", want: false},
		{name: "wget", accept: "*/*", ua: "Wget/1.21.4", want: false},
		{name: "httpie", accept: "*/*", ua: "HTTPie/3.2.2", want: false},
		{name: "powershell", accept: "text/html", ua: "Mozilla/5.0 WindowsPowerShell/5.1", want: false},
		{name: "text_plain", accept: "text/plain", ua: "Mozilla/5.0", want: false},
		{
			name:   "browser",
			accept: "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
			ua:     "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/128.0.0.0 Safari/537.36",
			want:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			req, err := http.NewRequest(http.MethodGet, "/", nil)
			if err != nil {
				t.Fatal(err)
			}
			if tt.accept != "" {
				req.Header.Set("Accept", tt.accept)
			}
			if tt.ua != "" {
				req.Header.Set("User-Agent", tt.ua)
			}
			if got := wantsInstallHTML(req); got != tt.want {
				t.Fatalf("wantsInstallHTML() = %v, want %v", got, tt.want)
			}
		})
	}
}

package spec

import (
	"fmt"
	"strings"

	"github.com/manovaspace/orbit-cli/pkg/clierr"
)

const (
	githubSSHPrefix   = "git@github.com:"
	githubHTTPSPrefix = "https://github.com/"
)

// CloneURL converts a GitHub remote between its SSH (git@github.com:o/r.git) and HTTPS
// (https://github.com/o/r.git) forms to match protocol ("https" or "ssh"). Other hosts are returned unchanged.
func CloneURL(remoteURL, protocol string) (string, error) {
	if protocol != "https" && protocol != "ssh" {
		return "", clierr.Usage(fmt.Sprintf("git protocol %q is invalid: want https or ssh", protocol))
	}
	switch {
	case strings.HasPrefix(remoteURL, githubSSHPrefix):
		if protocol == "ssh" {
			return remoteURL, nil
		}
		return githubHTTPSPrefix + strings.TrimPrefix(remoteURL, githubSSHPrefix), nil
	case strings.HasPrefix(remoteURL, githubHTTPSPrefix):
		if protocol == "https" {
			return remoteURL, nil
		}
		return githubSSHPrefix + strings.TrimPrefix(remoteURL, githubHTTPSPrefix), nil
	}
	return remoteURL, nil
}

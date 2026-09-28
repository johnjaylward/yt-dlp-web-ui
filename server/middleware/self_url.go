package middlewares

import (
	"net/url"
	"strings"
)

// IsOwnFileDownloadURL detects links served by this application's filebrowser
// when the submitted URL points back to the host handling the request.
func IsOwnFileDownloadURL(rawURL, requestHost string) bool {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" || requestHost == "" || !strings.EqualFold(u.Host, requestHost) {
		return false
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return false
	}
	return strings.HasPrefix(u.Path, "/filebrowser/d/") || strings.HasPrefix(u.Path, "/filebrowser/v/")
}

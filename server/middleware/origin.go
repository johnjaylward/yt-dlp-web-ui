package middlewares

import (
	"net/http"
	"net/url"
	"strings"
)

// OriginAllowed accepts same-origin browser requests and explicitly configured
// origins. An empty allowlist therefore keeps cross-origin access disabled.
func OriginAllowed(r *http.Request, origin string, allowedOrigins []string) bool {
	if origin == "" {
		return false
	}

	parsed, err := url.Parse(origin)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.Path != "" ||
		parsed.RawQuery != "" || parsed.Fragment != "" ||
		(parsed.Scheme != "http" && parsed.Scheme != "https") {
		return false
	}

	requestScheme := "http"
	if r.TLS != nil {
		requestScheme = "https"
	}
	if strings.EqualFold(parsed.Scheme, requestScheme) && strings.EqualFold(parsed.Host, r.Host) {
		return true
	}

	origin = strings.ToLower(origin)
	for _, configured := range allowedOrigins {
		configured = strings.ToLower(strings.TrimSpace(configured))
		if configured == "*" || configured == origin {
			return true
		}
		if i := strings.IndexByte(configured, '*'); i >= 0 &&
			strings.HasPrefix(origin, configured[:i]) && strings.HasSuffix(origin, configured[i+1:]) {
			return true
		}
	}

	return false
}

package openid

import (
	"net/http"
	"strings"

	middlewares "github.com/marcopiovanello/yt-dlp-web-ui/v4/server/middleware"
)

func tokenFromRequest(r *http.Request) string {
	if parts := strings.Fields(r.Header.Get("Authorization")); len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
		return parts[1]
	}

	token, err := r.Cookie("oid-token")
	if err != nil {
		return ""
	}
	return token.Value
}

func Middleware(next http.Handler) http.Handler {
	return middlewares.Authenticated(next)
}

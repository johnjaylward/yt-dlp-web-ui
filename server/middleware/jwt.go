package middlewares

import (
	"context"
	"net/http"
	"strings"

	"github.com/marcopiovanello/yt-dlp-web-ui/v4/server/auth/session"
)

type principalContextKey struct{}

func PrincipalFromContext(ctx context.Context) (session.Principal, bool) {
	principal, ok := ctx.Value(principalContextKey{}).(session.Principal)
	return principal, ok
}

func sessionTokenFromRequest(r *http.Request) string {
	if cookie, err := r.Cookie(session.CookieName); err == nil && cookie.Value != "" {
		return cookie.Value
	}
	if token := r.Header.Get("X-Authentication"); token != "" {
		return token
	}
	if parts := strings.Fields(r.Header.Get("Authorization")); len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
		return parts[1]
	}
	if token := r.URL.Query().Get("token"); token != "" && token != "null" {
		return token
	}
	return ""
}

// Authenticated validates the application's session JWT and adds its verified
// principal to the request context. Local and OIDC sessions use this same path.
func Authenticated(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims, err := session.Parse(sessionTokenFromRequest(r))
		if err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		principal := claims.Principal
		ctx := context.WithValue(r.Context(), principalContextKey{}, principal)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

package middlewares

import (
	"context"
	"crypto/hmac"
	"log/slog"
	"net/http"
	"strings"

	"github.com/marcopiovanello/yt-dlp-web-ui/v4/server/auth/session"
	"github.com/marcopiovanello/yt-dlp-web-ui/v4/server/config"
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
		if principal.AuthSource == session.AuthSourceLocal {
			authConfig := config.Instance().Authentication
			isAdmin := !config.Instance().OpenId.UseOpenId
			if authConfig.IsAdmin != nil {
				isAdmin = *authConfig.IsAdmin
			}
			fingerprint, fingerprintErr := session.FingerprintLocalAuth(session.LocalAuthConfig{
				Enabled:      authConfig.RequireAuth,
				Username:     authConfig.Username,
				PasswordHash: authConfig.PasswordHash,
				IsAdmin:      isAdmin,
			})
			if fingerprintErr != nil || principal.Username != authConfig.Username ||
				!hmac.Equal([]byte(fingerprint), []byte(claims.LocalAuthFingerprint)) {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
		}

		user := principal.Username
		if user == "" {
			user = principal.ID
		}
		slog.Info("authenticated request",
			slog.String("user", user),
			slog.String("principal_id", principal.ID),
			slog.String("auth_source", string(principal.AuthSource)),
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
		)
		ctx := context.WithValue(r.Context(), principalContextKey{}, principal)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// AdminOnly protects administrative endpoints when authentication is enabled.
// Unauthenticated installations retain their configured open-access behavior.
func AdminOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !config.Instance().Authentication.RequireAuth && !config.Instance().OpenId.UseOpenId {
			next.ServeHTTP(w, r)
			return
		}
		principal, ok := PrincipalFromContext(r.Context())
		if !ok || !principal.IsAdmin {
			http.Error(w, "administrator access required", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

package openid

import (
	"net/http"
	"strings"
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
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := tokenFromRequest(r)
		if token == "" {
			http.Error(w, "missing OpenID token", http.StatusUnauthorized)
			return
		}

		if _, err := verifier.Verify(r.Context(), token); err != nil {
			http.Error(w, err.Error(), http.StatusUnauthorized)
			return
		}

		next.ServeHTTP(w, r)
	})
}

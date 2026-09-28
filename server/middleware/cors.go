package middlewares

import (
	"net/http"

	"github.com/go-chi/cors"
	"github.com/marcopiovanello/yt-dlp-web-ui/v4/server/config"
)

// CORS applies the configured same-origin-by-default CORS policy.
func CORS(next http.Handler) http.Handler {
	return cors.New(cors.Options{
		AllowOriginFunc: func(r *http.Request, origin string) bool {
			return OriginAllowed(r, origin, config.Instance().CORS.AllowedOrigins)
		},
		AllowedMethods:   []string{http.MethodHead, http.MethodGet, http.MethodPost, http.MethodPatch, http.MethodDelete},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-Authentication"},
		AllowCredentials: true,
	}).Handler(next)
}

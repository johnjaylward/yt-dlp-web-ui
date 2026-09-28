package middlewares

import (
	"net/http"

	"github.com/marcopiovanello/yt-dlp-web-ui/v4/server/config"
)

func ApplyAuthenticationByConfig(next http.Handler) http.Handler {
	if config.Instance().Authentication.RequireAuth || config.Instance().OpenId.UseOpenId {
		return Authenticated(next)
	}
	return next
}

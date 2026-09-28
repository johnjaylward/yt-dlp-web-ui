package user

import (
	"encoding/json"
	"net/http"

	"github.com/marcopiovanello/yt-dlp-web-ui/v4/server/auth/session"
	"github.com/marcopiovanello/yt-dlp-web-ui/v4/server/config"
	middlewares "github.com/marcopiovanello/yt-dlp-web-ui/v4/server/middleware"
	"golang.org/x/crypto/bcrypt"
)

const TOKEN_COOKIE_NAME = session.CookieName

func Session(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	principal, authenticated := middlewares.PrincipalFromContext(r.Context())
	if err := json.NewEncoder(w).Encode(struct {
		AuthEnabled   bool               `json:"authEnabled"`
		Authenticated bool               `json:"authenticated"`
		Principal     *session.Principal `json:"principal,omitempty"`
	}{
		AuthEnabled:   config.Instance().Authentication.RequireAuth || config.Instance().OpenId.UseOpenId,
		Authenticated: authenticated,
		Principal:     principalPointer(principal, authenticated),
	}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func principalPointer(principal session.Principal, ok bool) *session.Principal {
	if !ok {
		return nil
	}
	return &principal
}

type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func Login(w http.ResponseWriter, r *http.Request) {
	if !config.Instance().Authentication.RequireAuth {
		http.Error(w, "local authentication is disabled", http.StatusNotFound)
		return
	}

	var req LoginRequest

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var (
		username     = config.Instance().Authentication.Username
		passwordHash = config.Instance().Authentication.PasswordHash
	)

	err := bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(req.Password))
	if err != nil {
		http.Error(w, "invalid username or password", http.StatusBadRequest)
		return
	}

	if username != req.Username {
		http.Error(w, "invalid username or password", http.StatusBadRequest)
		return
	}

	config := config.Instance()
	isAdmin := !config.OpenId.UseOpenId
	if config.Authentication.IsAdmin != nil {
		isAdmin = *config.Authentication.IsAdmin
	}
	principal, err := session.NewLocalPrincipal(req.Username, isAdmin)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	localAuth := session.LocalAuthConfig{
		Enabled:      config.Authentication.RequireAuth,
		Username:     config.Authentication.Username,
		PasswordHash: config.Authentication.PasswordHash,
		IsAdmin:      isAdmin,
	}
	tokenString, expiresAt, err := session.SignLocal(principal, localAuth)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	session.SetCookie(w, r, tokenString, expiresAt)

	if err := json.NewEncoder(w).Encode(tokenString); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func Logout(w http.ResponseWriter, r *http.Request) {
	session.ClearCookie(w, r)
}

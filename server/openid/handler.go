package openid

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/google/uuid"
	"github.com/marcopiovanello/yt-dlp-web-ui/v4/server/config"
	"golang.org/x/oauth2"
)

type OAuth2SuccessResponse struct {
	OAuth2Token   *oauth2.Token
	IDTokenClaims *json.RawMessage
}

// var cookieMaxAge = int(time.Hour * 24 * 30) XXX: overflows on 32 bit architectures.

func Login(w http.ResponseWriter, r *http.Request) {
	state := uuid.NewString()

	nonceBytes := make([]byte, 16)
	rand.Read(nonceBytes)

	nonce := hex.EncodeToString(nonceBytes)

	http.SetCookie(w, &http.Cookie{
		Name:     "state",
		Value:    state,
		HttpOnly: true,
		Path:     "/",
		Secure:   r.TLS != nil,
		// MaxAge:   cookieMaxAge,
		Expires: time.Now().Add(time.Hour * 24 * 30), // XXX: change to MaxAge
	})

	http.SetCookie(w, &http.Cookie{
		Name:     "nonce",
		Value:    nonce,
		HttpOnly: true,
		Path:     "/",
		Secure:   r.TLS != nil,
		// MaxAge:   cookieMaxAge,
		Expires: time.Now().Add(time.Hour * 24 * 30), // XXX: change to MaxAge
	})

	http.Redirect(w, r, oauth2Config.AuthCodeURL(state, oidc.Nonce(nonce)), http.StatusFound)
}

func doAuthentification(r *http.Request, setCookieCallback func(t *oauth2.Token)) (*OAuth2SuccessResponse, error) {
	state, err := r.Cookie("state")
	if err != nil {
		return nil, err
	}

	if r.URL.Query().Get("state") != state.Value {
		return nil, errors.New("auth state does not match")
	}

	oauth2Token, err := oauth2Config.Exchange(r.Context(), r.URL.Query().Get("code"))
	if err != nil {
		return nil, err
	}

	rawToken, ok := oauth2Token.Extra("id_token").(string)
	if !ok {
		return nil, errors.New("openid field \"id_token\" not found in oauth2 token")
	}

	idToken, err := verifier.Verify(r.Context(), rawToken)
	if err != nil {
		return nil, err
	}

	var claims struct {
		Email    string `json:"email"`
		Verified bool   `json:"email_verified"`
	}

	if err := idToken.Claims(&claims); err != nil {
		return nil, err
	}

	whitelist := config.Instance().OpenId.EmailWhitelist

	if len(whitelist) > 0 && !slices.Contains(whitelist, claims.Email) {
		return nil, errors.New("email address not found in ACL")
	}

	nonce, err := r.Cookie("nonce")
	if err != nil {
		return nil, err
	}

	if idToken.Nonce != nonce.Value {
		return nil, errors.New("auth nonce does not match")
	}

	setCookieCallback(oauth2Token)

	// redact
	oauth2Token.AccessToken = "*REDACTED*"

	res := OAuth2SuccessResponse{
		oauth2Token,
		&json.RawMessage{},
	}

	if err := idToken.Claims(&res.IDTokenClaims); err != nil {
		return nil, err
	}

	return &res, nil
}

func SingIn(w http.ResponseWriter, r *http.Request) {
	_, err := doAuthentification(r, func(t *oauth2.Token) {
		idToken, _ := t.Extra("id_token").(string)

		setOIDCookie(w, r, "oid-token", idToken)
		if t.RefreshToken != "" {
			setOIDCookie(w, r, "oid-refresh-token", t.RefreshToken)
		}
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	http.Redirect(w, r, config.Instance().Server.BaseURL+"/", http.StatusSeeOther)
}

func Refresh(w http.ResponseWriter, r *http.Request) {
	if !config.Instance().OpenId.UseOpenId {
		http.NotFound(w, r)
		return
	}

	refreshCookie, err := r.Cookie("oid-refresh-token")
	if err != nil || refreshCookie.Value == "" {
		http.Error(w, "missing OpenID refresh token", http.StatusUnauthorized)
		return
	}
	if idCookie, err := r.Cookie("oid-token"); err == nil && !tokenExpiresSoon(idCookie.Value) {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	ts := oauth2Config.TokenSource(r.Context(), &oauth2.Token{RefreshToken: refreshCookie.Value})

	token, err := ts.Token()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	idToken, ok := token.Extra("id_token").(string)
	if !ok {
		http.Error(w, "refresh response did not contain an ID token", http.StatusBadGateway)
		return
	}
	if _, err := verifier.Verify(r.Context(), idToken); err != nil {
		http.Error(w, "refreshed ID token is invalid: "+err.Error(), http.StatusBadGateway)
		return
	}
	setOIDCookie(w, r, "oid-token", idToken)
	if token.RefreshToken != "" {
		setOIDCookie(w, r, "oid-refresh-token", token.RefreshToken)
	}
	w.WriteHeader(http.StatusNoContent)
}

func setOIDCookie(w http.ResponseWriter, r *http.Request, name, value string) {
	http.SetCookie(w, &http.Cookie{Name: name, Value: value, HttpOnly: true, Path: "/", Secure: r.TLS != nil, SameSite: http.SameSiteLaxMode})
}

func tokenExpiresSoon(token string) bool {
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return true
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return true
	}
	var claims struct {
		Exp int64 `json:"exp"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil || claims.Exp == 0 {
		return true
	}
	return time.Until(time.Unix(claims.Exp, 0)) <= time.Minute
}

func Logout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     "oid-token",
		HttpOnly: true,
		Path:     "/",
		Secure:   r.TLS != nil,
		MaxAge:   -1,
	})
	http.SetCookie(w, &http.Cookie{
		Name: "oid-refresh-token", HttpOnly: true, Path: "/", Secure: r.TLS != nil,
		SameSite: http.SameSiteLaxMode, MaxAge: -1,
	})

	http.SetCookie(w, &http.Cookie{
		Name:     "state",
		HttpOnly: true,
		Path:     "/",
		Secure:   r.TLS != nil,
		MaxAge:   -1,
	})

	http.SetCookie(w, &http.Cookie{
		Name:     "nonce",
		HttpOnly: true,
		Path:     "/",
		Secure:   r.TLS != nil,
		MaxAge:   -1,
	})
}

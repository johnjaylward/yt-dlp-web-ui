package session

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	Issuer     = "yt-dlp-web-ui"
	CookieName = "jwt-yt-dlp-webui"
	Lifetime   = 30 * 24 * time.Hour
)

type AuthSource string

const (
	AuthSourceLocal AuthSource = "local"
	AuthSourceOIDC  AuthSource = "oidc"
)

// Principal is the stable identity and authorization context for an app session.
// Username is descriptive; ID is the value intended for future owner scoping.
type Principal struct {
	ID         string     `json:"principal_id"`
	Username   string     `json:"username"`
	AuthSource AuthSource `json:"auth_source"`
	IDPIssuer  string     `json:"idp_issuer,omitempty"`
	IDPSubject string     `json:"idp_subject,omitempty"`
	IsAdmin    bool       `json:"is_admin"`
}

type Claims struct {
	Principal
	jwt.RegisteredClaims
}

func NewLocalPrincipal(username string, isAdmin bool) (Principal, error) {
	if username == "" {
		return Principal{}, errors.New("local username is empty")
	}
	return Principal{
		ID:         "local:" + username,
		Username:   username,
		AuthSource: AuthSourceLocal,
		IsAdmin:    isAdmin,
	}, nil
}

func NewOIDCPrincipal(issuer, subject, username string, isAdmin bool) (Principal, error) {
	if issuer == "" || subject == "" {
		return Principal{}, errors.New("OIDC issuer and subject are required")
	}
	identity := sha256.Sum256([]byte(issuer + "\x00" + subject))
	return Principal{
		ID:         "oidc:" + hex.EncodeToString(identity[:]),
		Username:   username,
		AuthSource: AuthSourceOIDC,
		IDPIssuer:  issuer,
		IDPSubject: subject,
		IsAdmin:    isAdmin,
	}, nil
}

func Sign(principal Principal) (string, time.Time, error) {
	secret := os.Getenv("JWT_SECRET")
	if secret == "" {
		return "", time.Time{}, errors.New("JWT_SECRET is not configured")
	}
	if principal.ID == "" || principal.AuthSource == "" {
		return "", time.Time{}, errors.New("session principal is incomplete")
	}
	if err := validatePrincipal(principal); err != nil {
		return "", time.Time{}, err
	}
	issuedAt := time.Now().UTC()
	expiresAt := issuedAt.Add(Lifetime)
	claims := Claims{
		Principal: principal,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    Issuer,
			Subject:   principal.ID,
			IssuedAt:  jwt.NewNumericDate(issuedAt),
			NotBefore: jwt.NewNumericDate(issuedAt),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	value, err := token.SignedString([]byte(secret))
	return value, expiresAt, err
}

func Parse(value string) (*Claims, error) {
	secret := os.Getenv("JWT_SECRET")
	if secret == "" {
		return nil, errors.New("JWT_SECRET is not configured")
	}
	claims := new(Claims)
	token, err := jwt.ParseWithClaims(value, claims, func(token *jwt.Token) (any, error) {
		if token.Method.Alg() != jwt.SigningMethodHS256.Alg() {
			return nil, fmt.Errorf("unexpected signing method: %s", token.Method.Alg())
		}
		return []byte(secret), nil
	}, jwt.WithIssuer(Issuer), jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}), jwt.WithExpirationRequired())
	if err != nil {
		return nil, err
	}
	if !token.Valid || claims.Subject == "" || claims.Subject != claims.Principal.ID {
		return nil, errors.New("invalid application session")
	}
	if err := validatePrincipal(claims.Principal); err != nil {
		return nil, err
	}
	return claims, nil
}

func validatePrincipal(principal Principal) error {
	switch principal.AuthSource {
	case AuthSourceLocal:
		if principal.Username == "" || principal.ID != "local:"+principal.Username ||
			principal.IDPIssuer != "" || principal.IDPSubject != "" {
			return errors.New("invalid local session principal")
		}
	case AuthSourceOIDC:
		if principal.IDPIssuer == "" || principal.IDPSubject == "" {
			return errors.New("OIDC session is missing its verified identity")
		}
		expected, err := NewOIDCPrincipal(principal.IDPIssuer, principal.IDPSubject, principal.Username, principal.IsAdmin)
		if err != nil || principal.ID != expected.ID {
			return errors.New("invalid OIDC session principal")
		}
	default:
		return errors.New("invalid application session auth source")
	}
	return nil
}

func SetCookie(w http.ResponseWriter, r *http.Request, value string, expiresAt time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    value,
		Path:     "/",
		Expires:  expiresAt,
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteLaxMode,
	})
}

func ClearCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Path:     "/",
		Expires:  time.Unix(1, 0),
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteLaxMode,
	})
}

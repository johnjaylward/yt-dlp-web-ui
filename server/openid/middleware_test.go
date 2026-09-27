package openid

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTokenFromRequest(t *testing.T) {
	tests := []struct {
		name   string
		header string
		cookie string
		want   string
	}{
		{
			name:   "bearer token",
			header: "Bearer external-id-token",
			cookie: "cookie-id-token",
			want:   "external-id-token",
		},
		{
			name:   "cookie token fallback",
			cookie: "cookie-id-token",
			want:   "cookie-id-token",
		},
		{
			name: "missing token",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/rpc/http", nil)
			if tt.header != "" {
				req.Header.Set("Authorization", tt.header)
			}
			if tt.cookie != "" {
				req.AddCookie(&http.Cookie{Name: "oid-token", Value: tt.cookie})
			}

			if got := tokenFromRequest(req); got != tt.want {
				t.Fatalf("tokenFromRequest() = %q, want %q", got, tt.want)
			}
		})
	}
}

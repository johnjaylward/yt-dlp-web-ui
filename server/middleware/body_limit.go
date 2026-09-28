package middlewares

import "net/http"

// MaxRequestBodyBytes bounds memory and parsing work for request bodies.
const MaxRequestBodyBytes = 1 << 20

func LimitRequestBody(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body != nil {
			r.Body = http.MaxBytesReader(w, r.Body, MaxRequestBodyBytes)
		}
		next.ServeHTTP(w, r)
	})
}

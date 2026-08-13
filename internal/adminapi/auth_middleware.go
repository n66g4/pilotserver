package adminapi

import (
	"net/http"
	"strings"

	"pilotserver/internal/auth"
)

func requireAdmin(jwtSecret string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := ""
		scheme, value, ok := strings.Cut(r.Header.Get("Authorization"), " ")
		if ok && strings.EqualFold(scheme, "Bearer") {
			token = value
		}
		if token == "" {
			token = r.URL.Query().Get("access_token")
		}
		if token == "" || auth.ParseAdminJWT(jwtSecret, token) != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

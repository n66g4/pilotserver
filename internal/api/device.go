package api

import (
	"net/http"
	"strings"

	"pilotserver/internal/auth"
)

func (a *API) me(w http.ResponseWriter, r *http.Request) {
	parts := strings.Fields(r.Header.Get("Authorization"))
	if len(parts) != 2 ||
		(!strings.EqualFold(parts[0], "JWT") && !strings.EqualFold(parts[0], "Bearer")) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	dongleID, err := auth.ParseDeviceJWT(a.jwtSecret, parts[1])
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"dongle_id": dongleID})
}

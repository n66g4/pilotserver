package api

import (
	"net/http"
	"strings"

	"pilotserver/internal/auth"
)

func (a *API) me(w http.ResponseWriter, r *http.Request) {
	dongleID, ok := a.authenticateDevice(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"dongle_id": dongleID})
}

func (a *API) authenticateDevice(w http.ResponseWriter, r *http.Request) (string, bool) {
	parts := strings.Fields(r.Header.Get("Authorization"))
	if len(parts) != 2 ||
		(!strings.EqualFold(parts[0], "JWT") && !strings.EqualFold(parts[0], "Bearer")) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return "", false
	}

	dongleID, err := auth.VerifyDeviceJWT(parts[1], func(identity string) (string, error) {
		device, err := a.store.GetDevice(identity)
		return device.PublicKeyPEM, err
	})
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return "", false
	}
	return dongleID, true
}

package adminapi

import (
	"encoding/json"
	"net/http"
	"time"

	"pilotserver/internal/athena"
	"pilotserver/internal/auth"
	"pilotserver/internal/config"
	"pilotserver/internal/store"
)

const adminJWTTTL = 24 * time.Hour

func Mount(mux *http.ServeMux, st *store.Store, hub *athena.Hub, cfg config.Config, passwordHash string) {
	mux.HandleFunc("POST /admin/api/login", func(w http.ResponseWriter, r *http.Request) {
		handleLogin(w, r, cfg.JWTSecret, passwordHash)
	})

	adminMux := http.NewServeMux()
	adminMux.HandleFunc("GET /admin/api/devices", func(w http.ResponseWriter, _ *http.Request) {
		handleDevices(w, st, hub)
	})
	adminMux.HandleFunc("POST /admin/api/devices/{dongleID}/ssh", func(w http.ResponseWriter, r *http.Request) {
		handleOpenSSH(w, r, hub, cfg)
	})
	mux.Handle("/admin/api/", requireAdmin(cfg.JWTSecret, adminMux))
}

func handleLogin(w http.ResponseWriter, r *http.Request, jwtSecret, passwordHash string) {
	var request struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil ||
		!auth.CheckAdminPassword(request.Password, passwordHash) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	token, err := auth.IssueAdminJWT(jwtSecret, adminJWTTTL)
	if err != nil {
		http.Error(w, "issue admin token", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(struct {
		Token string `json:"token"`
	}{Token: token})
}

func handleDevices(w http.ResponseWriter, st *store.Store, hub *athena.Hub) {
	devices, err := st.ListDevices()
	if err != nil {
		http.Error(w, "list devices", http.StatusInternalServerError)
		return
	}
	response := make([]struct {
		DongleID string `json:"dongle_id"`
		Online   bool   `json:"online"`
	}, 0, len(devices))
	for _, device := range devices {
		response = append(response, struct {
			DongleID string `json:"dongle_id"`
			Online   bool   `json:"online"`
		}{
			DongleID: device.DongleID,
			Online:   hub.IsOnline(device.DongleID),
		})
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}

package adminapi

import (
	"encoding/json"
	"net/http"
	"time"

	"pilotserver/internal/athena"
	"pilotserver/internal/auth"
	"pilotserver/internal/config"
	"pilotserver/internal/listenaddr"
	"pilotserver/internal/publicbase"
	"pilotserver/internal/replay"
	"pilotserver/internal/store"
)

const adminJWTTTL = 24 * time.Hour

func Mount(mux *http.ServeMux, st *store.Store, hub *athena.Hub, cfg config.Config, passwordHash string, baseURL *publicbase.Resolver, listen *listenaddr.Resolver, replayService *replay.Service) {
	mux.HandleFunc("POST /admin/api/login", func(w http.ResponseWriter, r *http.Request) {
		handleLogin(w, r, cfg.JWTSecret, passwordHash)
	})

	adminMux := http.NewServeMux()
	adminMux.HandleFunc("GET /admin/api/settings", func(w http.ResponseWriter, _ *http.Request) {
		handleGetSettings(w, st, baseURL, listen)
	})
	adminMux.HandleFunc("PUT /admin/api/settings", func(w http.ResponseWriter, r *http.Request) {
		handlePutSettings(w, r, st, baseURL, listen)
	})
	adminMux.HandleFunc("GET /admin/api/devices", func(w http.ResponseWriter, _ *http.Request) {
		handleDevices(w, st, hub)
	})
	adminMux.HandleFunc("POST /admin/api/devices/{dongleID}/ssh", func(w http.ResponseWriter, r *http.Request) {
		handleOpenSSH(w, r, hub, baseURL)
	})
	adminMux.HandleFunc("GET /admin/api/devices/{dongleID}/ssh/pty", func(w http.ResponseWriter, r *http.Request) {
		handleSSHPty(w, r, hub, baseURL, cfg.DataDir)
	})
	adminMux.HandleFunc("GET /admin/api/ssh-key", func(w http.ResponseWriter, r *http.Request) {
		handleGetSSHKey(w, cfg.DataDir)
	})
	adminMux.HandleFunc("PUT /admin/api/ssh-key", func(w http.ResponseWriter, r *http.Request) {
		handlePutSSHKey(w, r, cfg.DataDir)
	})
	adminMux.HandleFunc("DELETE /admin/api/ssh-key", func(w http.ResponseWriter, r *http.Request) {
		handleDeleteSSHKey(w, cfg.DataDir)
	})
	adminMux.HandleFunc("GET /admin/api/devices/{dongleID}/routes", func(w http.ResponseWriter, r *http.Request) {
		handleRoutes(w, r, st)
	})
	adminMux.HandleFunc("GET /admin/api/devices/{dongleID}/routes/{route}/segments", func(w http.ResponseWriter, r *http.Request) {
		handleSegments(w, r, st)
	})
	adminMux.HandleFunc("GET /admin/api/devices/{dongleID}/routes/{route}/replay", func(w http.ResponseWriter, r *http.Request) {
		handleReplay(w, r, replayService)
	})
	adminMux.HandleFunc("GET /admin/api/devices/{dongleID}/routes/{route}/segments/{segment}/telemetry", func(w http.ResponseWriter, r *http.Request) {
		handleTelemetry(w, r, replayService)
	})
	adminMux.HandleFunc("POST /admin/api/devices/{dongleID}/routes/{route}/media-ticket", func(w http.ResponseWriter, r *http.Request) {
		handleMediaTicket(w, r, replayService)
	})
	adminMux.HandleFunc("GET /admin/api/devices/{dongleID}/routes/{route}/files/{path...}", func(w http.ResponseWriter, r *http.Request) {
		handleDownload(w, r, cfg.DataDir)
	})
	// Method-specific registration: a bare "/admin/api/" pattern conflicts with
	// the UI's "GET /admin/" in Go 1.22+ ServeMux and panics at startup.
	protected := requireAdmin(cfg.JWTSecret, adminMux)
	mux.Handle("GET /admin/api/", protected)
	mux.Handle("POST /admin/api/", protected)
	mux.Handle("PUT /admin/api/", protected)
	mux.Handle("DELETE /admin/api/", protected)
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

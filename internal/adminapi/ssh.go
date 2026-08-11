package adminapi

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/url"
	"time"

	"pilotserver/internal/athena"
	"pilotserver/internal/config"
)

func handleOpenSSH(w http.ResponseWriter, r *http.Request, hub *athena.Hub, cfg config.Config) {
	base, err := url.Parse(cfg.PublicBaseURL)
	if err != nil || base.Hostname() == "" {
		http.Error(w, "invalid public base URL", http.StatusInternalServerError)
		return
	}
	port, _, err := hub.OpenSSHTunnel(context.Background(), r.PathValue("dongleID"))
	if errors.Is(err, athena.ErrOffline) {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	if err != nil {
		http.Error(w, "open SSH tunnel", http.StatusInternalServerError)
		return
	}
	log.Printf("admin SSH tunnel opened dongle=%s port=%d time=%s",
		r.PathValue("dongleID"), port, time.Now().UTC().Format(time.RFC3339))

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(struct {
		Host      string `json:"host"`
		Port      int    `json:"port"`
		ExpiresIn int    `json:"expires_in"`
	}{
		Host:      base.Hostname(),
		Port:      port,
		ExpiresIn: 600,
	})
}

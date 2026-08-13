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
	"pilotserver/internal/publicbase"
)

func handleOpenSSH(w http.ResponseWriter, r *http.Request, hub *athena.Hub, baseURL *publicbase.Resolver) {
	publicURL := ""
	if baseURL != nil {
		publicURL = baseURL.Get()
	}
	base, err := url.Parse(publicURL)
	if err != nil || base.Hostname() == "" {
		http.Error(w, "public base URL not configured", http.StatusServiceUnavailable)
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

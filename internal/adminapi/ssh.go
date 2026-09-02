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
	"pilotserver/internal/store"
)

func handleOpenSSH(w http.ResponseWriter, r *http.Request, hub *athena.Hub, baseURL *publicbase.Resolver, st *store.Store) {
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
	recordSSHAudit(st, r.PathValue("dongleID"), "tunnel", port)

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

func recordSSHAudit(st *store.Store, dongleID, action string, port int) {
	if st == nil {
		return
	}
	if err := st.InsertSSHAudit(store.SSHAudit{
		DongleID: dongleID,
		Action:   action,
		Port:     port,
	}); err != nil {
		log.Printf("ssh audit: %v", err)
	}
}

func handleListSSHAudit(w http.ResponseWriter, st *store.Store) {
	if st == nil {
		http.Error(w, "list SSH audit", http.StatusInternalServerError)
		return
	}
	entries, err := st.ListSSHAudit(100)
	if err != nil {
		http.Error(w, "list SSH audit", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(entries)
}

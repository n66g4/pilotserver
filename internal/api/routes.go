package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"pilotserver/internal/upload"
)

const uploadURLTTL = 15 * time.Minute

func (a *API) uploadURL(w http.ResponseWriter, r *http.Request) {
	dongleID, ok := a.authenticateDevice(w, r)
	if !ok {
		return
	}
	if dongleID != r.PathValue("dongleID") {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	relPath := r.URL.Query().Get("path")
	if r.Method == http.MethodPost {
		var request struct {
			Path     string `json:"path"`
			Filename string `json:"filename"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		relPath = request.Path
		if relPath == "" {
			relPath = request.Filename
		}
	}
	if err := upload.ValidateRelPath(relPath); err != nil {
		http.Error(w, "invalid upload path", http.StatusBadRequest)
		return
	}

	token, err := upload.Sign(a.jwtSecret, upload.Claim{
		DongleID: dongleID,
		RelPath:  relPath,
		Exp:      time.Now().Add(uploadURLTTL).Unix(),
	})
	if err != nil {
		http.Error(w, "sign upload URL", http.StatusInternalServerError)
		return
	}
	if a.baseURL == nil || a.baseURL.Get() == "" {
		http.Error(w, "public base URL not configured", http.StatusServiceUnavailable)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		URL     string            `json:"url"`
		Headers map[string]string `json:"headers"`
	}{
		URL:     strings.TrimRight(a.baseURL.Get(), "/") + "/upload/put/" + token,
		Headers: map[string]string{},
	})
}

func (a *API) listRoutes(w http.ResponseWriter, r *http.Request) {
	dongleID, ok := a.authenticateDevice(w, r)
	if !ok {
		return
	}
	if dongleID != r.PathValue("dongleID") {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	routes, err := a.store.ListRoutes(dongleID)
	if err != nil {
		http.Error(w, "list routes", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, routes)
}

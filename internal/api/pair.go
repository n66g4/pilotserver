package api

import (
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"time"

	"pilotserver/internal/auth"
	"pilotserver/internal/config"
	"pilotserver/internal/store"
)

const deviceJWTTTL = 24 * time.Hour

type API struct {
	store     *store.Store
	jwtSecret string
}

func New(st *store.Store, cfg config.Config) *API {
	return &API{store: st, jwtSecret: cfg.JWTSecret}
}

func (a *API) Mount(mux *http.ServeMux) {
	mux.HandleFunc("POST "+PairPath, a.pair)
	mux.HandleFunc("GET "+MePath, a.me)
}

func (a *API) pair(w http.ResponseWriter, r *http.Request) {
	var request struct {
		IMEI          string `json:"imei"`
		Serial        string `json:"serial"`
		PublicKey     string `json:"public_key"`
		RegisterToken string `json:"register_token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if !validRSAPublicKey(request.PublicKey) {
		http.Error(w, "invalid public_key", http.StatusBadRequest)
		return
	}

	sum := sha256.Sum256([]byte(request.PublicKey))
	dongleID := hex.EncodeToString(sum[:])[:16]
	if err := a.store.UpsertDevice(store.Device{
		DongleID:     dongleID,
		PublicKeyPEM: request.PublicKey,
		CreatedAt:    time.Now().Unix(),
	}); err != nil {
		http.Error(w, "store device", http.StatusInternalServerError)
		return
	}

	token, err := auth.IssueDeviceJWT(a.jwtSecret, dongleID, deviceJWTTTL)
	if err != nil {
		http.Error(w, "issue access token", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"dongle_id":    dongleID,
		"access_token": token,
	})
}

func validRSAPublicKey(publicKeyPEM string) bool {
	block, _ := pem.Decode([]byte(publicKeyPEM))
	if block == nil {
		return false
	}
	if key, err := x509.ParsePKIXPublicKey(block.Bytes); err == nil {
		_, ok := key.(*rsa.PublicKey)
		return ok
	}
	_, err := x509.ParsePKCS1PublicKey(block.Bytes)
	return err == nil
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

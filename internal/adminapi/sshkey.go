package adminapi

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"

	"pilotserver/internal/sshkey"
	"pilotserver/internal/store"
)

type sshKeyResponse struct {
	Configured  bool   `json:"configured"`
	Fingerprint string `json:"fingerprint,omitempty"`
}

func handleGetSSHKey(w http.ResponseWriter, r *http.Request, st *store.Store, dataDir string) {
	keyStore, ok := openDeviceSSHKey(w, r, st, dataDir)
	if !ok {
		return
	}
	status, err := keyStore.Status()
	if err != nil {
		http.Error(w, "load SSH key", http.StatusInternalServerError)
		return
	}
	writeSSHKey(w, status)
}

func handlePutSSHKey(w http.ResponseWriter, r *http.Request, st *store.Store, dataDir string) {
	var request struct {
		PrivateKey string `json:"private_key"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	keyStore, ok := openDeviceSSHKey(w, r, st, dataDir)
	if !ok {
		return
	}
	status, err := keyStore.Import([]byte(request.PrivateKey))
	if errors.Is(err, sshkey.ErrInvalidKey) {
		http.Error(w, "invalid SSH private key", http.StatusBadRequest)
		return
	}
	if err != nil {
		http.Error(w, "save SSH key", http.StatusInternalServerError)
		return
	}
	writeSSHKey(w, status)
}

func handleDeleteSSHKey(w http.ResponseWriter, r *http.Request, st *store.Store, dataDir string) {
	keyStore, ok := openDeviceSSHKey(w, r, st, dataDir)
	if !ok {
		return
	}
	if err := keyStore.Clear(); err != nil {
		http.Error(w, "clear SSH key", http.StatusInternalServerError)
		return
	}
	writeSSHKey(w, sshkey.Status{})
}

func openDeviceSSHKey(w http.ResponseWriter, r *http.Request, st *store.Store, dataDir string) (*sshkey.Store, bool) {
	dongleID := r.PathValue("dongleID")
	if _, err := st.GetDevice(dongleID); errors.Is(err, sql.ErrNoRows) {
		http.Error(w, "device not found", http.StatusNotFound)
		return nil, false
	} else if err != nil {
		http.Error(w, "load device", http.StatusInternalServerError)
		return nil, false
	}
	keyStore, err := sshkey.Open(dataDir, dongleID)
	if errors.Is(err, sshkey.ErrInvalidDongleID) {
		http.Error(w, "invalid device", http.StatusBadRequest)
		return nil, false
	}
	if err != nil {
		http.Error(w, "open SSH key store", http.StatusInternalServerError)
		return nil, false
	}
	return keyStore, true
}

func writeSSHKey(w http.ResponseWriter, status sshkey.Status) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(sshKeyResponse{
		Configured:  status.Configured,
		Fingerprint: status.Fingerprint,
	})
}

package adminapi

import (
	"encoding/json"
	"errors"
	"net/http"

	"pilotserver/internal/sshkey"
)

type sshKeyResponse struct {
	Configured  bool   `json:"configured"`
	Fingerprint string `json:"fingerprint,omitempty"`
}

func handleGetSSHKey(w http.ResponseWriter, dataDir string) {
	keyStore, err := sshkey.Open(dataDir)
	if err != nil {
		http.Error(w, "open SSH key store", http.StatusInternalServerError)
		return
	}
	status, err := keyStore.Status()
	if err != nil {
		http.Error(w, "load SSH key", http.StatusInternalServerError)
		return
	}
	writeSSHKey(w, status)
}

func handlePutSSHKey(w http.ResponseWriter, r *http.Request, dataDir string) {
	var request struct {
		PrivateKey string `json:"private_key"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	keyStore, err := sshkey.Open(dataDir)
	if err != nil {
		http.Error(w, "open SSH key store", http.StatusInternalServerError)
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

func handleDeleteSSHKey(w http.ResponseWriter, dataDir string) {
	keyStore, err := sshkey.Open(dataDir)
	if err != nil {
		http.Error(w, "open SSH key store", http.StatusInternalServerError)
		return
	}
	if err := keyStore.Clear(); err != nil {
		http.Error(w, "clear SSH key", http.StatusInternalServerError)
		return
	}
	writeSSHKey(w, sshkey.Status{})
}

func writeSSHKey(w http.ResponseWriter, status sshkey.Status) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(sshKeyResponse{
		Configured:  status.Configured,
		Fingerprint: status.Fingerprint,
	})
}

package adminapi

import (
	"encoding/json"
	"net/http"

	"pilotserver/internal/sshkey"
)

func handleGetSSHKey(w http.ResponseWriter, dataDir string) {
	handleSSHKey(w, dataDir, false)
}

func handleRotateSSHKey(w http.ResponseWriter, dataDir string) {
	handleSSHKey(w, dataDir, true)
}

func handleSSHKey(w http.ResponseWriter, dataDir string, rotate bool) {
	keyStore, err := sshkey.Open(dataDir)
	if err != nil {
		http.Error(w, "open SSH key store", http.StatusInternalServerError)
		return
	}
	var publicKey string
	if rotate {
		publicKey, err = keyStore.Rotate()
	} else {
		publicKey, err = keyStore.PublicKey()
	}
	if err != nil {
		http.Error(w, "load SSH key", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(struct {
		PublicKey string `json:"public_key"`
	}{PublicKey: publicKey})
}

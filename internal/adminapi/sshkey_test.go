package adminapi_test

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"pilotserver/internal/athena"
	"pilotserver/internal/auth"
	"pilotserver/internal/config"
	"pilotserver/internal/store"
)

func TestAdminSSHKeyImportClearAndNeverExposesPrivateKey(t *testing.T) {
	dataDir := t.TempDir()
	st, err := store.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.UpsertDevice(store.Device{DongleID: "d1", CreatedAt: 1}); err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	cfg := config.Config{JWTSecret: adminTestSecret, DataDir: dataDir}
	mountAdmin(t, mux, st, athena.NewHub(), cfg, "")

	unauthorized := httptest.NewRecorder()
	mux.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/admin/api/devices/d1/ssh-key", nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d, want %d", unauthorized.Code, http.StatusUnauthorized)
	}

	token, err := auth.IssueAdminJWT(adminTestSecret, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	queryAuthorized := httptest.NewRecorder()
	mux.ServeHTTP(queryAuthorized, httptest.NewRequest(
		http.MethodGet,
		"/admin/api/devices/d1/ssh-key?access_token="+url.QueryEscape(token),
		nil,
	))
	if queryAuthorized.Code != http.StatusOK {
		t.Fatalf("query token status = %d, want %d", queryAuthorized.Code, http.StatusOK)
	}

	request := func(method, target string, body []byte) (int, string) {
		t.Helper()
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(method, target, bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		mux.ServeHTTP(rec, req)
		return rec.Code, rec.Body.String()
	}
	decode := func(body string) struct {
		Configured  bool   `json:"configured"`
		Fingerprint string `json:"fingerprint"`
		PublicKey   string `json:"public_key"`
	} {
		t.Helper()
		if strings.Contains(body, "PRIVATE KEY") {
			t.Fatal("response exposed private key")
		}
		var response struct {
			Configured  bool   `json:"configured"`
			Fingerprint string `json:"fingerprint"`
			PublicKey   string `json:"public_key"`
		}
		if err := json.Unmarshal([]byte(body), &response); err != nil {
			t.Fatal(err)
		}
		if response.PublicKey != "" {
			t.Fatalf("public_key leaked: %q", response.PublicKey)
		}
		return response
	}

	status, body := request(http.MethodGet, "/admin/api/devices/d1/ssh-key", nil)
	if status != http.StatusOK {
		t.Fatalf("GET empty status = %d, body = %s", status, body)
	}
	got := decode(body)
	if got.Configured || got.Fingerprint != "" {
		t.Fatalf("empty GET = %+v", got)
	}

	pemBytes, fingerprint := marshalAdminTestKey(t)
	putBody, err := json.Marshal(map[string]string{"private_key": string(pemBytes)})
	if err != nil {
		t.Fatal(err)
	}
	status, body = request(http.MethodPut, "/admin/api/devices/d1/ssh-key", putBody)
	if status != http.StatusOK {
		t.Fatalf("PUT status = %d, body = %s", status, body)
	}
	got = decode(body)
	if !got.Configured || got.Fingerprint != fingerprint {
		t.Fatalf("PUT response = %+v, want %q", got, fingerprint)
	}

	status, body = request(http.MethodGet, "/admin/api/devices/d1/ssh-key", nil)
	if status != http.StatusOK {
		t.Fatal(body)
	}
	got = decode(body)
	if got.Fingerprint != fingerprint {
		t.Fatalf("GET after PUT = %+v", got)
	}

	missing := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/api/devices/missing/ssh-key", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	mux.ServeHTTP(missing, req)
	if missing.Code != http.StatusNotFound {
		t.Fatalf("missing device status = %d, want %d", missing.Code, http.StatusNotFound)
	}

	status, body = request(http.MethodPut, "/admin/api/devices/d1/ssh-key", []byte(`{"private_key":"not-a-key"}`))
	if status != http.StatusBadRequest {
		t.Fatalf("invalid PUT status = %d, body = %s", status, body)
	}
	status, body = request(http.MethodGet, "/admin/api/devices/d1/ssh-key", nil)
	got = decode(body)
	if got.Fingerprint != fingerprint {
		t.Fatal("invalid PUT overwrote the key")
	}

	status, body = request(http.MethodDelete, "/admin/api/devices/d1/ssh-key", nil)
	if status != http.StatusOK {
		t.Fatalf("DELETE status = %d, body = %s", status, body)
	}
	got = decode(body)
	if got.Configured {
		t.Fatal("still configured after DELETE")
	}
	status, body = request(http.MethodDelete, "/admin/api/devices/d1/ssh-key", nil)
	if status != http.StatusOK {
		t.Fatalf("second DELETE status = %d, body = %s", status, body)
	}
}

func TestAdminSSHKeyGetReturns500ForCorruptStoredKey(t *testing.T) {
	dataDir := t.TempDir()
	st, err := store.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.UpsertDevice(store.Device{DongleID: "d1", CreatedAt: 1}); err != nil {
		t.Fatal(err)
	}

	keyDir := filepath.Join(dataDir, "ssh", "d1")
	if err := os.MkdirAll(keyDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(keyDir, "id_ed25519"), []byte("garbage private key"), 0o600); err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	mountAdmin(t, mux, st, athena.NewHub(), config.Config{
		JWTSecret: adminTestSecret,
		DataDir:   dataDir,
	}, "")
	token, err := auth.IssueAdminJWT(adminTestSecret, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/admin/api/devices/d1/ssh-key", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusInternalServerError, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "PRIVATE KEY") {
		t.Fatal("response exposed private key material")
	}
}

func marshalAdminTestKey(t *testing.T) ([]byte, string) {
	t.Helper()
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	block, err := ssh.MarshalPrivateKey(privateKey, "")
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(block), ssh.FingerprintSHA256(signer.PublicKey())
}

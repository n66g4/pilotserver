package ota_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"pilotserver/internal/config"
	"pilotserver/internal/ota"
)

func TestVersionEndpoint(t *testing.T) {
	dataDir := t.TempDir()
	channelDir := filepath.Join(dataDir, "ota", "release")
	if err := os.MkdirAll(channelDir, 0o755); err != nil {
		t.Fatal(err)
	}
	versionJSON := `{"version":"0.9.1","notes":"test release","download_url":"https://example.test/ota/files/release.tar.gz"}`
	if err := os.WriteFile(filepath.Join(channelDir, "version.json"), []byte(versionJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	ota.Mount(mux, config.Config{DataDir: dataDir})
	server := httptest.NewServer(mux)
	defer server.Close()

	response, err := http.Get(server.URL + "/ota/release/version")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.StatusCode, http.StatusOK)
	}

	var info struct {
		Version string `json:"version"`
	}
	if err := json.NewDecoder(response.Body).Decode(&info); err != nil {
		t.Fatal(err)
	}
	if info.Version != "0.9.1" {
		t.Fatalf("version = %q, want %q", info.Version, "0.9.1")
	}
}

func TestFileServer(t *testing.T) {
	dataDir := t.TempDir()
	filesDir := filepath.Join(dataDir, "ota", "files")
	if err := os.MkdirAll(filesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(filesDir, "release.tar.gz"), []byte("tarball"), 0o644); err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	ota.Mount(mux, config.Config{DataDir: dataDir})
	server := httptest.NewServer(mux)
	defer server.Close()

	response, err := http.Get(server.URL + "/ota/files/release.tar.gz")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.StatusCode, http.StatusOK)
	}
}

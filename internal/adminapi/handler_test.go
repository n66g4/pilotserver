package adminapi_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"pilotserver/internal/adminapi"
	"pilotserver/internal/athena"
	"pilotserver/internal/auth"
	"pilotserver/internal/config"
	"pilotserver/internal/store"
)

const adminTestSecret = "test-secret-at-least-thirty-two-bytes"

func TestAdminLoginAndList(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.UpsertDevice(store.Device{DongleID: "d1", CreatedAt: 1}); err != nil {
		t.Fatal(err)
	}

	passwordHash, err := auth.HashPassword("secret")
	if err != nil {
		t.Fatal(err)
	}
	hub := athena.NewHub()
	hub.SetOnline("d1", athena.NopConn{})
	mux := http.NewServeMux()
	adminapi.Mount(mux, st, hub, config.Config{JWTSecret: adminTestSecret}, passwordHash)

	unauthorized := httptest.NewRecorder()
	mux.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/admin/api/devices", nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d, want %d", unauthorized.Code, http.StatusUnauthorized)
	}

	login := httptest.NewRecorder()
	mux.ServeHTTP(login, httptest.NewRequest(
		http.MethodPost,
		"/admin/api/login",
		bytes.NewBufferString(`{"password":"secret"}`),
	))
	if login.Code != http.StatusOK {
		t.Fatalf("login status = %d, body = %s", login.Code, login.Body.String())
	}
	var loginResponse struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(login.Body).Decode(&loginResponse); err != nil {
		t.Fatal(err)
	}
	if loginResponse.Token == "" {
		t.Fatal("login token is empty")
	}

	list := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/api/devices", nil)
	req.Header.Set("Authorization", "Bearer "+loginResponse.Token)
	mux.ServeHTTP(list, req)
	if list.Code != http.StatusOK {
		t.Fatalf("list status = %d, body = %s", list.Code, list.Body.String())
	}
	var devices []struct {
		DongleID string `json:"dongle_id"`
		Online   bool   `json:"online"`
	}
	if err := json.NewDecoder(list.Body).Decode(&devices); err != nil {
		t.Fatal(err)
	}
	if len(devices) != 1 || devices[0].DongleID != "d1" || !devices[0].Online {
		t.Fatalf("devices = %+v", devices)
	}
}

func TestAdminListsSegmentsAndDownloadsFile(t *testing.T) {
	dataDir := t.TempDir()
	st, err := store.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.InsertSegment(store.Segment{
		DongleID: "d1", RouteName: "route-1", SegmentName: "0",
		RelPath: "route-1/0/rlog.bz2", Size: 4, UploadedAt: 1,
	}); err != nil {
		t.Fatal(err)
	}
	filePath := filepath.Join(dataDir, "uploads", "d1", "route-1", "0", "rlog.bz2")
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filePath, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}

	passwordHash, err := auth.HashPassword("secret")
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{JWTSecret: adminTestSecret, DataDir: dataDir}
	mux := http.NewServeMux()
	adminapi.Mount(mux, st, athena.NewHub(), cfg, passwordHash)
	token, err := auth.IssueAdminJWT(adminTestSecret, time.Minute)
	if err != nil {
		t.Fatal(err)
	}

	for _, target := range []string{
		"/admin/api/devices/d1/routes",
		"/admin/api/devices/d1/routes/route-1/segments",
	} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, target, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s status = %d, body = %s", target, rec.Code, rec.Body.String())
		}
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet,
		"/admin/api/devices/d1/routes/route-1/files/0/rlog.bz2", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Body.String() != "data" {
		t.Fatalf("download status = %d, body = %q", rec.Code, rec.Body.String())
	}
}

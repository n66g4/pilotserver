package adminapi_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

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

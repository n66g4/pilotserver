package adminapi_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pilotserver/internal/adminapi"
	"pilotserver/internal/athena"
	"pilotserver/internal/auth"
	"pilotserver/internal/config"
	"pilotserver/internal/listenaddr"
	"pilotserver/internal/publicbase"
	"pilotserver/internal/replay"
	"pilotserver/internal/store"
)

func mountAdmin(t *testing.T, mux *http.ServeMux, st *store.Store, hub *athena.Hub, cfg config.Config, passwordHash string) *replay.TicketManager {
	t.Helper()
	tickets := replay.NewTicketManager(cfg.JWTSecret, 15*time.Minute)
	replayService := replay.NewService(st, tickets)
	mountAdminWithReplayService(t, mux, st, hub, cfg, passwordHash, replayService)
	return tickets
}

func mountAdminWithReplayService(t *testing.T, mux *http.ServeMux, st *store.Store, hub *athena.Hub, cfg config.Config, passwordHash string, replayService *replay.Service) {
	t.Helper()
	base, err := publicbase.New(st, cfg.PublicBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	listen, err := listenaddr.New(cfg.ListenAddr, "", config.AllowNonLoopback(), nil)
	if err != nil {
		t.Fatal(err)
	}
	hub.SetBaseURLProvider(base.Get)
	adminapi.Mount(mux, st, hub, cfg, passwordHash, base, listen, replayService)
}

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
	mountAdmin(t, mux, st, hub, config.Config{JWTSecret: adminTestSecret}, passwordHash)

	unauthorized := httptest.NewRecorder()
	mux.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/admin/api/devices", nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d, want %d", unauthorized.Code, http.StatusUnauthorized)
	}
	unauthorized = httptest.NewRecorder()
	mux.ServeHTTP(unauthorized, httptest.NewRequest(
		http.MethodPut, "/admin/api/settings", bytes.NewBufferString(`{"map_provider":"none"}`),
	))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized PUT status = %d, want %d", unauthorized.Code, http.StatusUnauthorized)
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
	mountAdmin(t, mux, st, athena.NewHub(), cfg, passwordHash)
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

func TestAdminMapSettingsAuthenticationValidationAndPartialUpdate(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	mux := http.NewServeMux()
	mountAdmin(t, mux, st, athena.NewHub(), config.Config{JWTSecret: adminTestSecret}, "")
	token, err := auth.IssueAdminJWT(adminTestSecret, time.Minute)
	if err != nil {
		t.Fatal(err)
	}

	unauthorized := httptest.NewRecorder()
	mux.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/admin/api/settings", nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d, want %d", unauthorized.Code, http.StatusUnauthorized)
	}

	request := func(method, body string) *httptest.ResponseRecorder {
		t.Helper()
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(method, "/admin/api/settings", bytes.NewBufferString(body))
		req.Header.Set("Authorization", "Bearer "+token)
		mux.ServeHTTP(rec, req)
		return rec
	}

	get := request(http.MethodGet, "")
	if get.Code != http.StatusOK {
		t.Fatalf("default GET status = %d, body = %s", get.Code, get.Body.String())
	}
	var defaults map[string]any
	if err := json.NewDecoder(get.Body).Decode(&defaults); err != nil {
		t.Fatal(err)
	}
	if defaults["map_provider"] != "none" || defaults["map_web_key"] != "" || defaults["map_security_code"] != "" {
		t.Fatalf("default map settings = %#v", defaults)
	}

	for name, body := range map[string]string{
		"unknown provider":    `{"map_provider":"google"}`,
		"missing amap key":    `{"map_provider":"amap"}`,
		"missing tencent key": `{"map_provider":"tencent"}`,
		"oversized key":       `{"map_web_key":"` + strings.Repeat("x", 513) + `"}`,
		"oversized code":      `{"map_security_code":"` + strings.Repeat("x", 513) + `"}`,
	} {
		t.Run(name, func(t *testing.T) {
			rec := request(http.MethodPut, body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusBadRequest, rec.Body.String())
			}
			if strings.Contains(rec.Body.String(), strings.Repeat("x", 32)) {
				t.Fatal("validation response exposed credential")
			}
		})
	}

	put := request(http.MethodPut, `{"map_provider":" amap ","map_web_key":" key ","map_security_code":" code "}`)
	if put.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, body = %s", put.Code, put.Body.String())
	}
	partial := request(http.MethodPut, `{"map_security_code":" replacement "}`)
	if partial.Code != http.StatusOK {
		t.Fatalf("partial PUT status = %d, body = %s", partial.Code, partial.Body.String())
	}
	var got map[string]any
	if err := json.NewDecoder(partial.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got["map_provider"] != "amap" || got["map_web_key"] != "key" || got["map_security_code"] != "replacement" {
		t.Fatalf("partial settings = %#v", got)
	}

	start := make(chan struct{})
	responses := make(chan *httptest.ResponseRecorder, 2)
	for _, body := range []string{
		`{"map_web_key":"concurrent-key"}`,
		`{"map_security_code":"concurrent-code"}`,
	} {
		body := body
		go func() {
			<-start
			responses <- request(http.MethodPut, body)
		}()
	}
	close(start)
	for range 2 {
		rec := <-responses
		if rec.Code != http.StatusOK {
			t.Fatalf("concurrent PUT status = %d, body = %s", rec.Code, rec.Body.String())
		}
	}
	final := request(http.MethodGet, "")
	if err := json.NewDecoder(final.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got["map_web_key"] != "concurrent-key" || got["map_security_code"] != "concurrent-code" {
		t.Fatalf("concurrent partial settings = %#v", got)
	}
}

func TestAdminSettingsValidationDoesNotPersistMapPartial(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.SetMapSettings(store.MapSettings{
		Provider: "amap", WebKey: "original", SecurityCode: "code",
	}); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mountAdmin(t, mux, st, athena.NewHub(), config.Config{JWTSecret: adminTestSecret}, "")
	token, err := auth.IssueAdminJWT(adminTestSecret, time.Minute)
	if err != nil {
		t.Fatal(err)
	}

	for name, body := range map[string]string{
		"invalid listen": `{"map_web_key":"changed","listen_addr":"0.0.0.0:18780","allow_lan":false}`,
		"invalid public": `{"map_web_key":"changed","public_base_url":"ftp://invalid.example"}`,
	} {
		t.Run(name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPut, "/admin/api/settings", bytes.NewBufferString(body))
			req.Header.Set("Authorization", "Bearer "+token)
			mux.ServeHTTP(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
			}
			settings, err := st.GetMapSettings()
			if err != nil {
				t.Fatal(err)
			}
			if settings.WebKey != "original" {
				t.Fatalf("map key persisted after validation failure: %+v", settings)
			}
		})
	}
}

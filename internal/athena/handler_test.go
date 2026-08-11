package athena

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"pilotserver/internal/auth"
	"pilotserver/internal/config"
	"pilotserver/internal/store"
)

const testSecret = "test-secret-at-least-thirty-two-bytes"

func TestTokenFromRequestOrder(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/ws/v2/d1?access_token=query", nil)
	req.AddCookie(&http.Cookie{Name: "jwt", Value: "cookie"})
	req.Header.Set("Authorization", "Bearer header")

	if got := tokenFromRequest(req); got != "cookie" {
		t.Fatalf("token = %q, want cookie", got)
	}

	req = httptest.NewRequest(http.MethodGet, "/ws/v2/d1?access_token=query", nil)
	req.Header.Set("Authorization", "JWT header")
	if got := tokenFromRequest(req); got != "query" {
		t.Fatalf("token = %q, want query", got)
	}

	req = httptest.NewRequest(http.MethodGet, "/ws/v2/d1", nil)
	req.Header.Set("Authorization", "Bearer header")
	if got := tokenFromRequest(req); got != "header" {
		t.Fatalf("token = %q, want header", got)
	}
}

func TestWebSocketTracksOnlineState(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	hub := NewHub()
	mux := http.NewServeMux()
	Mount(mux, st, hub, config.Config{JWTSecret: testSecret})
	server := httptest.NewServer(mux)
	defer server.Close()

	token, err := auth.IssueDeviceJWT(testSecret, "d1", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/ws/v2/d1?access_token="+token, nil)
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return hub.IsOnline("d1") })

	if err := conn.Close(websocket.StatusNormalClosure, ""); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return !hub.IsOnline("d1") })
}

func TestWebSocketRejectsMismatchedDevice(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	token, err := auth.IssueDeviceJWT(testSecret, "other", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	Mount(mux, st, NewHub(), config.Config{JWTSecret: testSecret})
	req := httptest.NewRequest(http.MethodGet, "/ws/v2/d1?access_token="+token, nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestAdminDevicesIncludesOnlineState(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.UpsertDevice(store.Device{DongleID: "d1", CreatedAt: 1}); err != nil {
		t.Fatal(err)
	}

	hub := NewHub()
	hub.SetOnline("d1", NopConn{})
	mux := http.NewServeMux()
	Mount(mux, st, hub, config.Config{JWTSecret: testSecret})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/api/devices", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var got []struct {
		DongleID string `json:"dongle_id"`
		Online   bool   `json:"online"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].DongleID != "d1" || !got[0].Online {
		t.Fatalf("devices = %+v", got)
	}
}

func waitFor(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("condition not met")
}

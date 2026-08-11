package athena

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"pilotserver/internal/auth"
	"pilotserver/internal/config"
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
	hub := NewHub()
	mux := http.NewServeMux()
	Mount(mux, hub, config.Config{JWTSecret: testSecret})
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
	token, err := auth.IssueDeviceJWT(testSecret, "other", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	Mount(mux, NewHub(), config.Config{JWTSecret: testSecret})
	req := httptest.NewRequest(http.MethodGet, "/ws/v2/d1?access_token="+token, nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
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

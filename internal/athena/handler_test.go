package athena

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/golang-jwt/jwt/v5"

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
	hub := NewHub()
	st, token := testDeviceToken(t, "d1")
	defer st.Close()
	mux := http.NewServeMux()
	Mount(mux, hub, st, config.Config{JWTSecret: testSecret})
	server := httptest.NewServer(mux)
	defer server.Close()

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
	st, token := testDeviceToken(t, "other")
	defer st.Close()
	mux := http.NewServeMux()
	Mount(mux, NewHub(), st, config.Config{JWTSecret: testSecret})
	req := httptest.NewRequest(http.MethodGet, "/ws/v2/d1?access_token="+token, nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func testDeviceToken(t *testing.T, dongleID string) (*store.Store, string) {
	t.Helper()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	publicKey := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})
	if err := st.UpsertDevice(store.Device{
		DongleID: dongleID, PublicKeyPEM: string(publicKey), CreatedAt: time.Now().Unix(),
	}); err != nil {
		t.Fatal(err)
	}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"identity": dongleID,
		"nbf":      time.Now().Add(-time.Minute).Unix(),
		"iat":      time.Now().Unix(),
		"exp":      time.Now().Add(time.Minute).Unix(),
	})
	signed, err := token.SignedString(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	return st, signed
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

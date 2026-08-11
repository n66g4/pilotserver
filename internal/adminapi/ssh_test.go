package adminapi_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"pilotserver/internal/adminapi"
	"pilotserver/internal/athena"
	"pilotserver/internal/auth"
	"pilotserver/internal/config"
	"pilotserver/internal/store"
)

type recordingConn struct {
	msg []byte
}

func (c *recordingConn) Send(msg []byte) error {
	c.msg = msg
	return nil
}

func (c *recordingConn) Close() error {
	return nil
}

func TestOpenSSHReturnsPublicEndpoint(t *testing.T) {
	cfg := config.Config{
		PublicBaseURL:    "https://op.example.com",
		JWTSecret:        adminTestSecret,
		SSHTunnelPortMin: 42100,
		SSHTunnelPortMax: 42199,
	}
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	passwordHash, err := auth.HashPassword("secret")
	if err != nil {
		t.Fatal(err)
	}
	hub := athena.NewHub(cfg)
	hub.SetOnline("d1", &recordingConn{})
	mux := http.NewServeMux()
	adminapi.Mount(mux, st, hub, cfg, passwordHash)

	token, err := auth.IssueAdminJWT(adminTestSecret, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/admin/api/devices/d1/ssh", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var response struct {
		Host      string `json:"host"`
		Port      int    `json:"port"`
		ExpiresIn int    `json:"expires_in"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if response.Host != "op.example.com" || response.Port < 42100 || response.Port > 42199 || response.ExpiresIn != 600 {
		t.Fatalf("response = %+v", response)
	}
}

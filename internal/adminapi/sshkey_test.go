package adminapi_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"pilotserver/internal/athena"
	"pilotserver/internal/auth"
	"pilotserver/internal/config"
	"pilotserver/internal/store"
)

func TestAdminSSHKeyRequiresAuthenticationAndRotates(t *testing.T) {
	dataDir := t.TempDir()
	st, err := store.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	mux := http.NewServeMux()
	cfg := config.Config{JWTSecret: adminTestSecret, DataDir: dataDir}
	mountAdmin(t, mux, st, athena.NewHub(), cfg, "")

	unauthorized := httptest.NewRecorder()
	mux.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/admin/api/ssh-key", nil))
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
		"/admin/api/ssh-key?access_token="+url.QueryEscape(token),
		nil,
	))
	if queryAuthorized.Code != http.StatusOK {
		t.Fatalf("query token status = %d, want %d", queryAuthorized.Code, http.StatusOK)
	}
	bogusQuery := httptest.NewRecorder()
	mux.ServeHTTP(bogusQuery, httptest.NewRequest(
		http.MethodGet,
		"/admin/api/ssh-key?access_token=bogus",
		nil,
	))
	if bogusQuery.Code != http.StatusUnauthorized {
		t.Fatalf("bogus query token status = %d, want %d", bogusQuery.Code, http.StatusUnauthorized)
	}
	request := func(method, target string) (int, string) {
		t.Helper()
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(method, target, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		mux.ServeHTTP(rec, req)
		return rec.Code, rec.Body.String()
	}
	publicKey := func(method, target string) string {
		t.Helper()
		status, body := request(method, target)
		if status != http.StatusOK {
			t.Fatalf("%s %s status = %d, body = %s", method, target, status, body)
		}
		if strings.Contains(body, "PRIVATE KEY") {
			t.Fatal("response exposed private key")
		}
		var response struct {
			PublicKey string `json:"public_key"`
		}
		if err := json.Unmarshal([]byte(body), &response); err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(response.PublicKey, "ssh-ed25519 ") {
			t.Fatalf("public_key = %q", response.PublicKey)
		}
		return response.PublicKey
	}

	first := publicKey(http.MethodGet, "/admin/api/ssh-key")
	rotated := publicKey(http.MethodPost, "/admin/api/ssh-key/rotate")
	if rotated == first {
		t.Fatal("rotate returned the same public key")
	}
	afterRotate := publicKey(http.MethodGet, "/admin/api/ssh-key")
	if afterRotate != rotated {
		t.Fatalf("GET after rotate = %q, want %q", afterRotate, rotated)
	}
}

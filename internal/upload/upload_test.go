package upload_test

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"pilotserver/internal/api"
	"pilotserver/internal/config"
	"pilotserver/internal/publicbase"
	"pilotserver/internal/store"
	"pilotserver/internal/upload"
)

const testSecret = "test-secret-at-least-thirty-two-bytes"

type failingReader struct {
	sent bool
}

func (r *failingReader) Read(p []byte) (int, error) {
	if !r.sent {
		r.sent = true
		return copy(p, "partial"), nil
	}
	return 0, errors.New("read failed")
}

func TestSignAndVerifyUploadToken(t *testing.T) {
	claim := upload.Claim{
		DongleID: "dongle-1",
		RelPath:  "route-1/0/rlog.bz2",
		Exp:      time.Now().Add(time.Hour).Unix(),
	}
	token, err := upload.Sign(testSecret, claim)
	if err != nil {
		t.Fatal(err)
	}

	got, err := upload.Verify(testSecret, token)
	if err != nil {
		t.Fatal(err)
	}
	if got != claim {
		t.Fatalf("claim = %+v, want %+v", got, claim)
	}

	if _, err := upload.Verify(testSecret, token+"tampered"); err == nil {
		t.Fatal("tampered token verified")
	}

	expired, err := upload.Sign(testSecret, upload.Claim{
		DongleID: "dongle-1",
		RelPath:  "route-1/0/rlog.bz2",
		Exp:      time.Now().Add(-time.Second).Unix(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := upload.Verify(testSecret, expired); err == nil {
		t.Fatal("expired token verified")
	}
}

func TestValidateRelPathAcceptsSegmentDirectoryAndFile(t *testing.T) {
	if err := upload.ValidateRelPath("2024-01-01--0/qlog.zst"); err != nil {
		t.Fatalf("valid two-segment path rejected: %v", err)
	}
	for _, relPath := range []string{"../qlog.zst", "/tmp/qlog.zst"} {
		if err := upload.ValidateRelPath(relPath); err == nil {
			t.Fatalf("unsafe path %q accepted", relPath)
		}
	}
}

func TestUploadURLPutAndListRoutes(t *testing.T) {
	dataDir := t.TempDir()
	st, err := store.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	cfg := config.Config{
		DataDir:       dataDir,
		PublicBaseURL: "https://uploads.example.test",
		JWTSecret:     testSecret,
	}
	base, err := publicbase.New(st, cfg.PublicBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	api.New(st, cfg, base).Mount(mux)
	upload.Mount(mux, st, cfg)
	server := httptest.NewServer(mux)
	defer server.Close()

	const dongleID = "dongle-1"
	deviceJWT := storeDeviceAndSignJWT(t, st, dongleID)

	request := authorizedRequest(t, http.MethodGet,
		server.URL+"/v1.4/"+dongleID+"/upload_url/?path="+url.QueryEscape("route-1/0/rlog.bz2"),
		nil, deviceJWT)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("upload URL status = %d, want %d", response.StatusCode, http.StatusOK)
	}

	var signedURL struct {
		URL     string            `json:"url"`
		Headers map[string]string `json:"headers"`
	}
	if err := json.NewDecoder(response.Body).Decode(&signedURL); err != nil {
		t.Fatal(err)
	}
	parsedURL, err := url.Parse(signedURL.URL)
	if err != nil {
		t.Fatal(err)
	}
	if parsedURL.Scheme+"://"+parsedURL.Host != cfg.PublicBaseURL {
		t.Fatalf("upload URL = %q, want base %q", signedURL.URL, cfg.PublicBaseURL)
	}

	putRequest, err := http.NewRequest(http.MethodPut, server.URL+parsedURL.Path, bytes.NewBufferString("segment-data"))
	if err != nil {
		t.Fatal(err)
	}
	putResponse, err := http.DefaultClient.Do(putRequest)
	if err != nil {
		t.Fatal(err)
	}
	defer putResponse.Body.Close()
	if putResponse.StatusCode != http.StatusCreated {
		t.Fatalf("PUT status = %d, want %d", putResponse.StatusCode, http.StatusCreated)
	}

	contents, err := os.ReadFile(filepath.Join(dataDir, "uploads", dongleID, "route-1", "0", "rlog.bz2"))
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != "segment-data" {
		t.Fatalf("uploaded contents = %q", contents)
	}

	listRequest := authorizedRequest(t, http.MethodGet,
		server.URL+"/v1/devices/"+dongleID+"/routes", nil, deviceJWT)
	listResponse, err := http.DefaultClient.Do(listRequest)
	if err != nil {
		t.Fatal(err)
	}
	defer listResponse.Body.Close()
	if listResponse.StatusCode != http.StatusOK {
		t.Fatalf("routes status = %d, want %d", listResponse.StatusCode, http.StatusOK)
	}
	var routes []store.Route
	if err := json.NewDecoder(listResponse.Body).Decode(&routes); err != nil {
		t.Fatal(err)
	}
	if len(routes) != 1 || routes[0].Name != "route-1" {
		t.Fatalf("routes = %+v", routes)
	}

	otherRoutesRequest := authorizedRequest(t, http.MethodGet,
		server.URL+"/v1/devices/another-dongle/routes", nil, deviceJWT)
	otherRoutesResponse, err := http.DefaultClient.Do(otherRoutesRequest)
	if err != nil {
		t.Fatal(err)
	}
	defer otherRoutesResponse.Body.Close()
	if otherRoutesResponse.StatusCode != http.StatusForbidden {
		t.Fatalf("other dongle routes status = %d, want %d",
			otherRoutesResponse.StatusCode, http.StatusForbidden)
	}
}

func TestUploadTwoLevelDragonPilotPathWritesCanonicalMetadata(t *testing.T) {
	dataDir := t.TempDir()
	st, err := store.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	cfg := config.Config{DataDir: dataDir, JWTSecret: testSecret}
	mux := http.NewServeMux()
	upload.Mount(mux, st, cfg)

	const (
		dongleID = "dongle-1"
		route    = "00000010--2cbbf69c9f"
		relPath  = route + "--12/qcamera.ts"
	)
	token, err := upload.Sign(testSecret, upload.Claim{
		DongleID: dongleID,
		RelPath:  relPath,
		Exp:      time.Now().Add(time.Hour).Unix(),
	})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPut, "/upload/put/"+token, bytes.NewBufferString("video"))
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}

	segments, err := st.ListSegments(dongleID, route)
	if err != nil {
		t.Fatal(err)
	}
	if len(segments) != 1 {
		t.Fatalf("segments = %+v", segments)
	}
	got := segments[0]
	if got.RouteName != route || got.SegmentName != "12" || got.RelPath != relPath {
		t.Fatalf("segment metadata = %+v", got)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "uploads", dongleID, filepath.FromSlash(relPath))); err != nil {
		t.Fatalf("physical two-level path changed: %v", err)
	}
}

func TestUploadURLRejectsPathTraversal(t *testing.T) {
	dataDir := t.TempDir()
	st, err := store.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	cfg := config.Config{
		DataDir:       dataDir,
		PublicBaseURL: "https://uploads.example.test",
		JWTSecret:     testSecret,
	}
	base, err := publicbase.New(st, cfg.PublicBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	api.New(st, cfg, base).Mount(mux)

	token := storeDeviceAndSignJWT(t, st, "dongle-1")
	request := authorizedRequest(t, http.MethodPost,
		"/v1.1/devices/dongle-1/upload_url/", bytes.NewBufferString(`{"filename":"../outside"}`), token)
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusBadRequest)
	}
}

func TestUploadRejectsOversizedRequest(t *testing.T) {
	dataDir := t.TempDir()
	st, err := store.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	cfg := config.Config{DataDir: dataDir, JWTSecret: testSecret}
	mux := http.NewServeMux()
	upload.Mount(mux, st, cfg)
	token, err := upload.Sign(testSecret, upload.Claim{
		DongleID: "dongle-1",
		RelPath:  "route-1/0/rlog.bz2",
		Exp:      time.Now().Add(time.Hour).Unix(),
	})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPut, "/upload/put/"+token, bytes.NewReader([]byte("small")))
	req.ContentLength = upload.MaxUploadSize + 1
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusRequestEntityTooLarge)
	}
}

func TestFailedUploadPreservesExistingFile(t *testing.T) {
	dataDir := t.TempDir()
	st, err := store.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	cfg := config.Config{DataDir: dataDir, JWTSecret: testSecret}
	mux := http.NewServeMux()
	upload.Mount(mux, st, cfg)
	target := filepath.Join(dataDir, "uploads", "dongle-1", "route-1", "0", "rlog.bz2")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("good"), 0o644); err != nil {
		t.Fatal(err)
	}
	token, err := upload.Sign(testSecret, upload.Claim{
		DongleID: "dongle-1",
		RelPath:  "route-1/0/rlog.bz2",
		Exp:      time.Now().Add(time.Hour).Unix(),
	})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPut, "/upload/put/"+token, &failingReader{})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
	contents, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != "good" {
		t.Fatalf("existing contents = %q", contents)
	}
}

func TestUploadPruneDeletesOldestWhenOverCap(t *testing.T) {
	dataDir := t.TempDir()
	st, err := store.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.SetUploadPolicy(store.UploadPolicy{MaxBytes: 20}); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{DataDir: dataDir, JWTSecret: testSecret}
	mux := http.NewServeMux()
	upload.Mount(mux, st, cfg)

	put := func(relPath, body string) {
		t.Helper()
		token, err := upload.Sign(testSecret, upload.Claim{
			DongleID: "d1", RelPath: relPath, Exp: time.Now().Add(time.Hour).Unix(),
		})
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodPut, "/upload/put/"+token, bytes.NewBufferString(body))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusCreated {
			t.Fatalf("PUT %s status = %d, body = %s", relPath, rec.Code, rec.Body.String())
		}
	}
	put("old/0/a.ts", "123456789012")
	put("new/0/b.ts", "abcdefghijkl")

	if _, err := os.Stat(filepath.Join(dataDir, "uploads", "d1", "old", "0", "a.ts")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("expected oldest upload to be pruned")
	}
	if _, err := os.Stat(filepath.Join(dataDir, "uploads", "d1", "new", "0", "b.ts")); err != nil {
		t.Fatal(err)
	}
	total, err := st.TotalUploadBytes()
	if err != nil {
		t.Fatal(err)
	}
	if total != 12 {
		t.Fatalf("total = %d, want 12", total)
	}
}

func storeDeviceAndSignJWT(t *testing.T, st *store.Store, dongleID string) string {
	t.Helper()
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	publicKeyPEM := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})
	if err := st.UpsertDevice(store.Device{
		DongleID:     dongleID,
		PublicKeyPEM: string(publicKeyPEM),
		CreatedAt:    time.Now().Unix(),
	}); err != nil {
		t.Fatal(err)
	}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"identity": dongleID,
		"nbf":      time.Now().Add(-time.Minute).Unix(),
		"iat":      time.Now().Unix(),
		"exp":      time.Now().Add(time.Hour).Unix(),
	})
	signed, err := token.SignedString(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	return signed
}

func authorizedRequest(t *testing.T, method, target string, body *bytes.Buffer, token string) *http.Request {
	t.Helper()
	var requestBody io.Reader
	if body != nil {
		requestBody = body
	}
	request, err := http.NewRequest(method, target, requestBody)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "JWT "+token)
	return request
}

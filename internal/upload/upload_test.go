package upload_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"pilotserver/internal/api"
	"pilotserver/internal/auth"
	"pilotserver/internal/config"
	"pilotserver/internal/store"
	"pilotserver/internal/upload"
)

const testSecret = "test-secret-at-least-thirty-two-bytes"

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
	mux := http.NewServeMux()
	api.New(st, cfg).Mount(mux)
	upload.Mount(mux, st, cfg)
	server := httptest.NewServer(mux)
	defer server.Close()

	const dongleID = "dongle-1"
	deviceJWT, err := auth.IssueDeviceJWT(testSecret, dongleID, time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	requestBody := bytes.NewBufferString(`{"path":"route-1/0/rlog.bz2"}`)
	request := authorizedRequest(t, http.MethodPost,
		server.URL+"/v1.1/devices/"+dongleID+"/upload_url/", requestBody, deviceJWT)
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
	if putResponse.StatusCode != http.StatusNoContent {
		t.Fatalf("PUT status = %d, want %d", putResponse.StatusCode, http.StatusNoContent)
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
	mux := http.NewServeMux()
	api.New(st, cfg).Mount(mux)

	token, err := auth.IssueDeviceJWT(testSecret, "dongle-1", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	request := authorizedRequest(t, http.MethodPost,
		"/v1.1/devices/dongle-1/upload_url/", bytes.NewBufferString(`{"filename":"../outside"}`), token)
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusBadRequest)
	}
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

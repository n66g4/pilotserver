package api

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"pilotserver/internal/config"
	"pilotserver/internal/store"
)

func TestPairAndMe(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	api := New(st, config.Config{
		JWTSecret:    "test-secret-at-least-thirty-two-bytes",
		PairingToken: "pairing-token",
	})
	mux := http.NewServeMux()
	api.Mount(mux)
	server := httptest.NewServer(mux)
	defer server.Close()

	privateKey, publicKey := testKeyPair(t)
	body, err := json.Marshal(map[string]string{
		"imei":           "123456789012345",
		"serial":         "serial-1",
		"public_key":     publicKey,
		"register_token": signRegisterToken(t, privateKey, true),
		"pair_code":      "pairing-token",
	})
	if err != nil {
		t.Fatal(err)
	}

	resp, err := http.Post(server.URL+PairPath, "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("pair status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var pair struct {
		DongleID    string `json:"dongle_id"`
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&pair); err != nil {
		t.Fatal(err)
	}
	if pair.DongleID == "" || pair.AccessToken == "" {
		t.Fatalf("pair response missing fields: %+v", pair)
	}
	sum := sha256.Sum256([]byte(publicKey))
	wantDongleID := hex.EncodeToString(sum[:])[:16]
	if pair.DongleID != wantDongleID {
		t.Fatalf("dongle_id = %q, want %q", pair.DongleID, wantDongleID)
	}

	device, err := st.GetDevice(pair.DongleID)
	if err != nil {
		t.Fatal(err)
	}
	if device.PublicKeyPEM != publicKey {
		t.Fatal("stored public key does not match request")
	}
	deviceToken := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"identity": pair.DongleID,
		"nbf":      time.Now().Add(-time.Minute).Unix(),
		"iat":      time.Now().Unix(),
		"exp":      time.Now().Add(time.Hour).Unix(),
	})
	signedDeviceToken, err := deviceToken.SignedString(privateKey)
	if err != nil {
		t.Fatal(err)
	}

	for _, scheme := range []string{"JWT", "Bearer"} {
		t.Run(scheme, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodGet, server.URL+MePath, nil)
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Authorization", fmt.Sprintf("%s %s", scheme, signedDeviceToken))

			meResp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer meResp.Body.Close()
			if meResp.StatusCode != http.StatusOK {
				t.Fatalf("me status = %d, want %d", meResp.StatusCode, http.StatusOK)
			}

			var me struct {
				DongleID string `json:"dongle_id"`
			}
			if err := json.NewDecoder(meResp.Body).Decode(&me); err != nil {
				t.Fatal(err)
			}
			if me.DongleID != pair.DongleID {
				t.Fatalf("dongle_id = %q, want %q", me.DongleID, pair.DongleID)
			}
		})
	}
}

func TestPairRejectsWrongPairCode(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	api := New(st, config.Config{
		JWTSecret:    "test-secret-at-least-thirty-two-bytes",
		PairingToken: "pairing-token",
	})
	mux := http.NewServeMux()
	api.Mount(mux)
	privateKey, publicKey := testKeyPair(t)
	body, err := json.Marshal(map[string]string{
		"public_key":     publicKey,
		"register_token": signRegisterToken(t, privateKey, true),
		"pair_code":      "wrong-token",
	})
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, PairPath, bytes.NewReader(body)))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestPairAllowsSignedRegisterJWTWithoutPairingToken(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	api := New(st, config.Config{JWTSecret: "test-secret-at-least-thirty-two-bytes"})
	mux := http.NewServeMux()
	api.Mount(mux)
	privateKey, publicKey := testKeyPair(t)
	body, err := json.Marshal(map[string]string{
		"public_key":     publicKey,
		"register_token": signRegisterToken(t, privateKey, true),
	})
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, PairPath, bytes.NewReader(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestPairRejectsJWTWithoutRegisterIntent(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	api := New(st, config.Config{JWTSecret: "test-secret-at-least-thirty-two-bytes"})
	mux := http.NewServeMux()
	api.Mount(mux)
	privateKey, publicKey := testKeyPair(t)
	body, err := json.Marshal(map[string]string{
		"public_key":     publicKey,
		"register_token": signRegisterToken(t, privateKey, false),
	})
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, PairPath, bytes.NewReader(body)))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestPairRejectsInvalidPublicKey(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	api := New(st, config.Config{
		JWTSecret:    "test-secret-at-least-thirty-two-bytes",
		PairingToken: "pairing-token",
	})
	mux := http.NewServeMux()
	api.Mount(mux)

	body := []byte(`{"imei":"123","serial":"serial-1","public_key":"not pem","register_token":"pairing-token"}`)
	req := httptest.NewRequest(http.MethodPost, PairPath, bytes.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func testKeyPair(t *testing.T) (*rsa.PrivateKey, string) {
	t.Helper()
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	return privateKey, string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))
}

func signRegisterToken(t *testing.T, privateKey *rsa.PrivateKey, register bool) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"register": register,
		"exp":      time.Now().Add(time.Hour).Unix(),
	})
	signed, err := token.SignedString(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	return signed
}

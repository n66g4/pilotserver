package auth_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"pilotserver/internal/auth"
)

func TestVerifyDeviceJWT(t *testing.T) {
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	ecKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name       string
		method     jwt.SigningMethod
		privateKey any
		publicKey  any
	}{
		{name: "RS256", method: jwt.SigningMethodRS256, privateKey: rsaKey, publicKey: &rsaKey.PublicKey},
		{name: "ES256", method: jwt.SigningMethodES256, privateKey: ecKey, publicKey: &ecKey.PublicKey},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			token := jwt.NewWithClaims(tt.method, jwt.MapClaims{
				"identity": "dongleA",
				"nbf":      time.Now().Add(-time.Minute).Unix(),
				"iat":      time.Now().Unix(),
				"exp":      time.Now().Add(time.Hour).Unix(),
			})
			signed, err := token.SignedString(tt.privateKey)
			if err != nil {
				t.Fatal(err)
			}
			der, err := x509.MarshalPKIXPublicKey(tt.publicKey)
			if err != nil {
				t.Fatal(err)
			}
			publicKeyPEM := string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))

			id, err := auth.VerifyDeviceJWT(signed, func(identity string) (string, error) {
				if identity != "dongleA" {
					t.Fatalf("lookup identity = %q", identity)
				}
				return publicKeyPEM, nil
			})
			if err != nil || id != "dongleA" {
				t.Fatalf("id=%s err=%v", id, err)
			}
		})
	}
}

func TestVerifyDeviceJWTRejectsHS256(t *testing.T) {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"identity": "dongleA",
		"exp":      time.Now().Add(time.Hour).Unix(),
	})
	signed, err := token.SignedString([]byte("test-secret-at-least-32-bytes-long!!"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := auth.VerifyDeviceJWT(signed, func(string) (string, error) { return "", nil }); err == nil {
		t.Fatal("HS256 device JWT verified")
	}
}

func TestAdminPassword(t *testing.T) {
	hash, err := auth.HashPassword("secret")
	if err != nil {
		t.Fatal(err)
	}
	if !auth.CheckAdminPassword("secret", hash) {
		t.Fatal("expected match")
	}
}

func TestAdminJWTRoundTrip(t *testing.T) {
	secret := "test-secret-at-least-32-bytes-long!!"
	tok, err := auth.IssueAdminJWT(secret, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if err := auth.ParseAdminJWT(secret, tok); err != nil {
		t.Fatal(err)
	}
}

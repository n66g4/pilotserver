package auth_test

import (
	"testing"
	"time"

	"pilotserver/internal/auth"
)

func TestDeviceJWTRoundTrip(t *testing.T) {
	secret := "test-secret-at-least-32-bytes-long!!"
	tok, err := auth.IssueDeviceJWT(secret, "dongleA", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	id, err := auth.ParseDeviceJWT(secret, tok)
	if err != nil || id != "dongleA" {
		t.Fatalf("id=%s err=%v", id, err)
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

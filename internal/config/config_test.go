package config_test

import (
	"testing"

	"pilotserver/internal/config"
)

func TestLoadDefaults(t *testing.T) {
	t.Setenv("PILOTSERVER_LISTEN", "")
	t.Setenv("PILOTSERVER_DATA_DIR", t.TempDir())
	t.Setenv("PILOTSERVER_PUBLIC_BASE_URL", "https://op.example.com")
	t.Setenv("PILOTSERVER_JWT_SECRET", "test-secret-at-least-32-bytes-long!!")
	t.Setenv("PILOTSERVER_ADMIN_PASSWORD", "admin-password")
	t.Setenv("PILOTSERVER_PAIRING_TOKEN", "pairing-token")
	t.Setenv("PILOTSERVER_ALLOW_NON_LOOPBACK", "")

	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ListenAddr != "127.0.0.1:8080" {
		t.Fatalf("listen: %s", cfg.ListenAddr)
	}
}

func TestLoadRequiresAdminPasswordAndAllowsOptionalPairingToken(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("PILOTSERVER_ADMIN_PASSWORD", "")
	if _, err := config.Load(); err == nil {
		t.Fatal("missing admin password accepted")
	}

	setRequiredEnv(t)
	t.Setenv("PILOTSERVER_PAIRING_TOKEN", "")
	if _, err := config.Load(); err != nil {
		t.Fatalf("optional pairing token rejected: %v", err)
	}
}

func TestLoadRejectsNonLoopbackListen(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("PILOTSERVER_LISTEN", "0.0.0.0:8080")
	t.Setenv("PILOTSERVER_ALLOW_NON_LOOPBACK", "")
	if _, err := config.Load(); err == nil {
		t.Fatal("non-loopback listen accepted")
	}

	t.Setenv("PILOTSERVER_ALLOW_NON_LOOPBACK", "1")
	if _, err := config.Load(); err != nil {
		t.Fatalf("explicitly allowed non-loopback listen rejected: %v", err)
	}
}

func setRequiredEnv(t *testing.T) {
	t.Helper()
	t.Setenv("PILOTSERVER_LISTEN", "")
	t.Setenv("PILOTSERVER_DATA_DIR", t.TempDir())
	t.Setenv("PILOTSERVER_PUBLIC_BASE_URL", "https://op.example.com")
	t.Setenv("PILOTSERVER_JWT_SECRET", "test-secret-at-least-32-bytes-long!!")
	t.Setenv("PILOTSERVER_ADMIN_PASSWORD", "admin-password")
	t.Setenv("PILOTSERVER_PAIRING_TOKEN", "pairing-token")
}

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
	t.Setenv("PILOTSERVER_ADMIN_PASSWORD", "admin")

	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ListenAddr != "127.0.0.1:8080" {
		t.Fatalf("listen: %s", cfg.ListenAddr)
	}
}

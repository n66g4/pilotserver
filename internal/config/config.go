package config

import (
	"fmt"
	"net"
	"os"
)

type Config struct {
	ListenAddr       string
	DataDir          string
	PublicBaseURL    string
	JWTSecret        string
	AdminPassword    string // 明文仅用于首次启动哈希；生产用环境变量
	PairingToken     string
	SSHTunnelPortMin int
	SSHTunnelPortMax int
}

func Load() (Config, error) {
	cfg := Config{
		ListenAddr:       envOr("PILOTSERVER_LISTEN", "127.0.0.1:8080"),
		DataDir:          envOr("PILOTSERVER_DATA_DIR", "./data"),
		PublicBaseURL:    os.Getenv("PILOTSERVER_PUBLIC_BASE_URL"),
		JWTSecret:        os.Getenv("PILOTSERVER_JWT_SECRET"),
		AdminPassword:    os.Getenv("PILOTSERVER_ADMIN_PASSWORD"),
		PairingToken:     os.Getenv("PILOTSERVER_PAIRING_TOKEN"),
		SSHTunnelPortMin: 41000,
		SSHTunnelPortMax: 41099,
	}
	if cfg.PublicBaseURL == "" {
		return cfg, fmt.Errorf("PILOTSERVER_PUBLIC_BASE_URL required")
	}
	if len(cfg.JWTSecret) < 32 {
		return cfg, fmt.Errorf("PILOTSERVER_JWT_SECRET must be >= 32 bytes")
	}
	if len(cfg.AdminPassword) < 8 {
		return cfg, fmt.Errorf("PILOTSERVER_ADMIN_PASSWORD must be >= 8 bytes")
	}
	if cfg.PairingToken != "" && len(cfg.PairingToken) < 8 {
		return cfg, fmt.Errorf("PILOTSERVER_PAIRING_TOKEN must be empty or >= 8 bytes")
	}
	host, _, err := net.SplitHostPort(cfg.ListenAddr)
	if err != nil {
		return cfg, fmt.Errorf("invalid PILOTSERVER_LISTEN: %w", err)
	}
	ip := net.ParseIP(host)
	if (ip == nil || !ip.IsLoopback()) && os.Getenv("PILOTSERVER_ALLOW_NON_LOOPBACK") != "1" {
		return cfg, fmt.Errorf("PILOTSERVER_LISTEN must be loopback unless PILOTSERVER_ALLOW_NON_LOOPBACK=1")
	}
	return cfg, nil
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

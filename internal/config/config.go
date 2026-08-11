package config

import (
	"fmt"
	"os"
)

type Config struct {
	ListenAddr       string
	DataDir          string
	PublicBaseURL    string
	JWTSecret        string
	AdminPassword    string // 明文仅用于首次启动哈希；生产用环境变量
	SSHTunnelPortMin int
	SSHTunnelPortMax int
}

func Load() (Config, error) {
	cfg := Config{
		ListenAddr:       envOr("PILOTSERVER_LISTEN", "127.0.0.1:8080"),
		DataDir:          envOr("PILOTSERVER_DATA_DIR", "./data"),
		PublicBaseURL:    os.Getenv("PILOTSERVER_PUBLIC_BASE_URL"),
		JWTSecret:        os.Getenv("PILOTSERVER_JWT_SECRET"),
		AdminPassword:    envOr("PILOTSERVER_ADMIN_PASSWORD", "changeme"),
		SSHTunnelPortMin: 41000,
		SSHTunnelPortMax: 41099,
	}
	if cfg.PublicBaseURL == "" {
		return cfg, fmt.Errorf("PILOTSERVER_PUBLIC_BASE_URL required")
	}
	if len(cfg.JWTSecret) < 32 {
		return cfg, fmt.Errorf("PILOTSERVER_JWT_SECRET must be >= 32 bytes")
	}
	return cfg, nil
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

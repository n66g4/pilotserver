package config

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
)

const DefaultListenAddr = "127.0.0.1:18780"

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
		ListenAddr:       envOr("PILOTSERVER_LISTEN", DefaultListenAddr),
		DataDir:          envOr("PILOTSERVER_DATA_DIR", "./data"),
		PublicBaseURL:    strings.TrimSpace(os.Getenv("PILOTSERVER_PUBLIC_BASE_URL")),
		JWTSecret:        os.Getenv("PILOTSERVER_JWT_SECRET"),
		AdminPassword:    os.Getenv("PILOTSERVER_ADMIN_PASSWORD"),
		PairingToken:     os.Getenv("PILOTSERVER_PAIRING_TOKEN"),
		SSHTunnelPortMin: 41000,
		SSHTunnelPortMax: 41099,
	}
	// PublicBaseURL may be empty at boot; configure via admin UI or install wizard.
	if len(cfg.JWTSecret) < 32 {
		return cfg, fmt.Errorf("PILOTSERVER_JWT_SECRET must be >= 32 bytes")
	}
	if len(cfg.AdminPassword) < 8 {
		return cfg, fmt.Errorf("PILOTSERVER_ADMIN_PASSWORD must be >= 8 bytes")
	}
	if cfg.PairingToken != "" && len(cfg.PairingToken) < 8 {
		return cfg, fmt.Errorf("PILOTSERVER_PAIRING_TOKEN must be empty or >= 8 bytes")
	}
	if err := ValidateListenAddr(cfg.ListenAddr, AllowNonLoopback()); err != nil {
		return cfg, err
	}
	return cfg, nil
}

func AllowNonLoopback() bool {
	return os.Getenv("PILOTSERVER_ALLOW_NON_LOOPBACK") == "1"
}

func ValidateListenAddr(addr string, allowNonLoopback bool) error {
	host, _, err := net.SplitHostPort(strings.TrimSpace(addr))
	if err != nil {
		return fmt.Errorf("invalid listen address: %w", err)
	}
	ip := net.ParseIP(host)
	if (ip == nil || !ip.IsLoopback()) && !allowNonLoopback {
		return fmt.Errorf("listen address must be loopback unless PILOTSERVER_ALLOW_NON_LOOPBACK=1")
	}
	return nil
}

func EnvFilePath(dataDir string) string {
	if v := os.Getenv("PILOTSERVER_ENV_FILE"); v != "" {
		return v
	}
	return filepath.Join(dataDir, "pilotserver.env")
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

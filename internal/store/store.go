package store

import (
	"database/sql"
	"embed"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schemaFS embed.FS

type Device struct {
	DongleID     string
	PublicKeyPEM string
	Alias        string
	CreatedAt    int64
}

type Store struct {
	db *sql.DB
}

func Open(dataDir string) (*Store, error) {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}

	dbPath := filepath.Join(dataDir, "pilotserver.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}

	schema, err := schemaFS.ReadFile("schema.sql")
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("read schema: %w", err)
	}
	if _, err := db.Exec(string(schema)); err != nil {
		db.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}
	if err := initRoutes(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("apply routes schema: %w", err)
	}

	return &Store{db: db}, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) UpsertDevice(d Device) error {
	_, err := s.db.Exec(
		`INSERT INTO devices (dongle_id, public_key_pem, alias, created_at)
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT(dongle_id) DO UPDATE SET
		   public_key_pem = excluded.public_key_pem,
		   alias = excluded.alias,
		   created_at = excluded.created_at`,
		d.DongleID, d.PublicKeyPEM, d.Alias, d.CreatedAt,
	)
	return err
}

func (s *Store) GetDevice(dongleID string) (Device, error) {
	var d Device
	err := s.db.QueryRow(
		`SELECT dongle_id, public_key_pem, alias, created_at FROM devices WHERE dongle_id = ?`,
		dongleID,
	).Scan(&d.DongleID, &d.PublicKeyPEM, &d.Alias, &d.CreatedAt)
	return d, err
}

func (s *Store) ListDevices() ([]Device, error) {
	rows, err := s.db.Query(
		`SELECT dongle_id, public_key_pem, alias, created_at FROM devices ORDER BY created_at ASC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var devices []Device
	for rows.Next() {
		var d Device
		if err := rows.Scan(&d.DongleID, &d.PublicKeyPEM, &d.Alias, &d.CreatedAt); err != nil {
			return nil, err
		}
		devices = append(devices, d)
	}
	return devices, rows.Err()
}

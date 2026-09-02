package store

import (
	"database/sql"
	"time"
)

const sshAuditKeep = 1000

type SSHAudit struct {
	ID        int64  `json:"id"`
	DongleID  string `json:"dongle_id"`
	Action    string `json:"action"`
	Port      int    `json:"port"`
	CreatedAt int64  `json:"created_at"`
}

func (s *Store) initSSHAudit() error {
	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS ssh_audit (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			dongle_id TEXT NOT NULL,
			action TEXT NOT NULL,
			port INTEGER NOT NULL,
			created_at INTEGER NOT NULL
		)`)
	return err
}

func (s *Store) InsertSSHAudit(entry SSHAudit) error {
	if entry.CreatedAt == 0 {
		entry.CreatedAt = time.Now().Unix()
	}
	if _, err := s.db.Exec(
		`INSERT INTO ssh_audit (dongle_id, action, port, created_at) VALUES (?, ?, ?, ?)`,
		entry.DongleID, entry.Action, entry.Port, entry.CreatedAt,
	); err != nil {
		return err
	}
	var cutoff int64
	err := s.db.QueryRow(
		`SELECT id FROM ssh_audit ORDER BY id DESC LIMIT 1 OFFSET ?`,
		sshAuditKeep,
	).Scan(&cutoff)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`DELETE FROM ssh_audit WHERE id <= ?`, cutoff)
	return err
}

func (s *Store) ListSSHAudit(limit int) ([]SSHAudit, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	rows, err := s.db.Query(
		`SELECT id, dongle_id, action, port, created_at
		 FROM ssh_audit ORDER BY id DESC LIMIT ?`,
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	entries := make([]SSHAudit, 0)
	for rows.Next() {
		var entry SSHAudit
		if err := rows.Scan(&entry.ID, &entry.DongleID, &entry.Action, &entry.Port, &entry.CreatedAt); err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}

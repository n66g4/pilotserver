package store

import (
	"database/sql"
	"time"
)

const routesSchema = `
CREATE TABLE IF NOT EXISTS routes (
  dongle_id TEXT NOT NULL,
  name TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  PRIMARY KEY (dongle_id, name)
);
CREATE TABLE IF NOT EXISTS segments (
  dongle_id TEXT NOT NULL,
  route_name TEXT NOT NULL,
  segment_name TEXT NOT NULL,
  rel_path TEXT NOT NULL,
  size INTEGER NOT NULL,
  uploaded_at INTEGER NOT NULL,
  PRIMARY KEY (dongle_id, rel_path)
);`

type Route struct {
	DongleID  string `json:"dongle_id"`
	Name      string `json:"route"`
	CreatedAt int64  `json:"created_at"`
}

type Segment struct {
	DongleID    string
	RouteName   string
	SegmentName string
	RelPath     string
	Size        int64
	UploadedAt  int64
}

func initRoutes(db *sql.DB) error {
	_, err := db.Exec(routesSchema)
	return err
}

func (s *Store) InsertSegment(segment Segment) error {
	if segment.UploadedAt == 0 {
		segment.UploadedAt = time.Now().Unix()
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(
		`INSERT INTO routes (dongle_id, name, created_at) VALUES (?, ?, ?)
		 ON CONFLICT(dongle_id, name) DO NOTHING`,
		segment.DongleID, segment.RouteName, segment.UploadedAt,
	); err != nil {
		return err
	}
	if _, err := tx.Exec(
		`INSERT INTO segments (dongle_id, route_name, segment_name, rel_path, size, uploaded_at)
		 VALUES (?, ?, ?, ?, ?, ?)
		 ON CONFLICT(dongle_id, rel_path) DO UPDATE SET
		   route_name = excluded.route_name,
		   segment_name = excluded.segment_name,
		   size = excluded.size,
		   uploaded_at = excluded.uploaded_at`,
		segment.DongleID, segment.RouteName, segment.SegmentName,
		segment.RelPath, segment.Size, segment.UploadedAt,
	); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) ListRoutes(dongleID string) ([]Route, error) {
	rows, err := s.db.Query(
		`SELECT dongle_id, name, created_at
		 FROM routes WHERE dongle_id = ? ORDER BY created_at DESC, name DESC`,
		dongleID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	routes := make([]Route, 0)
	for rows.Next() {
		var route Route
		if err := rows.Scan(&route.DongleID, &route.Name, &route.CreatedAt); err != nil {
			return nil, err
		}
		routes = append(routes, route)
	}
	return routes, rows.Err()
}

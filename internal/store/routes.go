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
	DongleID    string `json:"dongle_id"`
	RouteName   string `json:"route"`
	SegmentName string `json:"segment"`
	RelPath     string `json:"path"`
	Size        int64  `json:"size"`
	UploadedAt  int64  `json:"uploaded_at"`
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

func (s *Store) ListSegments(dongleID, routeName string) ([]Segment, error) {
	rows, err := s.db.Query(
		`SELECT dongle_id, route_name, segment_name, rel_path, size, uploaded_at
		 FROM segments WHERE dongle_id = ? AND route_name = ?
		 ORDER BY segment_name ASC, rel_path ASC`,
		dongleID, routeName,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	segments := make([]Segment, 0)
	for rows.Next() {
		var segment Segment
		if err := rows.Scan(
			&segment.DongleID, &segment.RouteName, &segment.SegmentName,
			&segment.RelPath, &segment.Size, &segment.UploadedAt,
		); err != nil {
			return nil, err
		}
		segments = append(segments, segment)
	}
	return segments, rows.Err()
}

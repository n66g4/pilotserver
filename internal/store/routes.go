package store

import (
	"database/sql"
	"strings"
	"time"

	"pilotserver/internal/routepath"
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

func migrateRouteMetadata(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	rows, err := tx.Query(`
		SELECT s.dongle_id, s.route_name, s.segment_name, s.rel_path, s.uploaded_at,
		       COALESCE((
		         SELECT r.created_at FROM routes r
		         WHERE r.dongle_id = s.dongle_id AND r.name = s.route_name
		       ), s.uploaded_at)
		FROM segments s`)
	if err != nil {
		return err
	}
	type change struct {
		dongleID     string
		oldRouteName string
		routeName    string
		segmentName  string
		relPath      string
		uploadedAt   int64
		createdAt    int64
	}
	var changes []change
	for rows.Next() {
		var currentRoute, currentSegment string
		var candidate change
		if err := rows.Scan(
			&candidate.dongleID, &currentRoute, &currentSegment,
			&candidate.relPath, &candidate.uploadedAt, &candidate.createdAt,
		); err != nil {
			rows.Close()
			return err
		}
		parsed, ok := routepath.ParseSegmentFile(candidate.relPath)
		if !ok || strings.Count(candidate.relPath, "/") != 1 ||
			currentRoute == parsed.RouteName && currentSegment == parsed.SegmentName {
			continue
		}
		candidate.oldRouteName = currentRoute
		candidate.routeName = parsed.RouteName
		candidate.segmentName = parsed.SegmentName
		candidate.createdAt = min(candidate.createdAt, candidate.uploadedAt)
		changes = append(changes, candidate)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}

	for _, change := range changes {
		if _, err := tx.Exec(`
			INSERT INTO routes (dongle_id, name, created_at) VALUES (?, ?, ?)
			ON CONFLICT(dongle_id, name) DO UPDATE SET
			  created_at = MIN(routes.created_at, excluded.created_at)`,
			change.dongleID, change.routeName, change.createdAt,
		); err != nil {
			return err
		}
		if _, err := tx.Exec(`
			UPDATE segments SET route_name = ?, segment_name = ?
			WHERE dongle_id = ? AND rel_path = ?`,
			change.routeName, change.segmentName, change.dongleID, change.relPath,
		); err != nil {
			return err
		}
	}
	for _, change := range changes {
		if _, err := tx.Exec(`
			DELETE FROM routes
			WHERE dongle_id = ? AND name = ?
			  AND NOT EXISTS (
			    SELECT 1 FROM segments
			    WHERE segments.dongle_id = routes.dongle_id
			      AND segments.route_name = routes.name
			  )`,
			change.dongleID, change.oldRouteName,
		); err != nil {
			return err
		}
	}
	return tx.Commit()
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

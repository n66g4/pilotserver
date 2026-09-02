package store

import (
	"database/sql"
	"strconv"
	"time"
)

const (
	SettingUploadMaxBytes      = "upload_max_bytes"
	SettingUploadRetentionDays = "upload_retention_days"
)

type UploadPolicy struct {
	MaxBytes      int64
	RetentionDays int
}

func (s *Store) UploadPolicy() (UploadPolicy, error) {
	maxRaw, err := s.GetSetting(SettingUploadMaxBytes)
	if err != nil {
		return UploadPolicy{}, err
	}
	daysRaw, err := s.GetSetting(SettingUploadRetentionDays)
	if err != nil {
		return UploadPolicy{}, err
	}
	return UploadPolicy{
		MaxBytes:      parseNonNegInt64(maxRaw),
		RetentionDays: int(parseNonNegInt64(daysRaw)),
	}, nil
}

func (s *Store) SetUploadPolicy(policy UploadPolicy) error {
	if policy.MaxBytes < 0 {
		policy.MaxBytes = 0
	}
	if policy.RetentionDays < 0 {
		policy.RetentionDays = 0
	}
	if err := s.SetSetting(SettingUploadMaxBytes, strconv.FormatInt(policy.MaxBytes, 10)); err != nil {
		return err
	}
	return s.SetSetting(SettingUploadRetentionDays, strconv.Itoa(policy.RetentionDays))
}

func (s *Store) TotalUploadBytes() (int64, error) {
	var total int64
	err := s.db.QueryRow(`SELECT COALESCE(SUM(size), 0) FROM segments`).Scan(&total)
	return total, err
}

func (s *Store) ListSegmentsUploadedBefore(unix int64) ([]Segment, error) {
	rows, err := s.db.Query(
		`SELECT dongle_id, route_name, segment_name, rel_path, size, uploaded_at
		 FROM segments WHERE uploaded_at < ? ORDER BY uploaded_at ASC, rel_path ASC`,
		unix,
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

func (s *Store) OldestSegmentExcept(dongleID, relPath string) (Segment, bool, error) {
	var segment Segment
	err := s.db.QueryRow(
		`SELECT dongle_id, route_name, segment_name, rel_path, size, uploaded_at
		 FROM segments
		 WHERE NOT (dongle_id = ? AND rel_path = ?)
		 ORDER BY uploaded_at ASC, rel_path ASC LIMIT 1`,
		dongleID, relPath,
	).Scan(
		&segment.DongleID, &segment.RouteName, &segment.SegmentName,
		&segment.RelPath, &segment.Size, &segment.UploadedAt,
	)
	if err == sql.ErrNoRows {
		return Segment{}, false, nil
	}
	if err != nil {
		return Segment{}, false, err
	}
	return segment, true, nil
}

func (s *Store) DeleteSegment(dongleID, relPath string) error {
	if _, err := s.db.Exec(
		`DELETE FROM segments WHERE dongle_id = ? AND rel_path = ?`,
		dongleID, relPath,
	); err != nil {
		return err
	}
	_, err := s.db.Exec(`
		DELETE FROM routes
		WHERE dongle_id = ?
		  AND NOT EXISTS (
		    SELECT 1 FROM segments
		    WHERE segments.dongle_id = routes.dongle_id
		      AND segments.route_name = routes.name
		  )`,
		dongleID,
	)
	return err
}

func RetentionCutoff(now time.Time, days int) int64 {
	if days <= 0 {
		return 0
	}
	return now.AddDate(0, 0, -days).Unix()
}

func parseNonNegInt64(raw string) int64 {
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n < 0 {
		return 0
	}
	return n
}

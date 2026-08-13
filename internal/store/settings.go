package store

import "database/sql"

const (
	SettingPublicBaseURL   = "public_base_url"
	SettingMapProvider     = "map_provider"
	SettingMapWebKey       = "map_web_key"
	SettingMapSecurityCode = "map_security_code"
)

type MapSettings struct {
	Provider     string
	WebKey       string
	SecurityCode string
}

func (s *Store) initSettings() error {
	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS settings (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL
		)`)
	return err
}

func (s *Store) GetSetting(key string) (string, error) {
	var value string
	err := s.db.QueryRow(`SELECT value FROM settings WHERE key = ?`, key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return value, err
}

func (s *Store) SetSetting(key, value string) error {
	_, err := s.db.Exec(
		`INSERT INTO settings (key, value) VALUES (?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		key, value,
	)
	return err
}

func (s *Store) GetMapSettings() (MapSettings, error) {
	return readMapSettings(s.db)
}

type settingsQueryer interface {
	Query(query string, args ...any) (*sql.Rows, error)
}

func readMapSettings(queryer settingsQueryer) (MapSettings, error) {
	result := MapSettings{Provider: "none"}
	rows, err := queryer.Query(
		`SELECT key, value FROM settings WHERE key IN (?, ?, ?)`,
		SettingMapProvider, SettingMapWebKey, SettingMapSecurityCode,
	)
	if err != nil {
		return MapSettings{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return MapSettings{}, err
		}
		switch key {
		case SettingMapProvider:
			if value != "" {
				result.Provider = value
			}
		case SettingMapWebKey:
			result.WebKey = value
		case SettingMapSecurityCode:
			result.SecurityCode = value
		}
	}
	return result, rows.Err()
}

func (s *Store) SetMapSettings(settings MapSettings) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := writeMapSettings(tx, settings); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) UpdateMapSettings(update func(MapSettings) (MapSettings, error)) (MapSettings, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return MapSettings{}, err
	}
	defer tx.Rollback()

	settings, err := readMapSettings(tx)
	if err != nil {
		return MapSettings{}, err
	}
	settings, err = update(settings)
	if err != nil {
		return MapSettings{}, err
	}
	if err := writeMapSettings(tx, settings); err != nil {
		return MapSettings{}, err
	}
	if err := tx.Commit(); err != nil {
		return MapSettings{}, err
	}
	return settings, nil
}

func writeMapSettings(tx *sql.Tx, settings MapSettings) error {
	for _, setting := range []struct {
		key   string
		value string
	}{
		{SettingMapProvider, settings.Provider},
		{SettingMapWebKey, settings.WebKey},
		{SettingMapSecurityCode, settings.SecurityCode},
	} {
		if _, err := tx.Exec(
			`INSERT INTO settings (key, value) VALUES (?, ?)
			 ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
			setting.key, setting.value,
		); err != nil {
			return err
		}
	}
	return nil
}

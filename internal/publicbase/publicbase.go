package publicbase

import (
	"fmt"
	"net/url"
	"strings"
	"sync"

	"pilotserver/internal/store"
)

// Resolver resolves the public HTTPS base URL.
// Priority: SQLite setting (admin/wizard-seeded) over process env fallback.
type Resolver struct {
	mu       sync.RWMutex
	store    *store.Store
	envFallback string
}

func New(st *store.Store, envFallback string) (*Resolver, error) {
	r := &Resolver{store: st, envFallback: strings.TrimSpace(envFallback)}
	if r.envFallback != "" {
		normalized, err := Normalize(r.envFallback)
		if err != nil {
			return nil, err
		}
		r.envFallback = normalized
		current, err := st.GetSetting(store.SettingPublicBaseURL)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(current) == "" {
			if err := st.SetSetting(store.SettingPublicBaseURL, normalized); err != nil {
				return nil, err
			}
		}
	}
	return r, nil
}

func (r *Resolver) Get() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.store != nil {
		if value, err := r.store.GetSetting(store.SettingPublicBaseURL); err == nil {
			if v := strings.TrimSpace(value); v != "" {
				return v
			}
		}
	}
	return r.envFallback
}

func (r *Resolver) Set(raw string) error {
	normalized, err := Normalize(raw)
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.store.SetSetting(store.SettingPublicBaseURL, normalized); err != nil {
		return err
	}
	return nil
}

func Normalize(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("public base URL is empty")
	}
	// Users often omit the scheme (e.g. "xxx.synology.me"); assume https.
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("public base URL must be absolute http(s) URL")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("public base URL scheme must be http or https")
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String(), nil
}

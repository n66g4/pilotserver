package listenaddr

import (
	"strings"
	"sync"

	"pilotserver/internal/config"
	"pilotserver/internal/envfile"
)

// Resolver holds the effective listen address and the allow-non-loopback flag.
// The env file is the single source of truth: the install wizard, manual edits
// and the admin UI all write it, and the start script sources it at boot.
type Resolver struct {
	mu               sync.RWMutex
	current          string
	envFile          string
	allowNonLoopback bool
	onChange         func(newAddr string)
}

func New(envValue, envFile string, allowNonLoopback bool, onChange func(string)) (*Resolver, error) {
	current := strings.TrimSpace(envValue)
	if current == "" {
		current = config.DefaultListenAddr
	}
	if err := config.ValidateListenAddr(current, allowNonLoopback); err != nil {
		return nil, err
	}
	return &Resolver{
		current:          current,
		envFile:          envFile,
		allowNonLoopback: allowNonLoopback,
		onChange:         onChange,
	}, nil
}

func (r *Resolver) Get() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.current
}

func (r *Resolver) Set(raw string) (changed bool, err error) {
	normalized := strings.TrimSpace(raw)
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := config.ValidateListenAddr(normalized, r.allowNonLoopback); err != nil {
		return false, err
	}
	if r.envFile != "" {
		if err := envfile.Upsert(r.envFile, "PILOTSERVER_LISTEN", normalized); err != nil {
			return false, err
		}
	}
	if r.current == normalized {
		return false, nil
	}
	r.current = normalized
	if r.onChange != nil {
		r.onChange(normalized)
	}
	return true, nil
}

func (r *Resolver) AllowNonLoopback() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.allowNonLoopback
}

// SetAllowNonLoopback persists the flag; when disabling with a non-loopback
// address active, the listener falls back to the default loopback address.
func (r *Resolver) SetAllowNonLoopback(allow bool) (listenChanged bool, err error) {
	r.mu.Lock()
	value := "0"
	if allow {
		value = "1"
	}
	if r.envFile != "" {
		if err := envfile.Upsert(r.envFile, "PILOTSERVER_ALLOW_NON_LOOPBACK", value); err != nil {
			r.mu.Unlock()
			return false, err
		}
	}
	r.allowNonLoopback = allow
	needFallback := !allow && config.ValidateListenAddr(r.current, false) != nil
	r.mu.Unlock()

	if needFallback {
		return r.Set(config.DefaultListenAddr)
	}
	return false, nil
}

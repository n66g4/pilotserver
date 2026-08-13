package adminapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"

	"pilotserver/internal/config"
	"pilotserver/internal/listenaddr"
	"pilotserver/internal/publicbase"
	"pilotserver/internal/store"
)

var settingsUpdateMu sync.Mutex

type settingsResponse struct {
	PublicBaseURL   string `json:"public_base_url"`
	ListenAddr      string `json:"listen_addr"`
	AllowLAN        bool   `json:"allow_lan"`
	Configured      bool   `json:"configured"`
	ListenChanged   bool   `json:"listen_changed"`
	MapProvider     string `json:"map_provider"`
	MapWebKey       string `json:"map_web_key"`
	MapSecurityCode string `json:"map_security_code"`
}

func currentSettings(st *store.Store, baseURL *publicbase.Resolver, listen *listenaddr.Resolver) (settingsResponse, error) {
	response := settingsResponse{}
	if baseURL != nil {
		response.PublicBaseURL = baseURL.Get()
	}
	if listen != nil {
		response.ListenAddr = listen.Get()
		response.AllowLAN = listen.AllowNonLoopback()
	}
	response.Configured = response.PublicBaseURL != ""
	if st == nil {
		return response, errors.New("settings unavailable")
	}
	mapSettings, err := st.GetMapSettings()
	if err != nil {
		return response, err
	}
	response.MapProvider = mapSettings.Provider
	response.MapWebKey = mapSettings.WebKey
	response.MapSecurityCode = mapSettings.SecurityCode
	return response, nil
}

func handleGetSettings(w http.ResponseWriter, st *store.Store, baseURL *publicbase.Resolver, listen *listenaddr.Resolver) {
	response, err := currentSettings(st, baseURL, listen)
	if err != nil {
		http.Error(w, "read settings", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}

func handlePutSettings(w http.ResponseWriter, r *http.Request, st *store.Store, baseURL *publicbase.Resolver, listen *listenaddr.Resolver) {
	var request struct {
		PublicBaseURL   *string `json:"public_base_url"`
		ListenAddr      *string `json:"listen_addr"`
		AllowLAN        *bool   `json:"allow_lan"`
		MapProvider     *string `json:"map_provider"`
		MapWebKey       *string `json:"map_web_key"`
		MapSecurityCode *string `json:"map_security_code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	settingsUpdateMu.Lock()
	defer settingsUpdateMu.Unlock()

	var nextMap store.MapSettings
	if request.MapProvider != nil || request.MapWebKey != nil || request.MapSecurityCode != nil {
		if st == nil {
			http.Error(w, "settings unavailable", http.StatusInternalServerError)
			return
		}
		var err error
		nextMap, err = st.GetMapSettings()
		if err != nil {
			http.Error(w, "read settings", http.StatusInternalServerError)
			return
		}
		nextMap = mergeMapSettings(nextMap, request.MapProvider, request.MapWebKey, request.MapSecurityCode)
		if err := validateMapSettings(nextMap); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}

	nextPublicBase := ""
	if request.PublicBaseURL != nil {
		if baseURL == nil {
			http.Error(w, "settings unavailable", http.StatusInternalServerError)
			return
		}
		var err error
		nextPublicBase, err = publicbase.Normalize(*request.PublicBaseURL)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}
	nextAllowLAN := false
	nextListenAddr := ""
	if request.AllowLAN != nil || request.ListenAddr != nil {
		if listen == nil {
			http.Error(w, "settings unavailable", http.StatusInternalServerError)
			return
		}
		nextAllowLAN = listen.AllowNonLoopback()
		nextListenAddr = listen.Get()
		if request.AllowLAN != nil {
			nextAllowLAN = *request.AllowLAN
		}
		if request.ListenAddr != nil {
			nextListenAddr = strings.TrimSpace(*request.ListenAddr)
		} else if !nextAllowLAN && config.ValidateListenAddr(nextListenAddr, false) != nil {
			nextListenAddr = config.DefaultListenAddr
		}
		if err := config.ValidateListenAddr(nextListenAddr, nextAllowLAN); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}

	if request.MapProvider != nil || request.MapWebKey != nil || request.MapSecurityCode != nil {
		_, err := st.UpdateMapSettings(func(settings store.MapSettings) (store.MapSettings, error) {
			settings = mergeMapSettings(settings, request.MapProvider, request.MapWebKey, request.MapSecurityCode)
			if err := validateMapSettings(settings); err != nil {
				return store.MapSettings{}, mapSettingsValidationError{message: err.Error()}
			}
			return settings, nil
		})
		if err != nil {
			var validationError mapSettingsValidationError
			if errors.As(err, &validationError) {
				http.Error(w, validationError.Error(), http.StatusBadRequest)
			} else {
				http.Error(w, "save settings", http.StatusInternalServerError)
			}
			return
		}
	}

	listenChanged := false
	if request.PublicBaseURL != nil {
		if err := baseURL.Set(nextPublicBase); err != nil {
			http.Error(w, "save settings", http.StatusInternalServerError)
			return
		}
	}
	// Apply the flag before listen_addr so "允许局域网 + 0.0.0.0" can be saved in one request.
	if request.AllowLAN != nil {
		changed, err := listen.SetAllowNonLoopback(*request.AllowLAN)
		if err != nil {
			http.Error(w, "save settings", http.StatusInternalServerError)
			return
		}
		listenChanged = listenChanged || changed
	}
	if request.ListenAddr != nil {
		changed, err := listen.Set(nextListenAddr)
		if err != nil {
			http.Error(w, "save settings", http.StatusInternalServerError)
			return
		}
		listenChanged = listenChanged || changed
	}
	response, err := currentSettings(st, baseURL, listen)
	if err != nil {
		http.Error(w, "read settings", http.StatusInternalServerError)
		return
	}
	response.ListenChanged = listenChanged
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}

func mergeMapSettings(settings store.MapSettings, provider, webKey, securityCode *string) store.MapSettings {
	if provider != nil {
		settings.Provider = strings.TrimSpace(*provider)
	}
	if webKey != nil {
		settings.WebKey = strings.TrimSpace(*webKey)
	}
	if securityCode != nil {
		settings.SecurityCode = strings.TrimSpace(*securityCode)
	}
	return settings
}

type mapSettingsValidationError struct {
	message string
}

func (e mapSettingsValidationError) Error() string {
	return e.message
}

func validateMapSettings(settings store.MapSettings) error {
	if settings.Provider != "none" && settings.Provider != "amap" && settings.Provider != "tencent" {
		return errors.New("invalid map provider")
	}
	if len(settings.WebKey) > 512 || len(settings.SecurityCode) > 512 {
		return errors.New("map credential is too long")
	}
	if settings.Provider != "none" && settings.WebKey == "" {
		return errors.New("map web key is required")
	}
	return nil
}

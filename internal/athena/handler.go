package athena

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/coder/websocket"

	"pilotserver/internal/auth"
	"pilotserver/internal/config"
	"pilotserver/internal/store"
)

type websocketConn struct {
	conn *websocket.Conn
}

func (c websocketConn) Send(msg []byte) error {
	return c.conn.Write(context.Background(), websocket.MessageText, msg)
}

func (c websocketConn) Close() error {
	return c.conn.Close(websocket.StatusNormalClosure, "")
}

func Mount(mux *http.ServeMux, st *store.Store, hub *Hub, cfg config.Config) {
	mux.HandleFunc("GET /ws/v2/{dongleID}", func(w http.ResponseWriter, r *http.Request) {
		handleWebSocket(w, r, hub, cfg.JWTSecret)
	})
	mux.HandleFunc("GET /admin/api/devices", func(w http.ResponseWriter, _ *http.Request) {
		handleDevices(w, st, hub)
	})
}

func handleWebSocket(w http.ResponseWriter, r *http.Request, hub *Hub, jwtSecret string) {
	dongleID := r.PathValue("dongleID")
	tokenDongleID, err := auth.ParseDeviceJWT(jwtSecret, tokenFromRequest(r))
	if err != nil || tokenDongleID != dongleID {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	session := hub.SetOnline(dongleID, websocketConn{conn: conn})
	defer func() {
		hub.SetOffline(dongleID, session)
		conn.CloseNow()
	}()

	for {
		if _, _, err := conn.Read(context.Background()); err != nil {
			return
		}
	}
}

func tokenFromRequest(r *http.Request) string {
	if cookie, err := r.Cookie("jwt"); err == nil && cookie.Value != "" {
		return cookie.Value
	}
	if token := r.URL.Query().Get("access_token"); token != "" {
		return token
	}
	parts := strings.Fields(r.Header.Get("Authorization"))
	if len(parts) == 2 &&
		(strings.EqualFold(parts[0], "JWT") || strings.EqualFold(parts[0], "Bearer")) {
		return parts[1]
	}
	return ""
}

func handleDevices(w http.ResponseWriter, st *store.Store, hub *Hub) {
	devices, err := st.ListDevices()
	if err != nil {
		http.Error(w, "list devices", http.StatusInternalServerError)
		return
	}
	response := make([]struct {
		DongleID string `json:"dongle_id"`
		Online   bool   `json:"online"`
	}, 0, len(devices))
	for _, device := range devices {
		response = append(response, struct {
			DongleID string `json:"dongle_id"`
			Online   bool   `json:"online"`
		}{
			DongleID: device.DongleID,
			Online:   hub.IsOnline(device.DongleID),
		})
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}

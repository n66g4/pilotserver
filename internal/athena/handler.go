package athena

import (
	"context"
	"net/http"
	"strings"

	"github.com/coder/websocket"

	"pilotserver/internal/auth"
	"pilotserver/internal/config"
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

func Mount(mux *http.ServeMux, hub *Hub, cfg config.Config) {
	hub.tunnelMu.Lock()
	hub.tunnelConfig = cfg
	hub.tunnelMu.Unlock()
	mux.HandleFunc("GET /ws/v2/{dongleID}", func(w http.ResponseWriter, r *http.Request) {
		handleWebSocket(w, r, hub, cfg.JWTSecret)
	})
	mux.HandleFunc("GET /ws/proxy/{ticket}", hub.handleProxyWebSocket)
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

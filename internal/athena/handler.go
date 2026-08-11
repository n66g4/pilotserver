package athena

import (
	"context"
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

func Mount(mux *http.ServeMux, hub *Hub, st *store.Store, cfg config.Config) {
	hub.tunnelMu.Lock()
	hub.tunnelConfig = cfg
	hub.tunnelMu.Unlock()
	mux.HandleFunc("GET /ws/v2/{dongleID}", func(w http.ResponseWriter, r *http.Request) {
		handleWebSocket(w, r, hub, st)
	})
	mux.HandleFunc("GET /ws/proxy/{ticket}", hub.handleProxyWebSocket)
}

func handleWebSocket(w http.ResponseWriter, r *http.Request, hub *Hub, st *store.Store) {
	dongleID := r.PathValue("dongleID")
	tokenDongleID, err := auth.VerifyDeviceJWT(tokenFromRequest(r), func(identity string) (string, error) {
		device, err := st.GetDevice(identity)
		return device.PublicKeyPEM, err
	})
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
		_, message, err := conn.Read(context.Background())
		if err != nil {
			return
		}
		hub.HandleJSONRPCResponse(message)
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

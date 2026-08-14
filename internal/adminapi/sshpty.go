package adminapi

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"

	"github.com/coder/websocket"

	"pilotserver/internal/athena"
	"pilotserver/internal/publicbase"
	"pilotserver/internal/sshkey"
	"pilotserver/internal/sshsession"
)

var openDeviceTunnel = defaultOpenDeviceTunnel

func defaultOpenDeviceTunnel(ctx context.Context, hub *athena.Hub, dongleID string) (addr string, port int, cancel func(), err error) {
	port, cancel, err = hub.OpenSSHTunnel(ctx, dongleID)
	if err != nil {
		return "", 0, nil, err
	}
	return net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), port, cancel, nil
}

func handleSSHPty(w http.ResponseWriter, r *http.Request, hub *athena.Hub, baseURL *publicbase.Resolver, dataDir string) {
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer conn.CloseNow()

	var size struct {
		Cols int `json:"cols"`
		Rows int `json:"rows"`
	}
	messageType, payload, err := conn.Read(r.Context())
	if err != nil || messageType != websocket.MessageText || json.Unmarshal(payload, &size) != nil {
		return
	}
	if size.Cols <= 0 {
		size.Cols = 80
	}
	if size.Rows <= 0 {
		size.Rows = 24
	}

	publicURL := ""
	if baseURL != nil {
		publicURL = baseURL.Get()
	}
	base, err := url.Parse(publicURL)
	if err != nil || base.Hostname() == "" {
		writeSSHPtyError(r.Context(), conn, "public_base_unconfigured")
		return
	}

	keyStore, err := sshkey.Open(dataDir)
	if err != nil {
		writeSSHPtyError(r.Context(), conn, "tunnel_failed")
		return
	}
	signer, err := keyStore.Signer()
	if errors.Is(err, os.ErrNotExist) {
		writeSSHPtyError(r.Context(), conn, "key_unconfigured")
		return
	}
	if err != nil {
		writeSSHPtyError(r.Context(), conn, "tunnel_failed")
		return
	}

	addr, port, cancelTunnel, err := openDeviceTunnel(r.Context(), hub, r.PathValue("dongleID"))
	if errors.Is(err, athena.ErrOffline) {
		writeSSHPtyError(r.Context(), conn, "offline")
		return
	}
	if err != nil {
		writeSSHPtyError(r.Context(), conn, "tunnel_failed")
		return
	}
	if cancelTunnel != nil {
		defer cancelTunnel()
	}

	session, err := sshsession.Connect(r.Context(), addr, signer, size.Cols, size.Rows)
	if errors.Is(err, sshsession.ErrAuthFailed) {
		writeSSHPtyError(r.Context(), conn, "auth_failed")
		return
	}
	if err != nil {
		writeSSHPtyError(r.Context(), conn, "tunnel_failed")
		return
	}

	opened, err := json.Marshal(struct {
		Host      string `json:"host"`
		Port      int    `json:"port"`
		ExpiresIn int    `json:"expires_in"`
	}{
		Host:      base.Hostname(),
		Port:      port,
		ExpiresIn: 600,
	})
	if err != nil || conn.Write(r.Context(), websocket.MessageText, opened) != nil {
		session.Close()
		return
	}

	stdoutDone := make(chan struct{})
	go func() {
		defer close(stdoutDone)
		buffer := make([]byte, 32*1024)
		for {
			n, readErr := session.Stdout().Read(buffer)
			if n > 0 {
				if err := conn.Write(r.Context(), websocket.MessageBinary, buffer[:n]); err != nil {
					return
				}
			}
			if readErr != nil {
				writeSSHPtyError(r.Context(), conn, "tunnel_failed")
				conn.CloseNow()
				return
			}
		}
	}()

	for {
		messageType, payload, err := conn.Read(r.Context())
		if err != nil {
			break
		}
		switch messageType {
		case websocket.MessageBinary:
			if _, err := session.Stdin().Write(payload); err != nil {
				writeSSHPtyError(r.Context(), conn, "tunnel_failed")
				conn.CloseNow()
			}
		case websocket.MessageText:
			var resize struct {
				Cols int `json:"cols"`
				Rows int `json:"rows"`
			}
			if json.Unmarshal(payload, &resize) == nil && resize.Cols > 0 && resize.Rows > 0 {
				_ = session.Resize(resize.Cols, resize.Rows)
			}
		}
	}
	_ = session.Close()
	<-stdoutDone
}

func writeSSHPtyError(ctx context.Context, conn *websocket.Conn, code string) {
	payload, err := json.Marshal(struct {
		Error string `json:"error"`
	}{Error: code})
	if err == nil {
		_ = conn.Write(ctx, websocket.MessageText, payload)
	}
}

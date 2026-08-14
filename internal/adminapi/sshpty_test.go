package adminapi

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"golang.org/x/crypto/ssh"

	"pilotserver/internal/athena"
	"pilotserver/internal/auth"
	"pilotserver/internal/config"
	"pilotserver/internal/listenaddr"
	"pilotserver/internal/publicbase"
	"pilotserver/internal/replay"
	"pilotserver/internal/sshkey"
	"pilotserver/internal/store"
)

const ptyTestSecret = "pty-test-secret-at-least-thirty-two-bytes"

func TestSSHPtyRequiresAuthentication(t *testing.T) {
	server, _ := newPTYHTTPServer(t, config.Config{
		JWTSecret:     ptyTestSecret,
		DataDir:       t.TempDir(),
		PublicBaseURL: "https://admin.example.com",
	})

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, response, err := websocket.Dial(ctx, ptyWebSocketURL(server.URL, ""), nil)
	if err == nil {
		t.Fatal("websocket dial succeeded without JWT")
	}
	if response == nil || response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %v, want %d", responseStatus(response), http.StatusUnauthorized)
	}
}

func TestSSHPtyReportsOfflineDevice(t *testing.T) {
	setTunnelOpener(t, func(context.Context, *athena.Hub, string) (string, int, func(), error) {
		return "", 0, nil, athena.ErrOffline
	})
	server, token := newPTYHTTPServer(t, config.Config{
		JWTSecret:     ptyTestSecret,
		DataDir:       t.TempDir(),
		PublicBaseURL: "https://admin.example.com",
	})

	conn := dialPTY(t, server.URL, token)
	defer conn.CloseNow()
	writePTYSize(t, conn)
	assertPTYError(t, conn, "offline")
}

func TestSSHPtyReportsMissingPublicBase(t *testing.T) {
	setTunnelOpener(t, func(context.Context, *athena.Hub, string) (string, int, func(), error) {
		return "", 0, func() {}, nil
	})
	server, token := newPTYHTTPServer(t, config.Config{
		JWTSecret: ptyTestSecret,
		DataDir:   t.TempDir(),
	})

	conn := dialPTY(t, server.URL, token)
	defer conn.CloseNow()
	writePTYSize(t, conn)
	assertPTYError(t, conn, "public_base_unconfigured")
}

func TestSSHPtyReportsTunnelFailure(t *testing.T) {
	setTunnelOpener(t, func(context.Context, *athena.Hub, string) (string, int, func(), error) {
		return "", 0, nil, errors.New("tunnel unavailable")
	})
	server, token := newPTYHTTPServer(t, config.Config{
		JWTSecret:     ptyTestSecret,
		DataDir:       t.TempDir(),
		PublicBaseURL: "https://admin.example.com",
	})

	conn := dialPTY(t, server.URL, token)
	defer conn.CloseNow()
	writePTYSize(t, conn)
	assertPTYError(t, conn, "tunnel_failed")
}

func TestSSHPtyConnectsUsingStoredKey(t *testing.T) {
	dataDir := t.TempDir()
	keyStore, err := sshkey.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := keyStore.Signer()
	if err != nil {
		t.Fatal(err)
	}
	sshAddr := startPTYSSHServer(t, signer.PublicKey(), false)
	_, portText, err := net.SplitHostPort(sshAddr)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}
	tunnelCanceled := make(chan struct{})
	setTunnelOpener(t, func(context.Context, *athena.Hub, string) (string, int, func(), error) {
		return sshAddr, port, func() { close(tunnelCanceled) }, nil
	})
	server, token := newPTYHTTPServer(t, config.Config{
		JWTSecret:     ptyTestSecret,
		DataDir:       dataDir,
		PublicBaseURL: "https://admin.example.com",
	})

	conn := dialPTY(t, server.URL, token)
	defer conn.CloseNow()
	writePTYSize(t, conn)

	messageType, payload := readPTYMessage(t, conn)
	if messageType != websocket.MessageText {
		t.Fatalf("success message type = %v, want text", messageType)
	}
	var opened struct {
		Host      string `json:"host"`
		Port      int    `json:"port"`
		ExpiresIn int    `json:"expires_in"`
	}
	if err := json.Unmarshal(payload, &opened); err != nil {
		t.Fatal(err)
	}
	if opened.Host != "admin.example.com" || opened.Port != port || opened.ExpiresIn != 600 {
		t.Fatalf("success response = %+v", opened)
	}

	messageType, payload = readPTYMessage(t, conn)
	if messageType != websocket.MessageBinary {
		t.Fatalf("PTY message type = %v, want binary", messageType)
	}
	if string(payload) != "ready\n" {
		t.Fatalf("PTY greeting = %q, want %q", payload, "ready\n")
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := conn.Write(ctx, websocket.MessageBinary, []byte("echo me")); err != nil {
		t.Fatal(err)
	}
	messageType, payload = readPTYMessage(t, conn)
	if messageType != websocket.MessageBinary || string(payload) != "echo me" {
		t.Fatalf("PTY echo = (%v, %q), want binary %q", messageType, payload, "echo me")
	}
	if err := conn.Write(ctx, websocket.MessageText, []byte(`{"cols":40,"rows":12}`)); err != nil {
		t.Fatal(err)
	}
	if err := conn.Close(websocket.StatusNormalClosure, "test complete"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-tunnelCanceled:
	case <-ctx.Done():
		t.Fatal("tunnel cancel was not called after WebSocket close")
	}
}

func TestSSHPtyReportsRemoteShellExit(t *testing.T) {
	dataDir := t.TempDir()
	keyStore, err := sshkey.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := keyStore.Signer()
	if err != nil {
		t.Fatal(err)
	}
	sshAddr := startPTYSSHServer(t, signer.PublicKey(), true)
	_, portText, err := net.SplitHostPort(sshAddr)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}
	setTunnelOpener(t, func(context.Context, *athena.Hub, string) (string, int, func(), error) {
		return sshAddr, port, func() {}, nil
	})
	server, token := newPTYHTTPServer(t, config.Config{
		JWTSecret:     ptyTestSecret,
		DataDir:       dataDir,
		PublicBaseURL: "https://admin.example.com",
	})

	conn := dialPTY(t, server.URL, token)
	defer conn.CloseNow()
	writePTYSize(t, conn)
	readPTYMessage(t, conn)
	messageType, payload := readPTYMessage(t, conn)
	if messageType != websocket.MessageBinary || string(payload) != "ready\n" {
		t.Fatalf("PTY greeting = (%v, %q), want binary %q", messageType, payload, "ready\n")
	}
	assertPTYError(t, conn, "tunnel_failed")
}

func TestSSHPtyReportsRejectedStoredKey(t *testing.T) {
	sshAddr := startPTYSSHServer(t, newPTYSigner(t).PublicKey(), false)
	_, portText, err := net.SplitHostPort(sshAddr)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}
	setTunnelOpener(t, func(context.Context, *athena.Hub, string) (string, int, func(), error) {
		return sshAddr, port, func() {}, nil
	})
	server, token := newPTYHTTPServer(t, config.Config{
		JWTSecret:     ptyTestSecret,
		DataDir:       t.TempDir(),
		PublicBaseURL: "https://admin.example.com",
	})

	conn := dialPTY(t, server.URL, token)
	defer conn.CloseNow()
	writePTYSize(t, conn)
	assertPTYError(t, conn, "auth_failed")
}

func newPTYHTTPServer(t *testing.T, cfg config.Config) (*httptest.Server, string) {
	t.Helper()
	st, err := store.Open(cfg.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	baseURL, err := publicbase.New(st, cfg.PublicBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	listen, err := listenaddr.New(cfg.ListenAddr, "", config.AllowNonLoopback(), nil)
	if err != nil {
		t.Fatal(err)
	}
	hub := athena.NewHub(cfg)
	hub.SetBaseURLProvider(baseURL.Get)
	tickets := replay.NewTicketManager(cfg.JWTSecret, time.Minute)
	mux := http.NewServeMux()
	Mount(mux, st, hub, cfg, "", baseURL, listen, replay.NewService(st, tickets))
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	token, err := auth.IssueAdminJWT(cfg.JWTSecret, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	return server, token
}

func setTunnelOpener(t *testing.T, opener func(context.Context, *athena.Hub, string) (string, int, func(), error)) {
	t.Helper()
	old := openDeviceTunnel
	openDeviceTunnel = opener
	t.Cleanup(func() { openDeviceTunnel = old })
}

func ptyWebSocketURL(serverURL, token string) string {
	target := "ws" + strings.TrimPrefix(serverURL, "http") + "/admin/api/devices/d1/ssh/pty"
	if token != "" {
		target += "?access_token=" + url.QueryEscape(token)
	}
	return target
}

func dialPTY(t *testing.T, serverURL, token string) *websocket.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	t.Cleanup(cancel)
	conn, response, err := websocket.Dial(ctx, ptyWebSocketURL(serverURL, token), nil)
	if err != nil {
		t.Fatalf("websocket dial: %v (status %v)", err, responseStatus(response))
	}
	return conn
}

func writePTYSize(t *testing.T, conn *websocket.Conn) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := conn.Write(ctx, websocket.MessageText, []byte(`{"cols":80,"rows":24}`)); err != nil {
		t.Fatal(err)
	}
}

func readPTYMessage(t *testing.T, conn *websocket.Conn) (websocket.MessageType, []byte) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	messageType, payload, err := conn.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return messageType, payload
}

func assertPTYError(t *testing.T, conn *websocket.Conn, want string) {
	t.Helper()
	messageType, payload := readPTYMessage(t, conn)
	if messageType != websocket.MessageText {
		t.Fatalf("error message type = %v, want text", messageType)
	}
	var response struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(payload, &response); err != nil {
		t.Fatal(err)
	}
	if response.Error != want {
		t.Fatalf("error = %q, want %q", response.Error, want)
	}
}

func responseStatus(response *http.Response) any {
	if response == nil {
		return nil
	}
	return response.StatusCode
}

func newPTYSigner(t *testing.T) ssh.Signer {
	t.Helper()
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	return signer
}

func startPTYSSHServer(t *testing.T, allowed ssh.PublicKey, closeAfterGreeting bool) string {
	t.Helper()
	config := &ssh.ServerConfig{
		PublicKeyCallback: func(_ ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			if !bytes.Equal(key.Marshal(), allowed.Marshal()) {
				return nil, errors.New("unknown key")
			}
			return nil, nil
		},
	}
	config.AddHostKey(newPTYSigner(t))
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go servePTYSSHConnection(conn, config, closeAfterGreeting)
		}
	}()
	return listener.Addr().String()
}

func servePTYSSHConnection(conn net.Conn, config *ssh.ServerConfig, closeAfterGreeting bool) {
	defer conn.Close()
	_, channels, requests, err := ssh.NewServerConn(conn, config)
	if err != nil {
		return
	}
	go ssh.DiscardRequests(requests)
	for channelRequest := range channels {
		channel, requests, err := channelRequest.Accept()
		if err != nil {
			return
		}
		go func() {
			defer channel.Close()
			for request := range requests {
				switch request.Type {
				case "pty-req", "window-change":
					_ = request.Reply(true, nil)
				case "shell":
					_ = request.Reply(true, nil)
					_, _ = channel.Write([]byte("ready\n"))
					if closeAfterGreeting {
						return
					}
					_, _ = io.Copy(channel, channel)
				default:
					_ = request.Reply(false, nil)
				}
			}
		}()
	}
}

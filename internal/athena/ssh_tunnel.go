package athena

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
)

const sshTunnelTTL = 10 * time.Minute
const sshRPCResponseTimeout = 2 * time.Second

type proxyBridge struct {
	ctx      context.Context
	cancel   context.CancelFunc
	listener net.Listener
	tcp      chan net.Conn
	ws       chan *websocket.Conn
	done     chan struct{}

	mu        sync.Mutex
	wsClaimed bool
	activeTCP net.Conn
	activeWS  *websocket.Conn
	stopOnce  sync.Once
}

func PickTCPPort(minPort, maxPort int) (int, error) {
	listener, port, err := listenTCPInRange(minPort, maxPort)
	if err != nil {
		return 0, err
	}
	_ = listener.Close()
	return port, nil
}

func (h *Hub) OpenSSHTunnel(ctx context.Context, dongleID string) (int, func(), error) {
	if !h.IsOnline(dongleID) {
		return 0, nil, ErrOffline
	}

	h.tunnelMu.Lock()
	cfg := h.tunnelConfig
	getBase := h.baseURL
	h.tunnelMu.Unlock()
	listener, port, err := listenTCPInRange(cfg.SSHTunnelPortMin, cfg.SSHTunnelPortMax)
	if err != nil {
		return 0, nil, err
	}
	publicBase := cfg.PublicBaseURL
	if getBase != nil {
		if v := getBase(); v != "" {
			publicBase = v
		}
	}
	remoteWSURI, ticket, err := proxyWebSocketURI(publicBase)
	if err != nil {
		_ = listener.Close()
		return 0, nil, err
	}

	tunnelCtx, cancelContext := context.WithTimeout(ctx, sshTunnelTTL)
	bridge := &proxyBridge{
		ctx:      tunnelCtx,
		cancel:   cancelContext,
		listener: listener,
		tcp:      make(chan net.Conn, 1),
		ws:       make(chan *websocket.Conn, 1),
		done:     make(chan struct{}),
	}
	cancel := func() {
		h.removeProxyTicket(ticket, bridge)
		bridge.stop()
	}
	h.tunnelMu.Lock()
	h.proxyTickets[ticket] = bridge
	h.tunnelMu.Unlock()
	go bridge.run(func() { h.removeProxyTicket(ticket, bridge) })

	// openpilot also has forks using positional params; the object form matches
	// athenad.startLocalProxy and keeps the field meanings explicit.
	rpcCtx, cancelRPC := context.WithTimeout(ctx, sshRPCResponseTimeout)
	defer cancelRPC()
	err = h.CallJSONRPC(rpcCtx, dongleID, "startLocalProxy", map[string]any{
		"remote_ws_uri": remoteWSURI,
		"local_port":    22,
	})
	if err != nil {
		cancel()
		return 0, nil, err
	}
	return port, cancel, nil
}

func (h *Hub) handleProxyWebSocket(w http.ResponseWriter, r *http.Request) {
	ticket := r.PathValue("ticket")
	h.tunnelMu.Lock()
	bridge := h.proxyTickets[ticket]
	h.tunnelMu.Unlock()
	if bridge == nil {
		http.NotFound(w, r)
		return
	}

	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	if !bridge.claimWebSocket(conn) {
		_ = conn.Close(websocket.StatusPolicyViolation, "invalid or claimed ticket")
		return
	}
	<-bridge.done
}

func (h *Hub) removeProxyTicket(ticket string, bridge *proxyBridge) {
	h.tunnelMu.Lock()
	if h.proxyTickets[ticket] == bridge {
		delete(h.proxyTickets, ticket)
	}
	h.tunnelMu.Unlock()
}

func listenTCPInRange(minPort, maxPort int) (net.Listener, int, error) {
	if minPort < 1 || maxPort > 65535 || minPort > maxPort {
		return nil, 0, fmt.Errorf("invalid TCP port range %d-%d", minPort, maxPort)
	}
	for port := minPort; port <= maxPort; port++ {
		listener, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
		if err == nil {
			return listener, port, nil
		}
	}
	return nil, 0, fmt.Errorf("no TCP port available in range %d-%d", minPort, maxPort)
}

func proxyWebSocketURI(publicBaseURL string) (string, string, error) {
	base, err := url.Parse(publicBaseURL)
	if err != nil {
		return "", "", err
	}
	switch base.Scheme {
	case "http":
		base.Scheme = "ws"
	case "https":
		base.Scheme = "wss"
	default:
		return "", "", fmt.Errorf("unsupported public URL scheme %q", base.Scheme)
	}
	if base.Host == "" {
		return "", "", errors.New("public URL host required")
	}
	ticketBytes := make([]byte, 16)
	if _, err := rand.Read(ticketBytes); err != nil {
		return "", "", err
	}
	ticket := hex.EncodeToString(ticketBytes)
	base.Path = strings.TrimRight(base.Path, "/") + "/ws/proxy/" + ticket
	base.RawQuery = ""
	base.Fragment = ""
	return base.String(), ticket, nil
}

func (b *proxyBridge) run(onDone func()) {
	defer close(b.done)
	defer onDone()
	defer b.stop()

	go func() {
		conn, err := b.listener.Accept()
		if err != nil {
			return
		}
		b.setActiveTCP(conn)
		select {
		case b.tcp <- conn:
		case <-b.ctx.Done():
			_ = conn.Close()
		}
	}()

	var tcpConn net.Conn
	var wsConn *websocket.Conn
	for tcpConn == nil || wsConn == nil {
		select {
		case tcpConn = <-b.tcp:
		case wsConn = <-b.ws:
		case <-b.ctx.Done():
			return
		}
	}
	BridgeTCPAndWS(tcpConn, wsConn)
}

func (b *proxyBridge) claimWebSocket(conn *websocket.Conn) bool {
	b.mu.Lock()
	if b.wsClaimed || b.ctx.Err() != nil {
		b.mu.Unlock()
		return false
	}
	b.wsClaimed = true
	b.activeWS = conn
	b.mu.Unlock()

	select {
	case b.ws <- conn:
		return true
	case <-b.ctx.Done():
		return false
	}
}

func (b *proxyBridge) setActiveTCP(conn net.Conn) {
	b.mu.Lock()
	b.activeTCP = conn
	b.mu.Unlock()
}

func (b *proxyBridge) stop() {
	b.stopOnce.Do(func() {
		b.cancel()
		_ = b.listener.Close()
		b.mu.Lock()
		if b.activeTCP != nil {
			_ = b.activeTCP.Close()
		}
		if b.activeWS != nil {
			b.activeWS.CloseNow()
		}
		b.mu.Unlock()
	})
}

// BridgeTCPAndWS copies bytes both ways until either side closes.
func BridgeTCPAndWS(tcpConn net.Conn, ws *websocket.Conn) {
	wsConn := websocket.NetConn(context.Background(), ws, websocket.MessageBinary)
	defer wsConn.Close()
	defer tcpConn.Close()

	done := make(chan struct{}, 2)
	go func() {
		_, _ = io.Copy(wsConn, tcpConn)
		done <- struct{}{}
	}()
	go func() {
		_, _ = io.Copy(tcpConn, wsConn)
		done <- struct{}{}
	}()
	<-done
}

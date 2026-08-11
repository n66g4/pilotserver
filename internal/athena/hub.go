package athena

import (
	"encoding/json"
	"errors"
	"strconv"
	"sync"
	"sync/atomic"

	"pilotserver/internal/config"
)

var ErrOffline = errors.New("device offline")

type Conn interface {
	Send(msg []byte) error
	Close() error
}

type NopConn struct{}

func (NopConn) Send([]byte) error { return nil }
func (NopConn) Close() error      { return nil }

type hubConn struct {
	conn    Conn
	session uint64
}

type Hub struct {
	mu          sync.RWMutex
	conns       map[string]hubConn
	nextID      atomic.Uint64
	nextSession atomic.Uint64

	tunnelMu     sync.Mutex
	tunnelConfig config.Config
	proxyTickets map[string]*proxyBridge
}

func NewHub(configs ...config.Config) *Hub {
	h := &Hub{
		conns:        make(map[string]hubConn),
		proxyTickets: make(map[string]*proxyBridge),
	}
	if len(configs) > 0 {
		h.tunnelConfig = configs[0]
	}
	return h
}

func (h *Hub) SetOnline(dongleID string, conn Conn) uint64 {
	session := h.nextSession.Add(1)
	h.mu.Lock()
	old, replaced := h.conns[dongleID]
	h.conns[dongleID] = hubConn{conn: conn, session: session}
	h.mu.Unlock()
	if replaced {
		_ = old.conn.Close()
	}
	return session
}

func (h *Hub) SetOffline(dongleID string, session uint64) {
	h.mu.Lock()
	if current, ok := h.conns[dongleID]; ok && current.session == session {
		delete(h.conns, dongleID)
	}
	h.mu.Unlock()
}

func (h *Hub) IsOnline(dongleID string) bool {
	h.mu.RLock()
	_, ok := h.conns[dongleID]
	h.mu.RUnlock()
	return ok
}

func (h *Hub) SendJSONRPC(dongleID string, method string, params any) (string, error) {
	h.mu.RLock()
	current, ok := h.conns[dongleID]
	h.mu.RUnlock()
	if !ok {
		return "", ErrOffline
	}

	id := strconv.FormatUint(h.nextID.Add(1), 10)
	msg, err := json.Marshal(struct {
		JSONRPC string `json:"jsonrpc"`
		Method  string `json:"method"`
		ID      string `json:"id"`
		Params  any    `json:"params"`
	}{
		JSONRPC: "2.0",
		Method:  method,
		ID:      id,
		Params:  params,
	})
	if err != nil {
		return "", err
	}
	if err := current.conn.Send(msg); err != nil {
		return "", err
	}
	return id, nil
}

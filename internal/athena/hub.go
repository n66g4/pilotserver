package athena

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	pendingMu   sync.Mutex
	pending     map[string]chan error

	tunnelMu     sync.Mutex
	tunnelConfig config.Config
	proxyTickets map[string]*proxyBridge
}

func NewHub(configs ...config.Config) *Hub {
	h := &Hub{
		conns:        make(map[string]hubConn),
		pending:      make(map[string]chan error),
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
	return h.sendJSONRPC(dongleID, method, params, nil)
}

func (h *Hub) CallJSONRPC(ctx context.Context, dongleID string, method string, params any) error {
	response := make(chan error, 1)
	id, err := h.sendJSONRPC(dongleID, method, params, response)
	if err != nil {
		return err
	}
	defer h.removePending(id)
	select {
	case err := <-response:
		return err
	case <-ctx.Done():
		return fmt.Errorf("wait for JSON-RPC response: %w", ctx.Err())
	}
}

func (h *Hub) sendJSONRPC(dongleID string, method string, params any, response chan error) (string, error) {
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
	if response != nil {
		h.pendingMu.Lock()
		h.pending[id] = response
		h.pendingMu.Unlock()
	}
	if err := current.conn.Send(msg); err != nil {
		h.removePending(id)
		return "", err
	}
	return id, nil
}

func (h *Hub) HandleJSONRPCResponse(message []byte) {
	var response struct {
		ID    string `json:"id"`
		Error *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(message, &response); err != nil || response.ID == "" {
		return
	}
	h.pendingMu.Lock()
	waiter := h.pending[response.ID]
	delete(h.pending, response.ID)
	h.pendingMu.Unlock()
	if waiter == nil {
		return
	}
	if response.Error != nil {
		waiter <- fmt.Errorf("JSON-RPC error %d: %s", response.Error.Code, response.Error.Message)
		return
	}
	waiter <- nil
}

func (h *Hub) removePending(id string) {
	h.pendingMu.Lock()
	delete(h.pending, id)
	h.pendingMu.Unlock()
}

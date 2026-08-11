package athena

import (
	"encoding/json"
	"errors"
	"strconv"
	"sync"
	"sync/atomic"
)

var ErrOffline = errors.New("device offline")

type Conn interface {
	Send(msg []byte) error
	Close() error
}

type NopConn struct{}

func (NopConn) Send([]byte) error { return nil }
func (NopConn) Close() error      { return nil }

type Hub struct {
	mu     sync.RWMutex
	conns  map[string]Conn
	nextID atomic.Uint64
}

func NewHub() *Hub {
	return &Hub{conns: make(map[string]Conn)}
}

func (h *Hub) SetOnline(dongleID string, conn Conn) {
	h.mu.Lock()
	h.conns[dongleID] = conn
	h.mu.Unlock()
}

func (h *Hub) SetOffline(dongleID string) {
	h.mu.Lock()
	delete(h.conns, dongleID)
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
	conn, ok := h.conns[dongleID]
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
	if err := conn.Send(msg); err != nil {
		return "", err
	}
	return id, nil
}

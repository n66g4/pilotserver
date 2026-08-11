package athena

import (
	"encoding/json"
	"errors"
	"testing"
)

type recordingConn struct {
	msg []byte
}

func (c *recordingConn) Send(msg []byte) error {
	c.msg = msg
	return nil
}

func (*recordingConn) Close() error { return nil }

func TestHubOnlineOffline(t *testing.T) {
	h := NewHub()
	if h.IsOnline("d1") {
		t.Fatal("expected offline")
	}
	h.SetOnline("d1", NopConn{})
	if !h.IsOnline("d1") {
		t.Fatal("expected online")
	}
	h.SetOffline("d1")
	if h.IsOnline("d1") {
		t.Fatal("expected offline")
	}
}

func TestSendJSONRPC(t *testing.T) {
	h := NewHub()
	conn := &recordingConn{}
	h.SetOnline("d1", conn)

	id, err := h.SendJSONRPC("d1", "ping", map[string]string{"value": "ok"})
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		JSONRPC string            `json:"jsonrpc"`
		Method  string            `json:"method"`
		ID      string            `json:"id"`
		Params  map[string]string `json:"params"`
	}
	if err := json.Unmarshal(conn.msg, &got); err != nil {
		t.Fatal(err)
	}
	if id == "" || got.JSONRPC != "2.0" || got.Method != "ping" || got.ID != id || got.Params["value"] != "ok" {
		t.Fatalf("message = %+v, returned id = %q", got, id)
	}
}

func TestSendJSONRPCRejectsOfflineDevice(t *testing.T) {
	_, err := NewHub().SendJSONRPC("d1", "ping", nil)
	if !errors.Is(err, ErrOffline) {
		t.Fatalf("error = %v, want %v", err, ErrOffline)
	}
}

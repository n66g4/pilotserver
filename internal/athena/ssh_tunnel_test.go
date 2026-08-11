package athena

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"pilotserver/internal/config"
)

func TestPickTCPPortInRange(t *testing.T) {
	port, err := PickTCPPort(41000, 41099)
	if err != nil {
		t.Fatal(err)
	}
	if port < 41000 || port > 41099 {
		t.Fatalf("port = %d, want 41000..41099", port)
	}
}

func TestOpenSSHTunnelSendsStartLocalProxy(t *testing.T) {
	hub := NewHub(config.Config{
		PublicBaseURL:    "https://op.example.com/base",
		SSHTunnelPortMin: 42000,
		SSHTunnelPortMax: 42099,
	})
	conn := &recordingConn{onSend: func(message []byte) {
		var request struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(message, &request); err != nil {
			t.Error(err)
			return
		}
		hub.HandleJSONRPCResponse([]byte(`{"jsonrpc":"2.0","id":"` + request.ID + `","result":true}`))
	}}
	hub.SetOnline("d1", conn)

	port, cancel, err := hub.OpenSSHTunnel(context.Background(), "d1")
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	if port < 42000 || port > 42099 {
		t.Fatalf("port = %d, want 42000..42099", port)
	}

	var request struct {
		Method string `json:"method"`
		Params struct {
			RemoteWSURI string `json:"remote_ws_uri"`
			LocalPort   int    `json:"local_port"`
		} `json:"params"`
	}
	if err := json.Unmarshal(conn.msg, &request); err != nil {
		t.Fatal(err)
	}
	if request.Method != "startLocalProxy" {
		t.Fatalf("method = %q", request.Method)
	}
	if request.Params.LocalPort != 22 {
		t.Fatalf("local_port = %d", request.Params.LocalPort)
	}
	if !strings.HasPrefix(request.Params.RemoteWSURI, "wss://op.example.com/base/ws/proxy/") {
		t.Fatalf("remote_ws_uri = %q", request.Params.RemoteWSURI)
	}
}

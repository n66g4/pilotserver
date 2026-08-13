package listenaddr_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"pilotserver/internal/listenaddr"
)

func TestSetPersistsAndNotifies(t *testing.T) {
	envPath := filepath.Join(t.TempDir(), "pilotserver.env")
	var got string
	r, err := listenaddr.New("127.0.0.1:18780", envPath, false, func(addr string) {
		got = addr
	})
	if err != nil {
		t.Fatal(err)
	}
	changed, err := r.Set("127.0.0.1:9090")
	if err != nil || !changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	if got != "127.0.0.1:9090" || r.Get() != "127.0.0.1:9090" {
		t.Fatalf("got=%q current=%q", got, r.Get())
	}
	data, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "PILOTSERVER_LISTEN=127.0.0.1:9090") {
		t.Fatalf("env file: %s", data)
	}
	changed, err = r.Set("127.0.0.1:9090")
	if err != nil || changed {
		t.Fatalf("expected no change, changed=%v err=%v", changed, err)
	}
}

func TestRejectNonLoopback(t *testing.T) {
	r, err := listenaddr.New("127.0.0.1:18780", "", false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Set("0.0.0.0:18780"); err == nil {
		t.Fatal("expected reject")
	}
}

func TestEnvValueWins(t *testing.T) {
	r, err := listenaddr.New("0.0.0.0:18780", "", true, nil)
	if err != nil {
		t.Fatal(err)
	}
	if r.Get() != "0.0.0.0:18780" || !r.AllowNonLoopback() {
		t.Fatalf("get=%q allow=%v", r.Get(), r.AllowNonLoopback())
	}
}

func TestAllowNonLoopbackToggle(t *testing.T) {
	envPath := filepath.Join(t.TempDir(), "pilotserver.env")
	var rebinds []string
	r, err := listenaddr.New("127.0.0.1:18780", envPath, false, func(addr string) {
		rebinds = append(rebinds, addr)
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := r.SetAllowNonLoopback(true); err != nil {
		t.Fatal(err)
	}
	if !r.AllowNonLoopback() {
		t.Fatal("flag should be on")
	}
	if _, err := r.Set("0.0.0.0:18780"); err != nil {
		t.Fatalf("0.0.0.0 should be accepted: %v", err)
	}

	// Disabling with a non-loopback address active falls back to loopback.
	changed, err := r.SetAllowNonLoopback(false)
	if err != nil || !changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	if r.Get() != "127.0.0.1:18780" {
		t.Fatalf("get=%q", r.Get())
	}

	data, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"PILOTSERVER_ALLOW_NON_LOOPBACK=0", "PILOTSERVER_LISTEN=127.0.0.1:18780"} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("env file missing %q: %s", want, data)
		}
	}
}

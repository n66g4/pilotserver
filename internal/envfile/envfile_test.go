package envfile_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"pilotserver/internal/envfile"
)

func TestUpsertCreatesAndUpdates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pilotserver.env")
	if err := envfile.Upsert(path, "PILOTSERVER_LISTEN", "127.0.0.1:18780"); err != nil {
		t.Fatal(err)
	}
	if err := envfile.Upsert(path, "PILOTSERVER_LISTEN", "127.0.0.1:9080"); err != nil {
		t.Fatal(err)
	}
	if err := envfile.Upsert(path, "PILOTSERVER_PUBLIC_BASE_URL", "https://example.com"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, "PILOTSERVER_LISTEN=127.0.0.1:9080") {
		t.Fatalf("listen not updated: %s", text)
	}
	if strings.Count(text, "PILOTSERVER_LISTEN=") != 1 {
		t.Fatalf("duplicate listen keys: %s", text)
	}
	if !strings.Contains(text, "PILOTSERVER_PUBLIC_BASE_URL=https://example.com") {
		t.Fatalf("public url missing: %s", text)
	}
}

package publicbase_test

import (
	"testing"

	"pilotserver/internal/publicbase"
	"pilotserver/internal/store"
)

func TestResolverPrefersStoreOverEnv(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	r, err := publicbase.New(st, "https://from-env.example.com")
	if err != nil {
		t.Fatal(err)
	}
	if got := r.Get(); got != "https://from-env.example.com" {
		t.Fatalf("seeded get = %q", got)
	}
	if err := r.Set("https://from-admin.example.com/"); err != nil {
		t.Fatal(err)
	}
	if got := r.Get(); got != "https://from-admin.example.com" {
		t.Fatalf("after set = %q", got)
	}
}

func TestNormalizeAssumesHTTPS(t *testing.T) {
	got, err := publicbase.Normalize("xxx.synology.me")
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://xxx.synology.me" {
		t.Fatalf("normalize = %q", got)
	}
}

func TestNormalizeRejectsInvalid(t *testing.T) {
	for _, raw := range []string{"", "ftp://example.com", "https://"} {
		if _, err := publicbase.Normalize(raw); err == nil {
			t.Fatalf("expected error for %q", raw)
		}
	}
}

package sshkey_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"

	"pilotserver/internal/sshkey"
)

func TestOpenCreatesEd25519AndNeverExposesPrivateKey(t *testing.T) {
	dir := t.TempDir()
	store, err := sshkey.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	pub, err := store.PublicKey()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(pub, "ssh-ed25519 ") {
		t.Fatalf("public_key = %q", pub)
	}
	info, err := os.Stat(filepath.Join(dir, "ssh", "id_ed25519"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("perm = %o", info.Mode().Perm())
	}
	publicInfo, err := os.Stat(filepath.Join(dir, "ssh", "id_ed25519.pub"))
	if err != nil {
		t.Fatal(err)
	}
	if publicInfo.Mode().Perm() != 0o644 {
		t.Fatalf("public perm = %o", publicInfo.Mode().Perm())
	}
	again, err := sshkey.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	second, err := again.PublicKey()
	if err != nil {
		t.Fatal(err)
	}
	if second != pub {
		t.Fatal("reopen changed public key")
	}
	rotated, err := store.Rotate()
	if err != nil {
		t.Fatal(err)
	}
	if rotated == pub {
		t.Fatal("rotate returned the same public key")
	}
}

func TestSignerCreatesKeypairWhenMissing(t *testing.T) {
	store, err := sshkey.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	signer, err := store.Signer()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(ssh.MarshalAuthorizedKey(signer.PublicKey())), "ssh-ed25519 ") {
		t.Fatalf("signer public key type = %s", signer.PublicKey().Type())
	}
}

func TestPublicKeyRepairsExistingPermissions(t *testing.T) {
	dir := t.TempDir()
	store, err := sshkey.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.PublicKey(); err != nil {
		t.Fatal(err)
	}

	sshDir := filepath.Join(dir, "ssh")
	privatePath := filepath.Join(sshDir, "id_ed25519")
	publicPath := filepath.Join(sshDir, "id_ed25519.pub")
	if err := os.Chmod(sshDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(privatePath, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(publicPath, 0o666); err != nil {
		t.Fatal(err)
	}

	if _, err := store.PublicKey(); err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]os.FileMode{
		sshDir:      0o700,
		privatePath: 0o600,
		publicPath:  0o644,
	} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != want {
			t.Fatalf("%s perm = %o, want %o", path, got, want)
		}
	}
}

func TestRotateWritesMatchingPublicAndPrivateKeys(t *testing.T) {
	store, err := sshkey.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	publicKey, err := store.Rotate()
	if err != nil {
		t.Fatal(err)
	}
	signer, err := store.Signer()
	if err != nil {
		t.Fatal(err)
	}
	signerPublicKey := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(signer.PublicKey())))
	if signerPublicKey != publicKey {
		t.Fatalf("private key public part = %q, want %q", signerPublicKey, publicKey)
	}
}

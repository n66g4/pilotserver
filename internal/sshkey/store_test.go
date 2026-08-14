package sshkey_test

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/crypto/ssh"

	"pilotserver/internal/sshkey"
)

func TestStatusUnconfiguredWhenMissing(t *testing.T) {
	store, err := sshkey.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	status, err := store.Status()
	if err != nil {
		t.Fatal(err)
	}
	if status.Configured || status.Fingerprint != "" {
		t.Fatalf("status = %+v", status)
	}
	if _, err := store.Signer(); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Signer() error = %v, want ErrNotExist", err)
	}
}

func TestImportPersistsKeyAndReportsFingerprint(t *testing.T) {
	dir := t.TempDir()
	store, err := sshkey.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	pemBytes, wantFP := testPrivateKeyPEM(t)
	status, err := store.Import(pemBytes)
	if err != nil {
		t.Fatal(err)
	}
	if !status.Configured || status.Fingerprint != wantFP {
		t.Fatalf("status = %+v, want fingerprint %q", status, wantFP)
	}
	info, err := os.Stat(filepath.Join(dir, "ssh", "id_ed25519"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("perm = %o", info.Mode().Perm())
	}
	if _, err := os.Stat(filepath.Join(dir, "ssh", "id_ed25519.pub")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("did not want a .pub file")
	}
	again, err := sshkey.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	second, err := again.Status()
	if err != nil {
		t.Fatal(err)
	}
	if second != status {
		t.Fatalf("reopen status = %+v, want %+v", second, status)
	}
	if _, err := store.Signer(); err != nil {
		t.Fatal(err)
	}
}

func TestImportRejectsInvalidAndPassphraseKeysWithoutWriting(t *testing.T) {
	dir := t.TempDir()
	store, err := sshkey.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	good, _ := testPrivateKeyPEM(t)
	if _, err := store.Import(good); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(dir, "ssh", "id_ed25519"))
	if err != nil {
		t.Fatal(err)
	}

	if _, err := store.Import([]byte("not-a-key")); !errors.Is(err, sshkey.ErrInvalidKey) {
		t.Fatalf("invalid key error = %v", err)
	}
	passphrasePEM := testPassphrasePrivateKeyPEM(t)
	if _, err := store.Import(passphrasePEM); !errors.Is(err, sshkey.ErrInvalidKey) {
		t.Fatalf("passphrase key error = %v", err)
	}
	after, err := os.ReadFile(filepath.Join(dir, "ssh", "id_ed25519"))
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("rejected import overwrote the stored key")
	}
}

func TestClearRemovesPrivateAndLeftoverPublicKey(t *testing.T) {
	dir := t.TempDir()
	store, err := sshkey.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	pemBytes, _ := testPrivateKeyPEM(t)
	if _, err := store.Import(pemBytes); err != nil {
		t.Fatal(err)
	}
	pubPath := filepath.Join(dir, "ssh", "id_ed25519.pub")
	if err := os.WriteFile(pubPath, []byte("ssh-ed25519 leftover"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := store.Clear(); err != nil {
		t.Fatal(err)
	}
	if err := store.Clear(); err != nil {
		t.Fatal(err)
	}
	status, err := store.Status()
	if err != nil {
		t.Fatal(err)
	}
	if status.Configured {
		t.Fatal("still configured after Clear")
	}
	for _, name := range []string{"id_ed25519", "id_ed25519.pub"} {
		if _, err := os.Stat(filepath.Join(dir, "ssh", name)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("%s still exists", name)
		}
	}
}

func TestStatusRepairsPrivateKeyPermissions(t *testing.T) {
	dir := t.TempDir()
	store, err := sshkey.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	pemBytes, _ := testPrivateKeyPEM(t)
	if _, err := store.Import(pemBytes); err != nil {
		t.Fatal(err)
	}
	sshDir := filepath.Join(dir, "ssh")
	privatePath := filepath.Join(sshDir, "id_ed25519")
	if err := os.Chmod(sshDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(privatePath, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Status(); err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]os.FileMode{
		sshDir:      0o700,
		privatePath: 0o600,
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

func TestStatusErrorsOnCorruptKey(t *testing.T) {
	dir := t.TempDir()
	store, err := sshkey.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ssh", "id_ed25519"), []byte("corrupt"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Status(); err == nil {
		t.Fatal("Status() succeeded on corrupt key")
	}
}

func testPrivateKeyPEM(t *testing.T) ([]byte, string) {
	t.Helper()
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	block, err := ssh.MarshalPrivateKey(privateKey, "")
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(block), ssh.FingerprintSHA256(signer.PublicKey())
}

func testPassphrasePrivateKeyPEM(t *testing.T) []byte {
	t.Helper()
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	block, err := ssh.MarshalPrivateKeyWithPassphrase(privateKey, "", []byte("secret"))
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(block)
}

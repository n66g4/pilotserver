# Import GitHub SSH Private Key Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace NAS auto-generated SSH keys with an admin-imported GitHub OpenSSH private key, so the web terminal authenticates as `comma` against devices that already trust GitHub usernames.

**Architecture:** `internal/sshkey` stops generating keys. Admins PUT a PEM private key; the store validates with `ssh.ParsePrivateKey`, writes `$dataDir/ssh/id_ed25519` at `0600`, and reports only `{configured,fingerprint}`. PTY sessions refuse to open a tunnel until a key exists. The settings UI becomes paste/save/clear.

**Tech Stack:** Go 1.25, `golang.org/x/crypto/ssh`, existing admin JWT mux, embedded `web/admin/index.html` + `i18n.js`.

## Global Constraints

- Spec: `docs/superpowers/specs/2026-08-14-admin-import-github-ssh-key-design.md`
- Do not change device HTTP API paths, Athena `startLocalProxy`, HLS, telemetry, or the PTY WebSocket frame protocol.
- Do not auto-generate or rotate NAS keys. Do not return private key material in JSON, logs, or the DOM.
- Do not read or copy the operator's `~/.ssh/id_ed25519`. Tests must generate throwaway keys with `ed25519.GenerateKey` + `ssh.MarshalPrivateKey`.
- HTML must not contain the strings `PRIVATE KEY` or `id_ed25519`.
- Unsupported: passphrase-protected keys, encrypt-at-rest, multiple keys.
- Locales remain exactly `zh-CN` and `en`.
- Interactive targets stay at least 40px.
- After every production-code edit: `go build -o bin/pilotserver ./cmd/pilotserver`
- Do not create a Git commit unless the user separately requests one.
- Release artifact version is `1.0.21-1`.

---

## File Map

- Modify: `internal/sshkey/store.go` — Import / Status / Clear / Signer; remove generate/rotate/public-key files
- Modify: `internal/sshkey/store_test.go`
- Modify: `internal/adminapi/sshkey.go` — GET / PUT / DELETE
- Modify: `internal/adminapi/sshkey_test.go`
- Modify: `internal/adminapi/handler.go` — routes + `DELETE /admin/api/`
- Modify: `internal/adminapi/sshpty.go` — `key_unconfigured` before tunnel
- Modify: `internal/adminapi/sshpty_test.go` — import keys; new unconfigured test
- Modify: `web/admin/index.html`, `i18n.js`, `i18n_test.js`, `admin_test.go`
- Modify: `README.md`, `docs/synology-dsm72-spk.md`, `synology/build-spk.sh`

---

### Task 1: sshkey store imports instead of generating

**Files:**
- Modify: `internal/sshkey/store.go`
- Test: `internal/sshkey/store_test.go`

**Interfaces:**
- Consumes: `$dataDir/ssh/` directory created by `Open`
- Produces:
  - `type Status struct { Configured bool; Fingerprint string }`
  - `var ErrInvalidKey = errors.New("invalid SSH private key")`
  - `func Open(dataDir string) (*Store, error)`
  - `func (*Store) Status() (Status, error)` — missing file → `{Configured:false}`; corrupt file → error
  - `func (*Store) Import(privateKey []byte) (Status, error)` — parse first; invalid/passphrase → `ErrInvalidKey` and no write
  - `func (*Store) Clear() error` — delete `id_ed25519` and leftover `id_ed25519.pub`; missing is OK
  - `func (*Store) Signer() (ssh.Signer, error)` — missing → `os.ErrNotExist`; never generate
- Private file: `$dataDir/ssh/id_ed25519` mode `0600`
- Fingerprint: `ssh.FingerprintSHA256(pub)` (`SHA256:…`)

- [ ] **Step 1: Replace store tests**

Overwrite `internal/sshkey/store_test.go` with:

```go
package sshkey_test

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"
	"strings"
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
```

Remove unused `strings` import if the compiler complains; keep the file as written if `strings` is unused — delete the `strings` import. The template above does not use `strings`; do not import it.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test -count=1 ./internal/sshkey`

Expected: FAIL because `Status`, `Import`, `Clear`, and `ErrInvalidKey` do not exist (or existing tests fail after replacement).

- [ ] **Step 3: Rewrite `internal/sshkey/store.go`**

Replace the file with:

```go
package sshkey

import (
	"errors"
	"os"
	"path/filepath"
	"sync"

	"golang.org/x/crypto/ssh"
)

type Store struct {
	dir string
}

type Status struct {
	Configured  bool
	Fingerprint string
}

var ErrInvalidKey = errors.New("invalid SSH private key")

var fileMu sync.Mutex

func Open(dataDir string) (*Store, error) {
	fileMu.Lock()
	defer fileMu.Unlock()

	dir := filepath.Join(dataDir, "ssh")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	if err := ensurePermissions(dir); err != nil {
		return nil, err
	}
	return &Store{dir: dir}, nil
}

func (s *Store) Status() (Status, error) {
	fileMu.Lock()
	defer fileMu.Unlock()
	if err := ensurePermissions(s.dir); err != nil {
		return Status{}, err
	}
	signer, err := s.signer()
	if errors.Is(err, os.ErrNotExist) {
		return Status{}, nil
	}
	if err != nil {
		return Status{}, err
	}
	return Status{Configured: true, Fingerprint: ssh.FingerprintSHA256(signer.PublicKey())}, nil
}

func (s *Store) Import(privateKey []byte) (Status, error) {
	fileMu.Lock()
	defer fileMu.Unlock()
	if err := ensurePermissions(s.dir); err != nil {
		return Status{}, err
	}
	signer, err := ssh.ParsePrivateKey(privateKey)
	if err != nil {
		return Status{}, ErrInvalidKey
	}
	if err := writeFileAtomic(s.privatePath(), privateKey, 0o600); err != nil {
		return Status{}, err
	}
	_ = os.Remove(s.publicPath())
	return Status{Configured: true, Fingerprint: ssh.FingerprintSHA256(signer.PublicKey())}, nil
}

func (s *Store) Clear() error {
	fileMu.Lock()
	defer fileMu.Unlock()
	if err := ensurePermissions(s.dir); err != nil {
		return err
	}
	for _, path := range []string{s.privatePath(), s.publicPath()} {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func (s *Store) Signer() (ssh.Signer, error) {
	fileMu.Lock()
	defer fileMu.Unlock()
	if err := ensurePermissions(s.dir); err != nil {
		return nil, err
	}
	return s.signer()
}

func (s *Store) signer() (ssh.Signer, error) {
	privateKey, err := os.ReadFile(s.privatePath())
	if err != nil {
		return nil, err
	}
	return ssh.ParsePrivateKey(privateKey)
}

func (s *Store) privatePath() string {
	return filepath.Join(s.dir, "id_ed25519")
}

func (s *Store) publicPath() string {
	return filepath.Join(s.dir, "id_ed25519.pub")
}

func ensurePermissions(dir string) error {
	if err := os.Chmod(dir, 0o700); err != nil {
		return err
	}
	path := filepath.Join(dir, "id_ed25519")
	if err := os.Chmod(path, 0o600); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	temp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+"-*")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)

	if err := temp.Chmod(mode); err != nil {
		temp.Close()
		return err
	}
	if _, err := temp.Write(data); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(tempPath, path)
}
```

Delete `PublicKey`, `Rotate`, `rotate`, `writePublicKey`, and ed25519 generation. Keep `writeFileAtomic`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -count=1 ./internal/sshkey`

Expected: PASS

- [ ] **Step 5: Commit**

Skip unless the user asked to commit.

---

### Task 2: Admin SSH key API

**Files:**
- Modify: `internal/adminapi/sshkey.go`
- Modify: `internal/adminapi/sshkey_test.go`
- Modify: `internal/adminapi/handler.go`

**Interfaces:**
- Consumes: `sshkey.Open`, `Status`, `Import`, `Clear`, `ErrInvalidKey`
- Produces:
  - `GET /admin/api/ssh-key` → `{"configured":false}` or `{"configured":true,"fingerprint":"SHA256:…"}`
  - `PUT /admin/api/ssh-key` body `{"private_key":"-----BEGIN …"}` → same JSON; invalid → 400
  - `DELETE /admin/api/ssh-key` → `{"configured":false}` even if already missing
  - Remove `POST /admin/api/ssh-key/rotate`
  - `mux.Handle("DELETE /admin/api/", protected)` in `handler.go` (PUT is already mounted)
- GET corrupt key → 500
- No response body contains `PRIVATE KEY`

- [ ] **Step 1: Replace API tests**

Overwrite `internal/adminapi/sshkey_test.go` with:

```go
package adminapi_test

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"pilotserver/internal/athena"
	"pilotserver/internal/auth"
	"pilotserver/internal/config"
	"pilotserver/internal/store"
)

func TestAdminSSHKeyImportClearAndNeverExposesPrivateKey(t *testing.T) {
	dataDir := t.TempDir()
	st, err := store.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	mux := http.NewServeMux()
	cfg := config.Config{JWTSecret: adminTestSecret, DataDir: dataDir}
	mountAdmin(t, mux, st, athena.NewHub(), cfg, "")

	unauthorized := httptest.NewRecorder()
	mux.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/admin/api/ssh-key", nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d, want %d", unauthorized.Code, http.StatusUnauthorized)
	}

	token, err := auth.IssueAdminJWT(adminTestSecret, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	queryAuthorized := httptest.NewRecorder()
	mux.ServeHTTP(queryAuthorized, httptest.NewRequest(
		http.MethodGet,
		"/admin/api/ssh-key?access_token="+url.QueryEscape(token),
		nil,
	))
	if queryAuthorized.Code != http.StatusOK {
		t.Fatalf("query token status = %d, want %d", queryAuthorized.Code, http.StatusOK)
	}

	request := func(method, target string, body []byte) (int, string) {
		t.Helper()
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(method, target, bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		mux.ServeHTTP(rec, req)
		return rec.Code, rec.Body.String()
	}
	decode := func(body string) struct {
		Configured  bool   `json:"configured"`
		Fingerprint string `json:"fingerprint"`
		PublicKey   string `json:"public_key"`
	} {
		t.Helper()
		if strings.Contains(body, "PRIVATE KEY") {
			t.Fatal("response exposed private key")
		}
		var response struct {
			Configured  bool   `json:"configured"`
			Fingerprint string `json:"fingerprint"`
			PublicKey   string `json:"public_key"`
		}
		if err := json.Unmarshal([]byte(body), &response); err != nil {
			t.Fatal(err)
		}
		if response.PublicKey != "" {
			t.Fatalf("public_key leaked: %q", response.PublicKey)
		}
		return response
	}

	status, body := request(http.MethodGet, "/admin/api/ssh-key", nil)
	if status != http.StatusOK {
		t.Fatalf("GET empty status = %d, body = %s", status, body)
	}
	got := decode(body)
	if got.Configured || got.Fingerprint != "" {
		t.Fatalf("empty GET = %+v", got)
	}

	pemBytes, fingerprint := marshalAdminTestKey(t)
	putBody, err := json.Marshal(map[string]string{"private_key": string(pemBytes)})
	if err != nil {
		t.Fatal(err)
	}
	status, body = request(http.MethodPut, "/admin/api/ssh-key", putBody)
	if status != http.StatusOK {
		t.Fatalf("PUT status = %d, body = %s", status, body)
	}
	got = decode(body)
	if !got.Configured || got.Fingerprint != fingerprint {
		t.Fatalf("PUT response = %+v, want %q", got, fingerprint)
	}

	status, body = request(http.MethodGet, "/admin/api/ssh-key", nil)
	if status != http.StatusOK {
		t.Fatal(body)
	}
	got = decode(body)
	if got.Fingerprint != fingerprint {
		t.Fatalf("GET after PUT = %+v", got)
	}

	status, body = request(http.MethodPut, "/admin/api/ssh-key", []byte(`{"private_key":"not-a-key"}`))
	if status != http.StatusBadRequest {
		t.Fatalf("invalid PUT status = %d, body = %s", status, body)
	}
	status, body = request(http.MethodGet, "/admin/api/ssh-key", nil)
	got = decode(body)
	if got.Fingerprint != fingerprint {
		t.Fatal("invalid PUT overwrote the key")
	}

	status, body = request(http.MethodDelete, "/admin/api/ssh-key", nil)
	if status != http.StatusOK {
		t.Fatalf("DELETE status = %d, body = %s", status, body)
	}
	got = decode(body)
	if got.Configured {
		t.Fatal("still configured after DELETE")
	}
	status, body = request(http.MethodDelete, "/admin/api/ssh-key", nil)
	if status != http.StatusOK {
		t.Fatalf("second DELETE status = %d, body = %s", status, body)
	}
}

func marshalAdminTestKey(t *testing.T) ([]byte, string) {
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
```

- [ ] **Step 2: Run API tests to verify they fail**

Run: `go test -count=1 ./internal/adminapi -run TestAdminSSHKey`

Expected: FAIL (PUT/DELETE unregistered or old `{public_key}` JSON).

- [ ] **Step 3: Implement handlers and routes**

Replace `internal/adminapi/sshkey.go` with:

```go
package adminapi

import (
	"encoding/json"
	"errors"
	"net/http"

	"pilotserver/internal/sshkey"
)

type sshKeyResponse struct {
	Configured  bool   `json:"configured"`
	Fingerprint string `json:"fingerprint,omitempty"`
}

func handleGetSSHKey(w http.ResponseWriter, dataDir string) {
	keyStore, err := sshkey.Open(dataDir)
	if err != nil {
		http.Error(w, "open SSH key store", http.StatusInternalServerError)
		return
	}
	status, err := keyStore.Status()
	if err != nil {
		http.Error(w, "load SSH key", http.StatusInternalServerError)
		return
	}
	writeSSHKey(w, status)
}

func handlePutSSHKey(w http.ResponseWriter, r *http.Request, dataDir string) {
	var request struct {
		PrivateKey string `json:"private_key"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	keyStore, err := sshkey.Open(dataDir)
	if err != nil {
		http.Error(w, "open SSH key store", http.StatusInternalServerError)
		return
	}
	status, err := keyStore.Import([]byte(request.PrivateKey))
	if errors.Is(err, sshkey.ErrInvalidKey) {
		http.Error(w, "invalid SSH private key", http.StatusBadRequest)
		return
	}
	if err != nil {
		http.Error(w, "save SSH key", http.StatusInternalServerError)
		return
	}
	writeSSHKey(w, status)
}

func handleDeleteSSHKey(w http.ResponseWriter, dataDir string) {
	keyStore, err := sshkey.Open(dataDir)
	if err != nil {
		http.Error(w, "open SSH key store", http.StatusInternalServerError)
		return
	}
	if err := keyStore.Clear(); err != nil {
		http.Error(w, "clear SSH key", http.StatusInternalServerError)
		return
	}
	writeSSHKey(w, sshkey.Status{})
}

func writeSSHKey(w http.ResponseWriter, status sshkey.Status) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(sshKeyResponse{
		Configured:  status.Configured,
		Fingerprint: status.Fingerprint,
	})
}
```

In `internal/adminapi/handler.go`, replace the ssh-key routes:

```go
	adminMux.HandleFunc("GET /admin/api/ssh-key", func(w http.ResponseWriter, r *http.Request) {
		handleGetSSHKey(w, cfg.DataDir)
	})
	adminMux.HandleFunc("PUT /admin/api/ssh-key", func(w http.ResponseWriter, r *http.Request) {
		handlePutSSHKey(w, r, cfg.DataDir)
	})
	adminMux.HandleFunc("DELETE /admin/api/ssh-key", func(w http.ResponseWriter, r *http.Request) {
		handleDeleteSSHKey(w, cfg.DataDir)
	})
```

Delete `POST /admin/api/ssh-key/rotate`.

After the existing `mux.Handle("PUT /admin/api/", protected)` add:

```go
	mux.Handle("DELETE /admin/api/", protected)
```

- [ ] **Step 4: Run API tests to verify they pass**

Run: `go test -count=1 ./internal/adminapi -run 'TestAdminSSHKey|TestAdmin'`

Then: `go build -o bin/pilotserver ./cmd/pilotserver`

Expected: tests PASS; build succeeds. Existing PTY tests that call `Signer()` without importing a key may FAIL until Task 3 — if they fail with `os.ErrNotExist` / `tunnel_failed`, that is expected; do not revert Task 1. Still run `go test -count=1 ./internal/adminapi -run TestAdminSSHKeyImport` and confirm PASS.

- [ ] **Step 5: Commit**

Skip unless the user asked to commit.

---

### Task 3: PTY refuses unconfigured keys

**Files:**
- Modify: `internal/adminapi/sshpty.go`
- Modify: `internal/adminapi/sshpty_test.go`

**Interfaces:**
- Consumes: `(*sshkey.Store).Signer()` returning `os.ErrNotExist` when missing
- Produces: text frame `{"error":"key_unconfigured"}` after public-base check and **before** `openDeviceTunnel`
- Existing codes unchanged: `offline`, `public_base_unconfigured`, `auth_failed`, `tunnel_failed`

- [ ] **Step 1: Add / update PTY tests**

Add this helper near `newPTYSigner` in `internal/adminapi/sshpty_test.go`:

```go
func importPTYKey(t *testing.T, dataDir string) ssh.PublicKey {
	t.Helper()
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	block, err := ssh.MarshalPrivateKey(privateKey, "")
	if err != nil {
		t.Fatal(err)
	}
	keyStore, err := sshkey.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := keyStore.Import(pem.EncodeToMemory(block)); err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	return signer.PublicKey()
}
```

Add `"encoding/pem"` to imports.

Replace `TestSSHPtyConnectsUsingStoredKey` key setup (the `sshkey.Open` + `Signer()` block) with:

```go
	dataDir := t.TempDir()
	pub := importPTYKey(t, dataDir)
	sshAddr := startPTYSSHServer(t, pub, false)
```

Replace the same block in `TestSSHPtyReportsRemoteShellExit` with `importPTYKey` + `startPTYSSHServer(t, pub, true)`.

In `TestSSHPtyReportsOfflineDevice` and `TestSSHPtyReportsTunnelFailure`, set `dataDir := t.TempDir()`, call `importPTYKey(t, dataDir)`, and pass `DataDir: dataDir` in `config.Config` so missing-key does not mask those errors.

Replace `TestSSHPtyReportsRejectedStoredKey` so the NAS has a stored key that the fake sshd does **not** accept:

```go
func TestSSHPtyReportsRejectedStoredKey(t *testing.T) {
	dataDir := t.TempDir()
	importPTYKey(t, dataDir)
	sshAddr := startPTYSSHServer(t, newPTYSigner(t).PublicKey(), false)
	// ... existing port/tunnel/server/assertPTYError auth_failed ...
	server, token := newPTYHTTPServer(t, config.Config{
		JWTSecret:     ptyTestSecret,
		DataDir:       dataDir,
		PublicBaseURL: "https://admin.example.com",
	})
	conn := dialPTY(t, server.URL, token)
	defer conn.CloseNow()
	writePTYSize(t, conn)
	assertPTYError(t, conn, "auth_failed")
}
```

Add:

```go
func TestSSHPtyReportsUnconfiguredKeyWithoutOpeningTunnel(t *testing.T) {
	opened := false
	setTunnelOpener(t, func(context.Context, *athena.Hub, string) (string, int, func(), error) {
		opened = true
		return "", 0, func() {}, nil
	})
	server, token := newPTYHTTPServer(t, config.Config{
		JWTSecret:     ptyTestSecret,
		DataDir:       t.TempDir(),
		PublicBaseURL: "https://admin.example.com",
	})
	conn := dialPTY(t, server.URL, token)
	defer conn.CloseNow()
	writePTYSize(t, conn)
	assertPTYError(t, conn, "key_unconfigured")
	if opened {
		t.Fatal("opened tunnel before SSH key was configured")
	}
}
```

- [ ] **Step 2: Run PTY tests to verify the new case fails**

Run: `go test -count=1 ./internal/adminapi -run 'TestSSHPtyReportsUnconfiguredKey|TestSSHPtyConnectsUsingStoredKey'`

Expected: FAIL — `Signer()` no longer generates, connect tests fail until Import helper is used (if Step 1 tests are already saved they may fail on `key_unconfigured` vs greeting, or compile if helper is present but handler still auto-tunnels). The unconfigured test should fail until sshpty.go checks the key first.

- [ ] **Step 3: Check the key before opening the tunnel**

In `internal/adminapi/sshpty.go`, after the public-base check and **before** `openDeviceTunnel`:

```go
	keyStore, err := sshkey.Open(dataDir)
	if err != nil {
		writeSSHPtyError(r.Context(), conn, "tunnel_failed")
		return
	}
	signer, err := keyStore.Signer()
	if errors.Is(err, os.ErrNotExist) {
		writeSSHPtyError(r.Context(), conn, "key_unconfigured")
		return
	}
	if err != nil {
		writeSSHPtyError(r.Context(), conn, "tunnel_failed")
		return
	}
```

Add `"os"` to imports. Remove the later `sshkey.Open` / `Signer()` block that currently runs after the tunnel opens. Keep using this `signer` in `sshsession.Connect`.

- [ ] **Step 4: Run PTY and sshkey tests**

Run: `go test -count=1 ./internal/sshkey ./internal/adminapi`

Then: `go build -o bin/pilotserver ./cmd/pilotserver`

Expected: PASS; build succeeds.

- [ ] **Step 5: Commit**

Skip unless the user asked to commit.

---

### Task 4: Settings UI, i18n, and docs

**Files:**
- Modify: `web/admin/index.html`
- Modify: `web/admin/i18n.js`
- Modify: `web/admin/i18n_test.js`
- Modify: `web/admin/admin_test.go`
- Modify: `README.md`
- Modify: `docs/synology-dsm72-spk.md`
- Modify: `synology/build-spk.sh`

**Interfaces:**
- Consumes: GET/PUT/DELETE `/admin/api/ssh-key` JSON `{configured,fingerprint}`
- Produces: paste textarea `#ssh-private-key` (never filled from GET), `#ssh-key-state`, `#save-ssh-key`, `#clear-ssh-key`
- PTY error map includes `key_unconfigured: "ssh.keyUnconfigured"`
- `ssh.authFailed` copy talks about GitHub username / GitHub public key / SSH enabled — not NAS public key

- [ ] **Step 1: Write failing admin/i18n tests**

In `web/admin/i18n_test.js`, replace the ssh key keys in `required` with:

```js
    "settings.sshKeyTitle", "settings.sshKeyHelp", "settings.sshPrivateKey",
    "settings.saveSSHKey", "settings.clearSSHKey", "settings.clearSSHKeyConfirm",
    "settings.sshKeyConfigured", "settings.sshKeyMissing", "settings.sshKeySaved",
    "settings.sshKeyCleared", "settings.sshKeyInvalid",
```

Remove `settings.sshPublicKey`, `settings.copySSHKey`, `settings.rotateSSHKey`, `settings.rotateSSHKeyConfirm`, `settings.sshKeyCopied`.

Add `"ssh.keyUnconfigured"` next to the other `ssh.*` keys.

Add a dictionary assertion (in the existing zh/en completeness test is enough if `required` is used for both locales).

In `web/admin/admin_test.go`, change `TestAdminHTMLContainsSSHKeyAndTerminalHooks` wants to:

```go
		`id="ssh-private-key"`,
		`id="ssh-key-state"`,
		`id="save-ssh-key"`,
		`id="clear-ssh-key"`,
		`devices.openTerminal`,
		`id="view-ssh"`,
		`src="/admin/vendor/xterm.min.js"`,
		`/admin/api/devices/${encodeURIComponent(device.dongle_id)}/ssh/pty?access_token=`,
		`key_unconfigured: "ssh.keyUnconfigured"`,
		`api("/admin/api/ssh-key", {method: "PUT"`,
		`api("/admin/api/ssh-key", {method: "DELETE"`,
```

Keep forbidding `"PRIVATE KEY"` and `"id_ed25519"`.

Replace `TestAdminHTMLDisablesSSHKeyRotateUntilRequestFinishes` with:

```go
func TestAdminHTMLDisablesSSHKeySaveUntilRequestFinishes(t *testing.T) {
	html := adminInlineHTML(t)
	handler := adminFunction(t, html,
		`document.querySelector("#save-ssh-key").addEventListener`,
		`document.querySelector("#clear-ssh-key").addEventListener`)
	disabled := strings.Index(handler, `button.disabled = true;`)
	request := strings.Index(handler, `api("/admin/api/ssh-key", {method: "PUT"`)
	finally := strings.Index(handler, `} finally {`)
	enabled := strings.Index(handler, `button.disabled = false;`)
	if disabled < 0 || request < 0 || disabled > request {
		t.Error("save handler must disable the button before starting the request")
	}
	if finally < 0 || enabled < finally {
		t.Error("save handler must re-enable the button in finally")
	}
}
```

- [ ] **Step 2: Run frontend tests to verify they fail**

Run: `go test -count=1 ./web/admin`

Expected: FAIL on missing hooks / i18n keys.

- [ ] **Step 3: Update i18n, HTML, and docs**

In `web/admin/i18n.js` replace the SSH settings keys in **both** locales:

zh-CN:

```js
      "settings.sshKeyTitle": "SSH 钥匙",
      "settings.sshKeyHelp": "车上只需填写 GitHub 用户名并打开 SSH。把本机已添加到 GitHub 的 OpenSSH 私钥粘贴到这里保存；私钥只留在 NAS，页面不会回显。",
      "settings.sshPrivateKey": "私钥",
      "settings.saveSSHKey": "保存钥匙",
      "settings.clearSSHKey": "清除钥匙",
      "settings.clearSSHKeyConfirm": "清除后需要重新粘贴私钥才能开终端。继续？",
      "settings.sshKeyConfigured": "已配置 {fingerprint}",
      "settings.sshKeyMissing": "未配置私钥",
      "settings.sshKeySaved": "已保存",
      "settings.sshKeyCleared": "已清除",
      "settings.sshKeyInvalid": "私钥无效或带口令，请粘贴未加密的 OpenSSH 私钥",
      "ssh.keyUnconfigured": "请先在设置中保存 GitHub SSH 私钥",
      "ssh.authFailed": "认证失败，请确认车上已填写 GitHub 用户名、该账号已添加对应公钥，并且已打开 SSH",
```

en:

```js
      "settings.sshKeyTitle": "SSH key",
      "settings.sshKeyHelp": "The car only needs a GitHub username with SSH enabled. Paste the OpenSSH private key whose public key is on GitHub. The private key stays on the NAS and is never shown again.",
      "settings.sshPrivateKey": "Private key",
      "settings.saveSSHKey": "Save key",
      "settings.clearSSHKey": "Clear key",
      "settings.clearSSHKeyConfirm": "Clearing the key means you must paste it again before opening a terminal. Continue?",
      "settings.sshKeyConfigured": "Configured {fingerprint}",
      "settings.sshKeyMissing": "No private key configured",
      "settings.sshKeySaved": "Saved",
      "settings.sshKeyCleared": "Cleared",
      "settings.sshKeyInvalid": "Invalid or passphrase-protected key. Paste an unencrypted OpenSSH private key.",
      "ssh.keyUnconfigured": "Save the GitHub SSH private key in Settings first",
      "ssh.authFailed": "Authentication failed. Confirm the car has your GitHub username, that account has the matching public key, and SSH is enabled.",
```

Delete the old public-key / rotate / copied keys.

In `web/admin/index.html`:

CSS: change `#ssh-public-key` to `#ssh-private-key`.

Settings panel markup:

```html
              <section class="ssh-key-panel">
                <h2 data-i18n="settings.sshKeyTitle">SSH key</h2>
                <p data-i18n="settings.sshKeyHelp">The car only needs a GitHub username with SSH enabled. Paste the OpenSSH private key whose public key is on GitHub. The private key stays on the NAS and is never shown again.</p>
                <p id="ssh-key-state" aria-live="polite"></p>
                <label><span data-i18n="settings.sshPrivateKey">Private key</span><textarea id="ssh-private-key" spellcheck="false" autocomplete="off"></textarea></label>
                <button id="save-ssh-key" type="button" data-i18n="settings.saveSSHKey">Save key</button>
                <button id="clear-ssh-key" type="button" data-i18n="settings.clearSSHKey">Clear key</button>
                <span id="ssh-key-status" aria-live="polite"></span>
              </section>
```

Do not put PEM header text in HTML.

JS: keep `let sshKeyInfo = {configured: false, fingerprint: ""};`

```js
    function renderSSHKeyState() {
      const state = document.querySelector("#ssh-key-state");
      const input = document.querySelector("#ssh-private-key");
      input.value = "";
      if (sshKeyInfo.configured) {
        setLocalizedText(state, "settings.sshKeyConfigured", {fingerprint: sshKeyInfo.fingerprint});
      } else {
        setLocalizedText(state, "settings.sshKeyMissing");
      }
    }
```

Call `renderSSHKeyState()` from `applyLanguage` (after `renderCurrentView`) so language switch keeps the fingerprint.

In `clearSession`, replace `#ssh-public-key` with `#ssh-private-key` and `sshKeyInfo = {configured: false, fingerprint: ""}; renderSSHKeyState();`

In `loadSettings`, replace `sshKey.public_key` assignment with:

```js
        sshKeyInfo = {
          configured: !!sshKey.configured,
          fingerprint: sshKey.fingerprint || ""
        };
        renderSSHKeyState();
        clearLocalizedText(document.querySelector("#ssh-key-status"));
```

Replace copy/rotate listeners with:

```js
    document.querySelector("#save-ssh-key").addEventListener("click", async (event) => {
      const button = event.currentTarget;
      button.disabled = true;
      const status = document.querySelector("#ssh-key-status");
      try {
        const result = await api("/admin/api/ssh-key", {
          method: "PUT",
          body: JSON.stringify({private_key: document.querySelector("#ssh-private-key").value})
        });
        sshKeyInfo = {
          configured: !!result.configured,
          fingerprint: result.fingerprint || ""
        };
        renderSSHKeyState();
        setLocalizedText(status, "settings.sshKeySaved");
      } catch (error) {
        if (error === staleSession) return;
        if (error && error.code === "http_400") {
          setLocalizedText(status, "settings.sshKeyInvalid");
          return;
        }
        setLocalizedError(status, error, "errors.generic");
      } finally {
        button.disabled = false;
      }
    });
    document.querySelector("#clear-ssh-key").addEventListener("click", async () => {
      if (!confirm(i18n.t("settings.clearSSHKeyConfirm"))) return;
      const status = document.querySelector("#ssh-key-status");
      try {
        await api("/admin/api/ssh-key", {method: "DELETE"});
        sshKeyInfo = {configured: false, fingerprint: ""};
        renderSSHKeyState();
        setLocalizedText(status, "settings.sshKeyCleared");
      } catch (error) {
        if (error === staleSession) return;
        setLocalizedError(status, error, "errors.generic");
      }
    });
```

Check how `requestError` stores `code`. The existing `api()` throws `{code: "http_400"}`. If the thrown object uses a different shape, match that shape (read `requestError` in `index.html` and use the same field). Do not stringify the private key into logs.

In the PTY `message.error` map add `key_unconfigured: "ssh.keyUnconfigured"`.

`synology/build-spk.sh`: `VERSION="${PILOTSERVER_SPK_VERSION:-1.0.21-1}"`

`README.md` and `docs/synology-dsm72-spk.md`: replace `1.0.20-1` with `1.0.21-1`. Replace the SSH instructions with:

> 车上填写 GitHub 用户名并打开 SSH。在管理后台「设置」粘贴已添加到 GitHub 的 OpenSSH 私钥并保存（私钥只留在 NAS）。设备在线后点「开终端」。原有可选的复制 SSH 命令方式仍需放行 `41000–41099` 端口，本机终端使用同一把私钥。

- [ ] **Step 4: Run tests and build**

Run:

```bash
go test -count=1 ./internal/sshkey ./internal/adminapi ./internal/sshsession ./web/admin
go build -o bin/pilotserver ./cmd/pilotserver
```

Expected: all PASS; `bin/pilotserver` built.

- [ ] **Step 5: Commit**

Skip unless the user asked to commit.

---

## Self-review

| Spec item | Task |
|-----------|------|
| Import unencrypted OpenSSH private key | 1, 2, 4 |
| GET never returns private key; fingerprint only | 1, 2 |
| PUT invalid/passphrase → 400, no overwrite | 1, 2 |
| DELETE clears private + leftover `.pub` | 1, 2 |
| No auto-generate / rotate / public key copy | 1, 2, 4 |
| `key_unconfigured` before tunnel | 3 |
| `auth_failed` GitHub copy | 4 |
| PTY protocol / xterm / copy ssh command unchanged | 3, 4 (no protocol edits) |
| Docs + SPK `1.0.21-1` | 4 |
| Do not read `~/.ssh/` | Global + tests generate keys |

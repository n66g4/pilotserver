# Admin Web SSH Terminal Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let an admin open a browser terminal to an online comma device over the existing Athena SSH tunnel, using an NAS-held Ed25519 key that never enters the browser.

**Architecture:** Add a small key store under the data directory, extend admin auth so WebSockets can pass `access_token`, open the existing `startLocalProxy` tunnel, then dial `comma@127.0.0.1:<port>` with `golang.org/x/crypto/ssh`. The admin page vendors xterm.js like hls.js and pipes PTY bytes over an authenticated WebSocket.

**Tech Stack:** Go 1.25, `golang.org/x/crypto/ssh`, `github.com/coder/websocket`, embedded admin HTML/JS, vendored xterm, existing Athena tunnel.

## Global Constraints

- Do not change existing device HTTP API paths, request bodies, Athena JSON-RPC `startLocalProxy` shape, HLS tickets, or telemetry data.
- Do not add a frontend framework, package manager, external font, icon CDN, or runtime npm dependency.
- Do not put the SSH private key in any JSON response or in the DOM.
- Supported admin locales remain exactly `zh-CN` and `en`.
- Every new interactive target is at least 40px.
- SSH user is `comma`; host key checks against the loopback tunnel are ignored because Athena already authenticated the device.
- Run `go build -o bin/pilotserver ./cmd/pilotserver` after every production-code edit.
- Do not create a Git commit unless the user separately requests one.
- Release artifact version is `1.0.20-1`.

---

## File Map

- Create `internal/sshkey/store.go`: Ed25519 generate/load/rotate; public key only.
- Create `internal/sshkey/store_test.go`.
- Create `internal/sshsession/session.go`: dial `comma@addr` with a signer and PTY.
- Create `internal/sshsession/session_test.go`: fake sshd.
- Modify `internal/adminapi/auth_middleware.go`: accept `access_token` query.
- Modify `internal/adminapi/handler.go`: mount ssh-key and pty routes.
- Create `internal/adminapi/sshkey.go` and `sshpty.go`.
- Create `internal/adminapi/sshkey_test.go` and `sshpty_test.go`.
- Modify `web/admin/admin.go`: embed xterm assets.
- Modify `web/admin/index.html`, `i18n.js`, `i18n_test.js`, `admin_test.go`.
- Create `web/admin/vendor/xterm.min.js`, `xterm.css`, `xterm.LICENSE.txt`.
- Modify `synology/build-spk.sh`, `README.md`, `docs/synology-dsm72-spk.md`.

---

### Task 1: NAS SSH Key Store and Admin Key API

**Files:**
- Create: `internal/sshkey/store.go`
- Create: `internal/sshkey/store_test.go`
- Create: `internal/adminapi/sshkey.go`
- Create: `internal/adminapi/sshkey_test.go`
- Modify: `internal/adminapi/handler.go`

**Interfaces:**
- Produces: `sshkey.Open(dataDir string) (*Store, error)`
- Produces: `(*Store) PublicKey() (string, error)` — OpenSSH authorized_keys line; creates the keypair if missing
- Produces: `(*Store) Rotate() (string, error)`
- Produces: `(*Store) Signer() (ssh.Signer, error)`
- Produces: `GET /admin/api/ssh-key` → `{"public_key":"ssh-ed25519 ..."}`
- Produces: `POST /admin/api/ssh-key/rotate` → same JSON
- Private file: `$dataDir/ssh/id_ed25519` mode `0600`
- Public file: `$dataDir/ssh/id_ed25519.pub` mode `0644`

- [ ] **Step 1: Write failing key-store tests**

```go
func TestOpenCreatesEd25519AndNeverExposesPrivateKey(t *testing.T) {
	dir := t.TempDir()
	store, err := sshkey.Open(dir)
	if err != nil { t.Fatal(err) }
	pub, err := store.PublicKey()
	if err != nil { t.Fatal(err) }
	if !strings.HasPrefix(pub, "ssh-ed25519 ") {
		t.Fatalf("public_key = %q", pub)
	}
	info, err := os.Stat(filepath.Join(dir, "ssh", "id_ed25519"))
	if err != nil { t.Fatal(err) }
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("perm = %o", info.Mode().Perm())
	}
	again, err := sshkey.Open(dir)
	if err != nil { t.Fatal(err) }
	second, err := again.PublicKey()
	if err != nil { t.Fatal(err) }
	if second != pub {
		t.Fatal("reopen changed public key")
	}
	rotated, err := store.Rotate()
	if err != nil { t.Fatal(err) }
	if rotated == pub {
		t.Fatal("rotate returned the same public key")
	}
}
```

- [ ] **Step 2: Run the test and observe RED**

```bash
go test -count=1 ./internal/sshkey
```

Expected: FAIL because `internal/sshkey` does not exist.

- [ ] **Step 3: Implement the store**

Use `crypto/ed25519` + `golang.org/x/crypto/ssh.MarshalAuthorizedKey` / `ssh.ParsePrivateKey`. Write private key with `ssh.MarshalPrivateKey` (or OpenSSH PEM via `ssh.MarshalPrivateKeyWithPassphrase` only if empty passphrase). No passphrase. Create `$dataDir/ssh` with `0700`.

- [ ] **Step 4: Write failing admin API tests**

In `sshkey_test.go`, reuse `mountAdmin` from `handler_test.go`. Assert:

- `GET /admin/api/ssh-key` without JWT → 401
- with JWT → 200, body has `public_key` prefix `ssh-ed25519 `, body does not contain `PRIVATE KEY`
- `POST /admin/api/ssh-key/rotate` changes `public_key`
- GET after rotate returns the new key

- [ ] **Step 5: Mount handlers**

```go
adminMux.HandleFunc("GET /admin/api/ssh-key", func(w http.ResponseWriter, r *http.Request) {
	handleGetSSHKey(w, cfg.DataDir)
})
adminMux.HandleFunc("POST /admin/api/ssh-key/rotate", func(w http.ResponseWriter, r *http.Request) {
	handleRotateSSHKey(w, cfg.DataDir)
})
```

`handleGetSSHKey` / `handleRotateSSHKey` call `sshkey.Open(dataDir)`. Tests must pass `cfg.DataDir` as `t.TempDir()` in `mountAdmin` callers that already set `cfg.DataDir`, or set it inside the new tests.

- [ ] **Step 6: Verify and compile**

```bash
go test -count=1 ./internal/sshkey ./internal/adminapi
go build -o bin/pilotserver ./cmd/pilotserver
```

Expected: PASS, exit 0.

---

### Task 2: Authenticated PTY WebSocket over the Existing Tunnel

**Files:**
- Create: `internal/sshsession/session.go`
- Create: `internal/sshsession/session_test.go`
- Create: `internal/adminapi/sshpty.go`
- Create: `internal/adminapi/sshpty_test.go`
- Modify: `internal/adminapi/auth_middleware.go`
- Modify: `internal/adminapi/handler.go`

**Interfaces:**
- Produces: `sshsession.Connect(ctx context.Context, addr string, signer ssh.Signer, cols, rows int) (*Session, error)`
- `Session` methods: `Stdin() io.WriteCloser`, `Stdout() io.Reader`, `Resize(cols, rows int) error`, `Wait() error`, `Close() error`
- User: `comma`
- HostKeyCallback: `ssh.InsecureIgnoreHostKey()` (loopback tunnel only)
- Produces: `GET /admin/api/devices/{dongleID}/ssh/pty` WebSocket
- Protocol:
  1. Client text: `{"cols":80,"rows":24}`
  2. Server text success: `{"host":"...","port":41017,"expires_in":600}`
  3. Then binary PTY bytes both ways; optional later text `{"cols":n,"rows":n}`
  4. Failure text: `{"error":"offline"|"public_base_unconfigured"|"auth_failed"|"tunnel_failed"}` then close
- `requireAdmin` also accepts `access_token` query when `Authorization` is absent
- Tunnel: existing `hub.OpenSSHTunnel`; dial `127.0.0.1:<port>`
- On WebSocket close: `Session.Close()` and tunnel cancel

- [ ] **Step 1: Extend admin auth for WebSocket query tokens**

Write a failing test that `GET /admin/api/ssh-key?access_token=<jwt>` without Authorization header succeeds, and a bogus token still 401.

Update `requireAdmin`:

```go
token := ""
scheme, value, ok := strings.Cut(r.Header.Get("Authorization"), " ")
if ok && strings.EqualFold(scheme, "Bearer") {
	token = value
}
if token == "" {
	token = r.URL.Query().Get("access_token")
}
if token == "" || auth.ParseAdminJWT(jwtSecret, token) != nil {
	http.Error(w, "unauthorized", http.StatusUnauthorized)
	return
}
```

- [ ] **Step 2: Write failing sshsession tests against a fake sshd**

Use `golang.org/x/crypto/ssh` server on `127.0.0.1:0`. Accept the store's public key. On `SessionRequest` with `shell`, copy stdin to stdout with a `ready\n` greeting. Assert `Connect` reads `ready\n` after writing nothing, and a wrong signer fails.

- [ ] **Step 3: Implement `sshsession.Connect`**

```go
client, err := ssh.Dial("tcp", addr, &ssh.ClientConfig{
	User:            "comma",
	Auth:            []ssh.AuthMethod{ssh.PublicKeys(signer)},
	HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	Timeout:         10 * time.Second,
})
session, err := client.NewSession()
session.RequestPty("xterm-256color", rows, cols, ssh.TerminalModes{
	ssh.ECHO: 1, ssh.TTY_OP_ISPEED: 14400, ssh.TTY_OP_OSPEED: 14400,
})
stdin, _ := session.StdinPipe()
stdout, _ := session.StdoutPipe()
session.Shell()
```

Map auth failures to a sentinel `sshsession.ErrAuthFailed` so the WS handler can send `auth_failed`.

- [ ] **Step 4: Write failing PTY WebSocket tests**

Use `httptest.NewServer(mux)` (not `ResponseRecorder`) plus `websocket.Dial`. Inject a test-only tunnel opener by putting this unexported func var in `sshpty.go`:

```go
var openDeviceTunnel = defaultOpenDeviceTunnel

func defaultOpenDeviceTunnel(ctx context.Context, hub *athena.Hub, dongleID string) (addr string, port int, cancel func(), err error)
```

In the test, set `openDeviceTunnel` to return the fake sshd address and restore it with `t.Cleanup`. Cases:

- no JWT → 401
- offline device → WS text `{"error":"offline"}`
- missing public base URL → `{"error":"public_base_unconfigured"}`
- online + valid key: send `{"cols":80,"rows":24}`, receive success JSON with `port` and `host`, then binary/text greeting from fake sshd
- wrong NAS key: `{"error":"auth_failed"}`

Map `athena.ErrOffline` to `offline`. Reuse `url.Parse(baseURL.Get())` the same way `handleOpenSSH` rejects empty host.

- [ ] **Step 5: Implement `handleSSHPty` and mount it**

```go
adminMux.HandleFunc("GET /admin/api/devices/{dongleID}/ssh/pty", func(w http.ResponseWriter, r *http.Request) {
	handleSSHPty(w, r, hub, baseURL, cfg.DataDir)
})
```

After `websocket.Accept`, read the first text JSON for cols/rows (default 80×24 if omitted). Open tunnel, load signer, `sshsession.Connect`. Send success JSON using `baseURL` hostname and tunnel port. Copy stdout → binary WS frames; WS binary → stdin; WS text resize → `Session.Resize`. `expires_in` is `600`.

Do not log private keys or PTY payload.

- [ ] **Step 6: Verify and compile**

```bash
go test -count=1 ./internal/sshsession ./internal/adminapi
go build -o bin/pilotserver ./cmd/pilotserver
```

Expected: PASS, exit 0.

---

### Task 3: Settings Public Key UI and In-Page Terminal

**Files:**
- Create: `web/admin/vendor/xterm.min.js`
- Create: `web/admin/vendor/xterm.css`
- Create: `web/admin/vendor/xterm.LICENSE.txt`
- Modify: `web/admin/admin.go`
- Modify: `web/admin/admin_test.go`
- Modify: `web/admin/i18n.js`
- Modify: `web/admin/i18n_test.js`
- Modify: `web/admin/index.html`

**Interfaces:**
- Vendor xterm 5.3.0 (MIT) into `web/admin/vendor/`, served like hls.js (immutable cache)
- Settings: `#ssh-public-key` readonly, copy, rotate with confirm
- Devices: existing SSH button stays for copy-command; add `#`/`开终端` button `devices.openTerminal` enabled only when `device.online`
- View `#view-ssh` inside `#workspace`, opened via `setView("ssh")` (same shell rules as replay: leave on nav away)
- WS URL: `/admin/api/devices/${dongleID}/ssh/pty?access_token=${token}`
- First client message: `JSON.stringify({cols, rows})`
- i18n keys (both locales, identical key sets):

```text
settings.sshKeyTitle, settings.sshKeyHelp, settings.sshPublicKey,
settings.copySSHKey, settings.rotateSSHKey, settings.rotateSSHKeyConfirm,
settings.sshKeyCopied, devices.openTerminal, ssh.title, ssh.copyCommand,
ssh.close, ssh.connecting, ssh.offline, ssh.publicBaseUnconfigured,
ssh.authFailed, ssh.tunnelFailed
```

Chinese strings:

```text
settings.sshKeyTitle = SSH 钥匙
settings.sshKeyHelp = 把公钥加到车上 comma 用户的授权钥匙并打开 SSH。私钥只保存在 NAS。
settings.sshPublicKey = 公钥
settings.copySSHKey = 复制公钥
settings.rotateSSHKey = 重新生成
settings.rotateSSHKeyConfirm = 重新生成后旧钥匙立即失效，车上需要更新公钥。继续？
settings.sshKeyCopied = 已复制公钥
devices.openTerminal = 开终端
ssh.title = SSH 终端
ssh.copyCommand = 复制命令
ssh.close = 关闭
ssh.connecting = 正在连接…
ssh.offline = 设备离线
ssh.publicBaseUnconfigured = 未配置公网地址
ssh.authFailed = 认证失败，请确认车上已收录 NAS 公钥并已打开 SSH
ssh.tunnelFailed = 无法建立 SSH 隧道
```

English: `SSH key` / `Add this public key to the comma user's authorized keys and enable SSH. The private key stays on the NAS.` / `Public key` / `Copy public key` / `Regenerate` / `Regenerating replaces the key immediately; update the car's authorized keys. Continue?` / `Public key copied` / `Open terminal` / `SSH terminal` / `Copy command` / `Close` / `Connecting…` / `Device is offline` / `Public base URL is not configured` / `Authentication failed. Confirm the NAS public key is on the device and SSH is enabled.` / `Could not open the SSH tunnel`

Map WS `error` codes: `offline` → `ssh.offline`, `public_base_unconfigured` → `ssh.publicBaseUnconfigured`, `auth_failed` → `ssh.authFailed`, `tunnel_failed` → `ssh.tunnelFailed`.

- [ ] **Step 1: Vendor xterm and fail asset tests**

Download MIT-licensed xterm 5.3.0 browser build into `web/admin/vendor/` (js + css + license text). Do not add `package.json`. Extend `admin.go` / `TestServeHTTPEmbeddedAssets` for:

- `/admin/vendor/xterm.min.js`
- `/admin/vendor/xterm.css`
- `/admin/vendor/xterm.LICENSE.txt`

- [ ] **Step 2: Add dictionary keys and a failing i18n parity/value test**

Assert `zh.t("devices.openTerminal") === "开终端"` and key parity still holds.

- [ ] **Step 3: Write failing HTML hook tests in `admin_test.go`**

Assert presence of:

```text
id="ssh-public-key"
id="copy-ssh-key"
id="rotate-ssh-key"
devices.openTerminal
id="view-ssh"
src="/admin/vendor/xterm.min.js"
/admin/api/devices/${encodeURIComponent(device.dongle_id)}/ssh/pty?access_token=
```

Assert the page does not contain `PRIVATE KEY` or `id_ed25519` file contents.

- [ ] **Step 4: Wire settings and terminal view**

Load public key in `loadSettings` via `GET /admin/api/ssh-key`. Copy uses `navigator.clipboard.writeText`. Rotate: `confirm(i18n.t("settings.rotateSSHKeyConfirm"))` then `POST /admin/api/ssh-key/rotate`.

Open terminal: `setView("ssh")`, construct xterm in `#ssh-terminal`, connect WS, send `{cols, rows}` from `term.cols/rows`. On success JSON, show copyable `ssh -p ${port} comma@${host}`. Feed binary frames to `term.write`. `term.onData` sends binary. `term.onResize` sends text `{cols, rows}`. Close / leave view closes the WebSocket. Language switch must not close the socket or reset the terminal buffer (same rule as replay: `applyLanguage` must not call the closer).

Keep the existing 「开 SSH」 copy-command button.

- [ ] **Step 5: Verify and compile**

```bash
node --test web/admin/i18n_test.js
go test -count=1 ./web/admin ./internal/adminapi ./internal/sshkey ./internal/sshsession
go build -o bin/pilotserver ./cmd/pilotserver
```

Expected: PASS, exit 0.

---

### Task 4: Docs, SPK 1.0.20-1, and Verification

**Files:**
- Modify: `synology/build-spk.sh`
- Modify: `README.md`
- Modify: `docs/synology-dsm72-spk.md`
- Create: `.superpowers/sdd/admin-web-ssh-report.md`

**Interfaces:**
- Produces: `bin/pilotserver`
- Produces: `dist/pilotserver-1.0.20-1-x64.spk`

- [ ] **Step 1: Update version and docs**

Set `VERSION="${PILOTSERVER_SPK_VERSION:-1.0.20-1}"`. Document: generate/copy NAS public key onto the car, enable SSH, use 开终端; 41000–41099 still required for the optional copy-command path; private key stays on NAS.

- [ ] **Step 2: Run verification**

```bash
node --test web/admin/i18n_test.js
go test -count=1 ./...
go test -race -count=1 ./internal/sshkey ./internal/sshsession ./internal/adminapi ./web/admin
go vet ./...
go build -o bin/pilotserver ./cmd/pilotserver
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /tmp/pilotserver-linux-amd64 ./cmd/pilotserver
rm -f /tmp/pilotserver-linux-amd64
./synology/build-spk.sh
shasum -a 256 bin/pilotserver dist/pilotserver-1.0.20-1-x64.spk
```

Expected: all exit 0. Record hashes in the report.

- [ ] **Step 3: Write the verification report**

Include RED/GREEN per task, key-permission evidence, WS protocol cases, and the car-side reminder (authorized_keys + SSH enabled). Do not include private keys or production JWTs.

---

# PilotServer Phase-1 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在本仓库落地可部署的 Go 单体 `pilotserver`：设备配对、Athena 在线、外网 SSH 隧道、签名上传、OTA、billing/maps 桩、极简管理端，并配合已有 Nginx 暴露。

**Architecture:** 单一 Go 进程监听 `127.0.0.1:8080`；SQLite + 本地目录持久化；设备经 fork 默认 URL 接入；SSH 经 Athena `startLocalProxy` + 短时 TCP 端口。

**Tech Stack:** Go 1.22+、`net/http`、`coder/websocket` 或 `gorilla/websocket`、`modernc.org/sqlite`、JWT（`golang-jwt/jwt/v5`）、标准 `testing`。

**Spec:** `docs/superpowers/specs/2026-08-11-self-hosted-openpilot-server-design.md`

## Global Constraints

- 设备侧零额外可执行文件；不内嵌 Nginx/Caddy；Go 只绑回环地址。
- 个人规模 1–5 台；单管理员；地图/billing 仅桩。
- 每次完成可编译任务后必须执行：`go build -o bin/pilotserver ./cmd/pilotserver`（用户规则）。
- 协议字段以 DragonPilot fork 为准；本计划先实现可联调的兼容子集，路径可用配置覆盖。
- YAGNI：不做多租户、不做对象存储、不做官方手机 App。

## File Structure

```text
go.mod
cmd/pilotserver/main.go
internal/config/config.go
internal/store/store.go
internal/store/schema.sql
internal/auth/device_jwt.go
internal/auth/admin.go
internal/api/pair.go
internal/api/device.go
internal/api/routes.go
internal/athena/hub.go
internal/athena/handler.go
internal/athena/ssh_tunnel.go
internal/upload/sign.go
internal/upload/handler.go
internal/ota/handler.go
internal/billing/stub.go
internal/maps/stub.go
internal/adminapi/handler.go
web/admin/index.html
deploy/nginx.example.conf
docs/dragonpilot-fork-urls.md
bin/pilotserver                 # build output (gitignore)
data/                           # runtime (gitignore)
```

---

### Task 1: Go 模块骨架 + 健康检查

**Files:**
- Create: `go.mod`
- Create: `cmd/pilotserver/main.go`
- Create: `internal/config/config.go`
- Create: `.gitignore`
- Test: `internal/config/config_test.go`

**Interfaces:**
- Produces: `config.Load() (Config, error)`；`Config` 含 `ListenAddr`（默认 `127.0.0.1:8080`）、`DataDir`、`PublicBaseURL`、`JWTSecret`、`AdminPasswordHash`

- [ ] **Step 1: 写失败测试**

```go
// internal/config/config_test.go
package config_test

import (
	"os"
	"testing"

	"github.com/YOUR_USER/pilotserver/internal/config"
)

func TestLoadDefaults(t *testing.T) {
	t.Setenv("PILOTSERVER_LISTEN", "")
	t.Setenv("PILOTSERVER_DATA_DIR", t.TempDir())
	t.Setenv("PILOTSERVER_PUBLIC_BASE_URL", "https://op.example.com")
	t.Setenv("PILOTSERVER_JWT_SECRET", "test-secret-at-least-32-bytes-long!!")
	t.Setenv("PILOTSERVER_ADMIN_PASSWORD", "admin")

	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ListenAddr != "127.0.0.1:8080" {
		t.Fatalf("listen: %s", cfg.ListenAddr)
	}
}
```

（将 module path 换成实际：`pilotserver` 本地可用 `pilotserver`。）

- [ ] **Step 2: 跑测试确认失败**

Run: `cd /Users/gen/Downloads/pilotserver/pilotserver && go test ./internal/config/ -v`  
Expected: 失败（包不存在或 `Load` 未定义）

- [ ] **Step 3: 实现最小代码**

`go.mod`:
```go
module pilotserver

go 1.22
```

`internal/config/config.go`:
```go
package config

import (
	"fmt"
	"os"
)

type Config struct {
	ListenAddr        string
	DataDir           string
	PublicBaseURL     string
	JWTSecret         string
	AdminPassword     string // 明文仅用于首次启动哈希；生产用环境变量
	SSHTunnelPortMin  int
	SSHTunnelPortMax  int
}

func Load() (Config, error) {
	cfg := Config{
		ListenAddr:       envOr("PILOTSERVER_LISTEN", "127.0.0.1:8080"),
		DataDir:          envOr("PILOTSERVER_DATA_DIR", "./data"),
		PublicBaseURL:    os.Getenv("PILOTSERVER_PUBLIC_BASE_URL"),
		JWTSecret:        os.Getenv("PILOTSERVER_JWT_SECRET"),
		AdminPassword:    envOr("PILOTSERVER_ADMIN_PASSWORD", "changeme"),
		SSHTunnelPortMin: 41000,
		SSHTunnelPortMax: 41099,
	}
	if cfg.PublicBaseURL == "" {
		return cfg, fmt.Errorf("PILOTSERVER_PUBLIC_BASE_URL required")
	}
	if len(cfg.JWTSecret) < 32 {
		return cfg, fmt.Errorf("PILOTSERVER_JWT_SECRET must be >= 32 bytes")
	}
	return cfg, nil
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
```

`cmd/pilotserver/main.go`:
```go
package main

import (
	"log"
	"net/http"

	"pilotserver/internal/config"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	log.Printf("listening on %s", cfg.ListenAddr)
	log.Fatal(http.ListenAndServe(cfg.ListenAddr, mux))
}
```

`.gitignore`:
```
bin/
data/
*.db
```

- [ ] **Step 4: 测试通过并编译**

Run:
```bash
go test ./internal/config/ -v
go build -o bin/pilotserver ./cmd/pilotserver
```
Expected: PASS；生成 `bin/pilotserver`

- [ ] **Step 5: Commit**

```bash
git add go.mod go.sum cmd/ internal/config/ .gitignore
git commit -m "feat: scaffold pilotserver with config and healthz"
```

---

### Task 2: SQLite Store（设备表）

**Files:**
- Create: `internal/store/store.go`
- Create: `internal/store/schema.sql`
- Test: `internal/store/store_test.go`

**Interfaces:**
- Produces:
  - `store.Open(dataDir string) (*Store, error)`
  - `(*Store) UpsertDevice(d Device) error`
  - `(*Store) GetDevice(dongleID string) (Device, error)`
  - `(*Store) ListDevices() ([]Device, error)`
  - `type Device struct { DongleID, PublicKeyPEM, Alias string; CreatedAt int64 }`

- [ ] **Step 1: 写失败测试**

```go
package store_test

import (
	"testing"
	"time"

	"pilotserver/internal/store"
)

func TestUpsertAndGetDevice(t *testing.T) {
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	d := store.Device{
		DongleID:     "testdongle001",
		PublicKeyPEM: "-----BEGIN PUBLIC KEY-----\nMIIB\n-----END PUBLIC KEY-----",
		Alias:        "car1",
		CreatedAt:    time.Now().Unix(),
	}
	if err := s.UpsertDevice(d); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetDevice("testdongle001")
	if err != nil {
		t.Fatal(err)
	}
	if got.DongleID != d.DongleID || got.Alias != "car1" {
		t.Fatalf("got %+v", got)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/store/ -v`  
Expected: FAIL

- [ ] **Step 3: 实现 store**

`internal/store/schema.sql`:
```sql
CREATE TABLE IF NOT EXISTS devices (
  dongle_id TEXT PRIMARY KEY,
  public_key_pem TEXT NOT NULL,
  alias TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL
);
```

`internal/store/store.go`：用 `modernc.org/sqlite`，`Open` 时 `embed` 执行 schema；实现上述方法。`Close()` 关闭 DB。

依赖：
```bash
go get modernc.org/sqlite
```

- [ ] **Step 4: 测试通过**

Run: `go test ./internal/store/ -v`  
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/store/ go.mod go.sum
git commit -m "feat: add sqlite store for devices"
```

---

### Task 3: 设备 JWT + 管理员 Token

**Files:**
- Create: `internal/auth/device_jwt.go`
- Create: `internal/auth/admin.go`
- Test: `internal/auth/auth_test.go`

**Interfaces:**
- Produces:
  - `auth.IssueDeviceJWT(secret, dongleID string, ttl time.Duration) (string, error)`
  - `auth.ParseDeviceJWT(secret, token string) (dongleID string, err error)`
  - `auth.CheckAdminPassword(password, hash string) bool` — 第一期可用 `bcrypt`
  - `auth.HashPassword(password string) (string, error)`
  - `auth.IssueAdminJWT(secret string, ttl time.Duration) (string, error)`
  - `auth.ParseAdminJWT(secret, token string) error`

- [ ] **Step 1: 写失败测试**

```go
func TestDeviceJWTRoundTrip(t *testing.T) {
	secret := "test-secret-at-least-32-bytes-long!!"
	tok, err := auth.IssueDeviceJWT(secret, "dongleA", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	id, err := auth.ParseDeviceJWT(secret, tok)
	if err != nil || id != "dongleA" {
		t.Fatalf("id=%s err=%v", id, err)
	}
}

func TestAdminPassword(t *testing.T) {
	hash, err := auth.HashPassword("secret")
	if err != nil {
		t.Fatal(err)
	}
	if !auth.CheckAdminPassword("secret", hash) {
		t.Fatal("expected match")
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/auth/ -v`

- [ ] **Step 3: 实现**

使用 `github.com/golang-jwt/jwt/v5`：device claims 含 `identity: dongleID`（或 `dongle_id`，与 fork 对齐时再调 claim 名）；admin claims 含 `role: admin`。密码用 `golang.org/x/crypto/bcrypt`。

- [ ] **Step 4: 测试通过 + 编译**

```bash
go test ./internal/auth/ -v
go build -o bin/pilotserver ./cmd/pilotserver
```

- [ ] **Step 5: Commit**

```bash
git commit -am "feat: add device and admin JWT helpers"
```

---

### Task 4: 配对 API + 设备 JWT 签发

**Files:**
- Create: `internal/api/pair.go`
- Create: `internal/api/device.go`
- Modify: `cmd/pilotserver/main.go`（挂路由、注入 store/config）
- Test: `internal/api/pair_test.go`

**Interfaces:**
- Consumes: `store.UpsertDevice`、`auth.IssueDeviceJWT`
- Produces:
  - `POST /v2/pilotauth/`（或可配置路径）body: `{ "imei", "serial", "public_key", "register_token" }` → `{ "dongle_id", "access_token" }`
  - 第一期简化：用公钥 SHA256 前 16 hex 生成稳定 `dongle_id`；校验 `public_key` PEM 可解析即可
  - `GET /v1/me` 需 Bearer device JWT → `{ "dongle_id", ... }`

官方路径因版本而异；用常量集中在 `internal/api/paths.go`，便于对照 fork 修改。

- [ ] **Step 1: 写 httptest测试**

```go
func TestPairAndMe(t *testing.T) {
	// 启动 httptest.Server 挂 api.New(...).Routes()
	// POST 配对拿到 dongle_id + access_token
	// GET /v1/me 带 Authorization: JWT <token> 或 Bearer
	// 断言 dongle_id 一致且 store 中有设备
}
```

（实现时 Authorization 头格式对照 dragonpilot `Api` 类；常见为 cookie `jwt=` 或 `Authorization: JWT <tok>`。）

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/api/ -v`

- [ ] **Step 3: 实现 pair + me，并在 main 组装**

`main` 中：
```go
st, err := store.Open(cfg.DataDir)
// ...
apiHandler := api.New(st, cfg)
apiHandler.Mount(mux)
```

- [ ] **Step 4: 测试 + 编译**

```bash
go test ./internal/api/ -v
go build -o bin/pilotserver ./cmd/pilotserver
```

- [ ] **Step 5: Commit**

```bash
git commit -am "feat: add device pairing and /v1/me"
```

---

### Task 5: Athena WebSocket Hub + 在线状态

**Files:**
- Create: `internal/athena/hub.go`
- Create: `internal/athena/handler.go`
- Test: `internal/athena/hub_test.go`
- Modify: `cmd/pilotserver/main.go`

**Interfaces:**
- Produces:
  - `athena.NewHub() *Hub`
  - `(*Hub) SetOnline(dongleID string, conn Conn)`
  - `(*Hub) SetOffline(dongleID string)`
  - `(*Hub) IsOnline(dongleID string) bool`
  - `(*Hub) SendJSONRPC(dongleID string, method string, params any) (id string, err error)`
  - HTTP：`GET /ws/v2/{dongleID}` Upgrade；校验 JWT（query/cookie/`Authorization`）与 path 中 dongle 一致

JSON-RPC 入站：处理设备上报；出站：服务端推送 method。

- [ ] **Step 1: 写 Hub 单测（不依赖真 WS）**

```go
func TestHubOnlineOffline(t *testing.T) {
	h := athena.NewHub()
	if h.IsOnline("d1") {
		t.Fatal("expected offline")
	}
	h.SetOnline("d1", athena.NopConn{})
	if !h.IsOnline("d1") {
		t.Fatal("expected online")
	}
	h.SetOffline("d1")
	if h.IsOnline("d1") {
		t.Fatal("expected offline")
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/athena/ -v`

- [ ] **Step 3: 实现 Hub + WS handler**

依赖：`go get github.com/coder/websocket`

Handler 逻辑：
1. 从 URL 取 dongleID  
2. 解析 JWT，不匹配则 401  
3. Upgrade，注册 Hub  
4. 读循环：ping/pong 或 JSON-RPC response  
5. 断开时 SetOffline  

Mount：`mux.Handle("GET /ws/v2/{dongleID}", ...)`

- [ ] **Step 4: 增加管理查询在线接口（最小）**

`GET /admin/api/devices`（下一 Task 会加强鉴权）：返回 `[{dongle_id, online}]`。本 Task 可先内网无鉴权占位，Task 7 再加 admin JWT。

- [ ] **Step 5: 测试 + 编译 + Commit**

```bash
go test ./internal/athena/ -v
go build -o bin/pilotserver ./cmd/pilotserver
git commit -am "feat: add athena websocket hub and online presence"
```

---

### Task 6: SSH 隧道（startLocalProxy）

**Files:**
- Create: `internal/athena/ssh_tunnel.go`
- Create: `internal/adminapi/ssh.go`
- Test: `internal/athena/ssh_tunnel_test.go`
- Modify: `internal/athena/hub.go`（发送 RPC）
- Modify: `deploy` 文档片段（端口范围说明）

**Interfaces:**
- Produces:
  - `(*Hub) OpenSSHTunnel(ctx, dongleID string) (publicPort int, cancel func(), err error)`
  - 流程：在 `SSHTunnelPortMin–Max` 选空闲端口 → `net.Listen("tcp", "127.0.0.1:port")` → 向设备发 JSON-RPC `startLocalProxy`，params 含服务端侧 WS/TCP 桥接 URI（对齐 openpilot `athenad.startLocalProxy`：设备会再连回一个 remote WS URI）  
  - **对齐要点（实现时打开 dragonpilot `system/athena/athenad.py`）：** 官方是设备连 `remote_ws_uri` 做双向转发。本服务需提供第二个 WS 端点例如 `GET /ws/proxy/{ticket}`，Listen 到的 TCP 与该 proxy WS 互转字节流。
  - Admin：`POST /admin/api/devices/{dongleID}/ssh` → `{ "host": "<from PublicBaseURL host>", "port": 41005, "expires_in": 600 }`

- [ ] **Step 1: 写隧道端口分配测试**

```go
func TestPickPortInRange(t *testing.T) {
	p, err := athena.PickTCPPort(41000, 41099)
	if err != nil {
		t.Fatal(err)
	}
	if p < 41000 || p > 41099 {
		t.Fatalf("port %d", p)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

- [ ] **Step 3: 实现 PickTCPPort + Proxy ticket 表 + TCP↔WS 桥 + OpenSSHTunnel + admin POST**

最小可测桥接：
```go
// BridgeTCPAndWS copies bytes both ways until either side closes.
func BridgeTCPAndWS(tcp net.Conn, ws *websocket.Conn) 
```

JSON-RPC 请求形状（按 fork 调整）：
```json
{"jsonrpc":"2.0","id":"1","method":"startLocalProxy","params":{"remote_ws_uri":"wss://op.example.com/ws/proxy/TICKET","local_port":22}}
```
（若 fork 用位置参数数组，改为数组形式。）

- [ ] **Step 4: 集成测试（可选本地 mock conn）**

用假 `Conn` 记录 `SendJSONRPC` 是否发出 `startLocalProxy`；不断言真 SSH。

- [ ] **Step 5: 编译 + Commit**

```bash
go test ./internal/athena/ ./internal/adminapi/ -v
go build -o bin/pilotserver ./cmd/pilotserver
git commit -am "feat: add athena SSH tunnel via startLocalProxy"
```

---

### Task 7: 管理员登录 + 保护 admin API

**Files:**
- Create: `internal/adminapi/handler.go`
- Create: `internal/adminapi/auth_middleware.go`
- Modify: `cmd/pilotserver/main.go`（启动时 bcrypt hash 环境变量密码）
- Test: `internal/adminapi/handler_test.go`

**Interfaces:**
- `POST /admin/api/login` `{ "password" }` → `{ "token" }`
- 其余 `/admin/api/*` 需 `Authorization: Bearer <adminJWT>`
- `GET /admin/api/devices` → 列表含 online

- [ ] **Step 1: 写登录与未授权测试**

```go
func TestAdminLoginAndList(t *testing.T) { /* login 200 + list 200；无 token list 401 */ }
```

- [ ] **Step 2–4: 实现、测试、编译**

- [ ] **Step 5: Commit**

```bash
git commit -am "feat: protect admin API with password login"
```

---

### Task 8: 签名上传 + 行程元数据

**Files:**
- Create: `internal/upload/sign.go`
- Create: `internal/upload/handler.go`
- Create: `internal/store/routes.go`（routes/segments 表）
- Create: `internal/api/routes.go`
- Test: `internal/upload/upload_test.go`

**Interfaces:**
- `POST /v1.1/devices/{dongle}/upload_url/`（路径可调）→ `{ "url": "https://.../upload/put/TOKEN", "headers": {} }`
- `PUT /upload/put/{token}`：校验 HMAC token（含 dongle、path、exp）后写入 `data/uploads/{dongle}/{route}/{seg}/{filename}`
- `GET /v1/devices/{dongle}/routes` → 列表
- Store: `InsertSegment`、`ListRoutes`

- [ ] **Step 1: 签名往返测试**

```go
func TestSignAndVerifyUploadToken(t *testing.T) {
	tok, err := upload.Sign(secret, upload.Claim{DongleID: "d", RelPath: "r/0/rlog.bz2", Exp: time.Now().Add(time.Hour).Unix()})
	// Verify 成功；过期失败
}
```

- [ ] **Step 2–4: 实现落盘 + API + 测试 + 编译**

- [ ] **Step 5: Commit**

```bash
git commit -am "feat: add signed upload URLs and route listing"
```

---

### Task 9: OTA 元数据与产物

**Files:**
- Create: `internal/ota/handler.go`
- Create: `data/ota/.gitkeep`（说明目录）
- Test: `internal/ota/handler_test.go`
- Docs note in `docs/dragonpilot-fork-urls.md`

**Interfaces:**
- 产物放 `cfg.DataDir/ota/{channel}/`，含 `version.json`：`{"version":"0.9.x","notes":"...","download_url":"https://.../ota/files/..."}`
- `GET /ota/{channel}/version` 读该文件（或目录最新）
- `GET /ota/files/...` `http.FileServer` 或 Nginx 静态（示例里两种都写）

第一期：人工把 release tarball 放进目录并写 `version.json`；不做自动从 GitHub 同步。

- [ ] **Step 1: 测试 version 接口**

把临时目录写入 `version.json`，请求返回 version 字段。

- [ ] **Step 2–4: 实现、测试、编译**

- [ ] **Step 5: Commit**

```bash
git commit -am "feat: add OTA version endpoint and file serving"
```

---

### Task 10: billing / maps 桩

**Files:**
- Create: `internal/billing/stub.go`
- Create: `internal/maps/stub.go`
- Test: `internal/billing/stub_test.go`

**Interfaces:**
- billing：凡订阅相关路径返回「有效 Prime」最小 JSON（对照 fork 请求路径，未知路径可挂通配返回 `{"is_prime":true}`）
- maps：`GET` 导航相关返回 `{}` 或 204，保证不 5xx

- [ ] **Step 1–4: 测试 + 实现 + 编译**

- [ ] **Step 5: Commit**

```bash
git commit -am "feat: stub billing and maps endpoints"
```

---

### Task 11: 极简管理端页面

**Files:**
- Create: `web/admin/index.html`
- Modify: `cmd/pilotserver/main.go` 挂 `GET /admin/` → `embed` 或 `http.FileServer`

**功能（单页即可）：**
1. 登录  
2. 设备列表（dongle、online）  
3. 按钮「开 SSH」→ 显示 `ssh -p PORT comma@HOST`  
4. 链到行程列表 JSON（可读即可）

- [ ] **Step 1: 手写最小 HTML+JS（fetch admin API）**
- [ ] **Step 2: 本地启动验证**

```bash
export PILOTSERVER_PUBLIC_BASE_URL=http://127.0.0.1:8080
export PILOTSERVER_JWT_SECRET=test-secret-at-least-32-bytes-long!!
export PILOTSERVER_ADMIN_PASSWORD=admin
go build -o bin/pilotserver ./cmd/pilotserver
./bin/pilotserver
# 浏览器打开 http://127.0.0.1:8080/admin/
```

- [ ] **Step 3: Commit**

```bash
git commit -am "feat: add minimal admin web UI"
```

---

### Task 12: Nginx 示例 + Fork URL 说明 + Cabana 占位

**Files:**
- Create: `deploy/nginx.example.conf`
- Create: `docs/dragonpilot-fork-urls.md`
- Create: `web/cabana/README.md`（说明后续拷贝开源 Cabana 静态资源；第一期可不集成完整回放）

**nginx.example.conf 必须包含：**
- `proxy_pass http://127.0.0.1:8080`
- WebSocket headers for `/ws/`
- `client_max_body_size 1024m`
- `proxy_read_timeout` 加长
- `stream` 或额外 `server` 将 `41000-41099` 转发到本机（SSH 隧道）

**fork 文档必须列出：**
- `API_HOST` / `ATHENA_HOST` / upload / OTA 建议修改点
- 强调：不要安装额外二进制

- [ ] **Step 1: 写入上述文件**
- [ ] **Step 2: 全量测试 + 编译**

```bash
go test ./...
go build -o bin/pilotserver ./cmd/pilotserver
```

- [ ] **Step 3: Commit**

```bash
git commit -am "docs: add nginx example and dragonpilot URL patch guide"
```

---

## Spec Coverage Checklist

| 规格要求 | Task |
|----------|------|
| 配对 + 管理端可见 | 4, 7, 11 |
| Athena 在线 | 5 |
| 外网 SSH 隧道 | 6, 12（Nginx 端口） |
| 上传 + 列表下载 | 8 |
| OTA | 9 |
| 无设备额外二进制 | 12 文档 |
| billing/maps 桩 | 10 |
| 复用已有 Nginx、Go 内网 | 1, 12 |
| Cabana 复用 | 12 占位（完整回放可二期） |

## 后续计划（本文件不展开）

- **Plan 2：** 完整 Cabana 粘合、行程播放、上传保留策略  
- **Plan 3：** 对照具体 DragonPilot 版本做契约测试抓包对齐、差分 OTA  

---

## Self-Review Notes

- 已覆盖规格第一期成功标准；Cabana 完整回放显式放到后续，避免阻塞 SSH/上传/OTA。  
- SSH 桥接以 `athenad.startLocalProxy` + `/ws/proxy/{ticket}` 为准，实现时必须打开 fork 源码核对参数形状。  
- 无 TBD 占位；路径差异通过 `internal/api/paths.go` 集中调整。

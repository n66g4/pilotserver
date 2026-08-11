# 自建 OpenPilot Server（DragonPilot）设计规格

**日期：** 2026-08-11  
**状态：** 待用户审阅  
**仓库：** `pilotserver`

## 1. 背景与目标

为个人 fork 的 DragonPilot 自建一套仿官方的云端服务，部署在私有云上，经已有 DDNS 与 Nginx 对外提供。设备端**不增加任何额外可执行文件**，仅通过 fork 内默认上游地址（及可选 `launch_env.sh` 环境变量）指向自建服务。

### 1.1 成功标准（第一期）

1. 刷入 fork 的设备可完成配对，并出现在管理端。
2. Athena 长连接在线；管理端可显示在线状态。
3. **外网可通过 Athena `startLocalProxy` 隧道 SSH 到设备**（不依赖 frp 等旁路二进制）。
4. 行程日志/视频可上传，网页可列出与下载。
5. 设备可从自建 OTA 源检查版本并完成更新。
6. 设备上无自建额外二进制；仅 URL/域名指向本服务。
7. 地图与 billing 桩响应不阻断主流程。

### 1.2 非目标（第一期）

- 完整 Mapbox/高德导航实现（可后补）。
- 多租户、公开注册、计费。
- 进程内 TLS 或内嵌反向代理（使用现有 Nginx）。
- 兼容官方 Comma Connect 手机 App（以自建 Web 管理端为准）。

## 2. 约束与决策摘要

| 项 | 决策 |
|----|------|
| 功能范围 | 接近官方全栈；地图第一期桩掉 |
| 服务端策略 | 混合：核心协议自研，管理端/Cabana 等复用开源 |
| 规模 | 个人 1–5 台设备 |
| OTA | 第一期提供完整更新源（检查 + 产物下载） |
| 核心技术栈 | Go 单体 |
| 设备侧 | 零额外可执行文件；改 fork 默认 `API_HOST` / `ATHENA_HOST` 等 |
| 入口 | 复用私有云已有 Nginx（TLS + 反代）；Go 只监听内网 |
| SSH | 第一期必须支持，经 Athena 隧道 |

## 3. 总体架构

```text
[DragonPilot 设备]                         [私有云]
  athenad ──wss──┐
  Api/上传 ─https─┼──► 已有 Nginx (443/TLS, DDNS)
  updater ─https─┘         │
                           ├─ /ws/     → 127.0.0.1:8080  (WebSocket Upgrade)
                           ├─ /v1* …   → 127.0.0.1:8080
                           ├─ 上传路径 → 127.0.0.1:8080  (大 body / 长超时)
                           ├─ /ota/    → Go 或静态目录
                           ├─ /admin/  → 静态 + admin API
                           └─ /cabana/ → 静态（复用开源）

                           Go pilotserver (仅 127.0.0.1:8080)
                           ├─ auth / api / athena / upload / ota
                           ├─ billing+maps 桩
                           ├─ adminapi
                           └─ store: SQLite + 本地文件目录
```

### 3.1 设备侧改法

在 DragonPilot fork 源码中把默认上游改为自建域名（示例 `https://op.example.com` / `wss://op.example.com`）：

- `API_HOST`
- `ATHENA_HOST`
- 上传相关 URL（与 fork 中 uploader 实际读取处对齐）
- OTA / release 检查与下载入口
- `MAPS_HOST`：第一期可指向本域桩接口，或保持兼容空响应

可选：在设备 `/data/launch_env.sh` 用 `export` 临时覆盖，便于调试（仍不是新可执行文件）。

**禁止：** 在设备上放置 frp/`fish_arm64` 等二进制，或改 `process_config` 拉旁路进程。

### 3.2 SSH 隧道（外网连设备）

与官方相同路径，不增加设备端程序：

1. 设备 `athenad` 出网连接 `wss://<DDNS>/ws/v2/{dongle_id}`。
2. 管理员在管理端发起 SSH → 服务端经 Athena 下发 `startLocalProxy`。
3. 设备将本机 `127.0.0.1:22`（白名单端口）桥接到隧道。
4. 用户外网 SSH 到**私有云上的代理入口**，而非设备公网 IP。

**第一期 SSH 接入方式（拍板）：** 管理端一键开启隧道后，pilotserver 在内网临时监听一个短时 TCP 端口；Nginx 用 `stream` 或受控 `proxy_pass` 把该端口映射到公网（或仅 VPN/管理网）。管理端展示标准 `ssh -p <port> comma@<DDNS>`。不采用需自定义 `ProxyCommand` 客户端插件的方案，降低使用成本。

安全要求：仅管理员可开隧道；端口短时有效（到期关闭）；记录审计日志。

## 4. 模块划分（Go 单体）

| 包/模块 | 职责 |
|---------|------|
| `cmd/pilotserver` | 进程入口、配置加载、路由挂载 |
| `internal/auth` | 设备 RSA/JWT、管理员会话 |
| `internal/api` | 配对、设备、路线元数据等 HTTP API |
| `internal/athena` | WebSocket JSON-RPC；含 `echo`、上传队列相关、`startLocalProxy` |
| `internal/upload` | 签名 URL 校验后直传落盘（第一期不走第三方对象存储） |
| `internal/ota` | 版本检查、产物 URL/索引 |
| `internal/billing` | 桩：报告有效订阅，避免挡主流程 |
| `internal/maps` | 桩：兼容空/最小响应 |
| `internal/adminapi` | 管理端 API：设备、在线、行程、SSH/远程指令 |
| `internal/store` | SQLite + 文件系统布局 |

协议路径与字段以 **DragonPilot fork 实际请求** 为真相源（`common/api`、`athenad`、uploader、updater），用契约测试锁定，而非死记某一版官方文档。

### 4.1 主要数据流

1. **配对：** 设备公钥注册 → 写入已配对设备 → 签发 dongle JWT。  
2. **在线：** Athena 长连接；心跳/LastAthenaPing 类状态供管理端展示。  
3. **SSH：** 管理端 → adminapi → athena `startLocalProxy` → 用户经代理 SSH。  
4. **上传：** 设备凭短时签名 URL PUT/POST 到本域 → 文件按 `dongle/route/segment` 落盘 → DB 记元数据。  
5. **OTA：** 设备查询分支最新版本 → 返回 version + 下载 URL（可由 Nginx 静态托管产物）。  
6. **管理浏览：** 浏览器 → Nginx → admin 静态页 + adminapi；Cabana 读已上传数据。

## 5. 部署

### 5.1 Nginx（已有，本服务不内嵌）

- 终结 TLS（DDNS 证书）。
- 反代到 `127.0.0.1:8080`。
- WebSocket：`Upgrade` / `Connection`，超时加长。
- 上传：增大 `client_max_body_size` 与读写超时。
- OTA 产物可 `alias`/`root` 直接静态，或回源 Go。
- 仓库提供 `deploy/nginx.example.conf` 供粘贴进现有配置，**不**在 Go 内集成 Caddy/Nginx。

### 5.2 进程与数据

- systemd 或 Docker 运行 Go；只绑回环地址。
- 持久化：SQLite 文件、行程目录、OTA 产物目录。
- 定期备份上述三类数据。

## 6. 安全

- 设备：RSA 密钥对 + 短时 JWT（仿官方设备鉴权模型）。
- 管理端：单管理员（密码哈希）+ Session/JWT；建议限制来源 IP 或额外保护。
- 上传：带过期签名的 URL，防未授权灌盘。
- Athena 远程能力（含 SSH）默认仅管理员可触发。
- Go 不对公网直连；仅 Nginx 对外。

## 7. 存储布局（建议）

```text
data/
  pilotserver.db          # SQLite
  uploads/{dongle}/{route}/{segment}/...
  ota/{branch|channel}/...
```

个人 1–5 台默认 SQLite；若日后扩大规模再迁 Postgres，第一期不预做多租户抽象。

## 8. 仓库结构（落地时）

```text
cmd/pilotserver/
internal/{api,athena,upload,ota,auth,store,adminapi,billing,maps}/
web/admin/                 # 极简管理端
web/cabana/                # 复用开源静态资源（submodule 或拷贝）
deploy/nginx.example.conf
docs/                      # 操作与协议对照
```

## 9. 开源复用边界

- **自研：** auth、api、athena（含 SSH 隧道编排）、upload、ota、store、adminapi。  
- **复用/适配：** Cabana（行程回放）、管理端 UI 可参考 retropilot/rtz 的交互，但不强制运行其整套 Node 后端。  
- **地图：** 第一期仅桩；后续可接高德等并在 fork 中改导航后端。

## 10. 风险与缓解

| 风险 | 缓解 |
|------|------|
| fork 与官方 API 路径有漂移 | 以本机抓包/日志 + 契约测试对齐 fork |
| DDNS 证书/WebSocket 不稳 | Nginx 示例配置；监控 Athena 重连 |
| 上传占满磁盘 | 配额、保留策略、磁盘告警（个人规模先做简单清理策略） |
| SSH 隧道被滥用 | 仅管理员、短时、审计 |

## 11. 实现分期（规格内建议顺序）

1. 骨架 + store + auth + 配对 API  
2. Athena 长连接 + 在线状态  
3. SSH `startLocalProxy` 端到端  
4. 上传 + 行程列表/下载  
5. OTA 检查与产物  
6. billing/maps 桩  
7. 管理端 UI + Cabana 粘合  
8. Nginx 示例与文档；DragonPilot fork 默认 URL 补丁说明  

---

**审阅通过后**再编写详细实现计划（`writing-plans`）并开始编码。

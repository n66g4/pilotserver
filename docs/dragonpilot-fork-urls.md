# DragonPilot Fork 默认 URL 修改指南

设备端**不要安装任何额外可执行文件**（禁止 frp、`fish_arm64`、旁路代理等）。仅修改 fork 源码中的默认上游地址，或通过 `/data/launch_env.sh` 用 `export` 临时覆盖（仍是环境变量，不是新二进制）。

示例自建域名为 `https://op.example.com` / `wss://op.example.com`，请替换为你的 DDNS 域名。

## 必改项

| 变量 / 入口 | 建议值 | 用途 |
|-------------|--------|------|
| `API_HOST` | `https://op.example.com` | 配对、设备信息、路线列表、上传签名 URL |
| `ATHENA_HOST` | `wss://op.example.com` | Athena 长连接（`/ws/v2/{dongle_id}`） |

在 fork 源码中搜索上述常量（常见位置：`common/api.py`、`system/athenad/athenad.py` 或等价模块），将默认值改为自建域名。

## 上传

pilotserver 路由：

- `GET /v1.4/{dongleID}/upload_url/?path=...` — DragonPilot/openpilot uploader 兼容入口
- `POST /v1.1/devices/{dongleID}/upload_url/` — 获取短时签名 URL
- `PUT /upload/put/{token}` — 设备直传落盘

fork 侧需保证 uploader 请求的 API 基址与 `API_HOST` 一致；服务端 `PILOTSERVER_PUBLIC_BASE_URL` 须与 Nginx 对外 HTTPS 域名一致（签名 URL 的 host 由此生成）。

在 fork 中搜索 uploader / `upload_url` 相关读取处，确认无硬编码官方域名。

## OTA

pilotserver 路由：

- `GET /ota/{channel}/version` — 版本元数据（`version.json`）
- `GET /ota/files/...` — 产物下载

fork 侧 updater / release 检查入口应指向 `https://op.example.com/ota/...`。`version.json` 内 `download_url` 建议使用同一公网域名。详见 [ota.md](ota.md)。

大文件可选由 Nginx 静态 `alias` 托管 `{DataDir}/ota/files/`，元数据仍走 Go。

### fork 必须修改 updater 或 Git remote

phase-1 OTA HTTP 只提供 JSON 元数据和静态产物，不实现 Git 协议。若 fork 的 updater
仍通过 Git 更新，应把 remote 指向自行托管的 Git 仓库；若要使用上述 HTTP 接口，则
必须在 fork 中修改 updater 的检查和下载逻辑。未做任一项时，只能人工下载产物。

## 可选桩接口（第一期）

| 变量 | 说明 |
|------|------|
| `MAPS_HOST` | 指向本域 `/v1/maps/` 或 `/maps/` 桩；或保持兼容空响应 |

billing / prime 相关请求已由 pilotserver 桩返回有效订阅，一般无需改 fork，除非 fork 硬编码了不可达域名。

## SSH 隧道（无设备端额外程序）

外网 SSH 经 Nginx `stream` 转发 pilotserver 临时端口（41000–41099），设备仅通过已有 `athenad` 响应 `startLocalProxy`。管理端发起隧道后使用返回的 `ssh -p <port> comma@<host>`，**不要**在设备上部署 SSH 跳板二进制。

## 路径差异

HTTP 路径常量集中在 [`internal/api/paths.go`](../internal/api/paths.go)。若 fork 与默认路径不一致，优先改 pilotserver 侧常量，而非在设备上加代理。

## 调试

可在设备 `/data/launch_env.sh` 中：

```bash
export API_HOST="https://op.example.com"
export ATHENA_HOST="wss://op.example.com"
```

重启相关服务后验证配对、Athena 在线、上传与 OTA，再固化为 fork 默认值。

# DragonPilot Fork 默认 URL 修改指南

设备端**不要安装任何额外可执行文件**（禁止 frp、`fish_arm64`、旁路代理等）。仅修改 fork 源码中的默认上游地址，或通过 `/data/launch_env.sh` 用 `export` 临时覆盖（仍是环境变量，不是新二进制）。

示例自建域名为 `https://op.example.com` / `wss://op.example.com`，请替换为你的 DDNS 域名。

## 必改项

| 变量 / 入口 | 建议值 | 用途 |
|-------------|--------|------|
| `API_HOST` | `https://op.example.com` | 配对、设备信息、路线列表、上传签名 URL |
| `ATHENA_HOST` | `wss://op.example.com` | Athena 长连接（`/ws/v2/{dongle_id}`） |

在 fork 源码中搜索上述常量（常见位置：`common/api.py`、`system/athenad/athenad.py` 或等价模块），将默认值改为自建域名。

## 设备配对

`POST /v2/pilotauth/` 的 `register_token` 是设备私钥签名的注册 JWT，不是服务器静态密码。pilotserver 使用同一请求中的 `public_key` 校验 RS256/ES256 签名，并要求 JWT payload 包含 `"register": true`；这与 openpilot/DragonPilot 的原生注册流程兼容。

`PILOTSERVER_PAIRING_TOKEN` 是可选的独立二次校验：

- 未设置时，签名有效的设备可首次配对，服务端会记录 pairing gate 未启用的警告；适合受控网络内的个人设备。
- 设置时，设备除 `register_token` 外还必须在 JSON body 的 `pair_code` 或请求头 `X-Pairing-Password` 中提交相同值。
- 不要把 `PILOTSERVER_PAIRING_TOKEN` 塨入 `register_token`；两者用途不同。

## 上传

pilotserver 路由：

- `GET /v1.4/{dongleID}/upload_url/?path=...` — DragonPilot/openpilot uploader 兼容入口
- `POST /v1.1/devices/{dongleID}/upload_url/` — 获取短时签名 URL
- `PUT /upload/put/{token}` — 设备直传落盘

fork 侧需保证 uploader 请求的 API 基址与 `API_HOST` 一致；服务端 `PILOTSERVER_PUBLIC_BASE_URL` 须与 Nginx 对外 HTTPS 域名一致（签名 URL 的 host 由此生成）。

在 fork 中搜索 uploader / `upload_url` 相关读取处，确认无硬编码官方域名。

## 软件更新（Git updater）

车上用 Git updater，把 fork 的 Git remote 指到你能访问的仓库即可。pilotserver **不**提供 Git 协议，也 **不**要求设备走 `/ota/`。

遗留的 HTTP 接口见 [ota.md](ota.md)，第一期不用。

## 可选桩接口（第一期）

| 变量 | 说明 |
|------|------|
| `MAPS_HOST` | 指向本域 `/v1/maps/` 或 `/maps/` 桩即可；空 JSON，不做导航算路（openpilot 已删除 NOO） |

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

重启相关服务后验证配对、Athena 在线与上传，再固化为 fork 默认值。

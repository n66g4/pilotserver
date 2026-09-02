# PilotServer

自建 OpenPilot / DragonPilot 云端服务：设备配对、Athena 在线、外网 SSH 隧道、行程上传、管理后台。  
设备端**不安装额外二进制**，只改 fork 默认 `API_HOST` / `ATHENA_HOST`（或 `launch_env.sh`）。车上软件更新走 Git updater，不经本服务。

适合个人 1～5 台设备；服务默认只监听 `127.0.0.1:18780`，前面用 Nginx / 群晖反向代理做 HTTPS。

## 功能一览

| 能力 | 说明 |
|------|------|
| 配对 / 鉴权 | 设备 RSA 注册 JWT；管理端密码登录 |
| Athena | WebSocket 长连接、在线状态 |
| SSH | 经 `startLocalProxy` 开短时端口（41000–41099），无需 frp |
| 上传 | 兼容 `GET /v1.4/{dongle}/upload_url/`，落盘到本地目录 |
| 软件更新 | 车上 Git updater（改 Git remote）；本服务不提供设备自动 OTA |
| 桩接口 | billing / maps，避免挡主流程 |
| 管理端 | `/admin/` 统一概览 / 设备 / 行程 / 设置控制台，支持开 SSH、下载与在线回放 |

## 行程在线回放

管理端可在线连续播放一条行程中的 `qcamera.ts`，也可切换到单个 segment。回放页采用视频优先布局并适配桌面和手机；Chrome、Edge、Android Chrome 使用随程序内嵌的本地 hls.js，macOS/iPhone Safari 使用原生 HLS。

`qlog.zst` 会解析车速、控制状态、告警、GPS 轨迹，并显示与视频同步的速度曲线。首次打开某个 segment 需要解析，缓存写入 `{DataDir}/replay-cache/{dongleID}/{route}/{segment}.v2.json`；qlog 大小、修改时间、解析器版本或 schema 版本变化时会自动失效重建。

当前 cereal schema 锁定到 DragonPilot `pre-build` 分支、commit `21d40d72c65021c81e84a62e23d700972c7c8a7f`。完整 `rlog` / CAN / DBC 分析不集成进本服务；需要时用本机 Qt Cabana 打开已下载行程。

地图可在 `/admin/` 的服务配置中选择 `none`、高德（AMap）或腾讯。`none` 或未配置 Key 时仍会显示内置无底图轨迹；地图 SDK 加载失败也会自动降级。Web Key 和高德 security code 会发送到浏览器，必须在服务商控制台设置允许域名限制。

建议使用当前版本的 Chrome、Edge、Android Chrome、macOS Safari 或 iPhone Safari。浏览器需支持 MSE/hls.js 或原生 HLS；不支持时页面会明确提示，并保留原文件下载。

## 快速开始（本机 / 私有云二进制）

要求：Go 1.22+（依赖可能拉到更新的 toolchain）。

```bash
git clone <本仓库>
cd pilotserver

export PILOTSERVER_PUBLIC_BASE_URL="https://你的域名"   # 可选；也可启动后在 /admin/ 配置
export PILOTSERVER_JWT_SECRET="$(openssl rand -hex 32)" # ≥32 字节
export PILOTSERVER_ADMIN_PASSWORD="你的强密码"           # ≥8 位
# 可选：export PILOTSERVER_PAIRING_TOKEN="二次配对口令"

go test ./...
go build -o bin/pilotserver ./cmd/pilotserver
./bin/pilotserver
```

自检：`curl -s http://127.0.0.1:18780/healthz` → `ok`  
管理页：`http://127.0.0.1:18780/admin/`

更多环境变量见 [deploy/README.md](deploy/README.md)。

## 群晖 DSM 7.2（x86_64 SPK）

```bash
./synology/build-spk.sh
# 默认产物：dist/pilotserver-1.0.23-1-x64.spk
```

`1.0.23-1` 提供透明 DSM 桌面图标（Tesla 蓝方向盘，无自带底座）。同一版本重装往往不刷新图标，请装此新版本或先卸载再装。`1.0.21-1` 起提供统一的概览 / 设备 / 行程 / 设置控制台，可在简体中文与英文间切换并记住语言选择，并支持在设置中导入已添加到 GitHub 的 SSH 私钥。此次升级不涉及 API 或数据库迁移；原有数据和设备端配置可继续使用。

车上填写 GitHub 用户名并打开 SSH。在管理后台「设置」粘贴已添加到 GitHub 的 OpenSSH 私钥并保存（私钥只留在 NAS）。设备在线后点「开终端」。原有可选的复制 SSH 命令方式仍需放行 `41000–41099` 端口，本机终端使用同一把私钥。
升级后若显示的指纹不是你的 GitHub 私钥指纹，请先清除，再重新粘贴保存。

3. 向导填写管理密码（可选：公网 HTTPS 地址、配对口令；DDNS 仍用 DSM 自带）  
4. 若未填公网地址：启动后打开 `/admin/` →「公网访问地址」保存  
5. DSM 反向代理指到 `127.0.0.1:18780`，并为 `/ws/` 打开 WebSocket  
6. 数据目录（SPK）：`/var/packages/pilotserver/var/data/`

详细步骤：[docs/synology-dsm72-spk.md](docs/synology-dsm72-spk.md)

## DragonPilot / 设备侧

只改上游地址，例如：

```bash
export API_HOST="https://你的域名"
export ATHENA_HOST="wss://你的域名"
```

说明与路径对照：[docs/dragonpilot-fork-urls.md](docs/dragonpilot-fork-urls.md)

## 反向代理

- 通用 Nginx 示例：[deploy/nginx.example.conf](deploy/nginx.example.conf)（含 WebSocket、大上传、SSH 端口 stream）  
- 群晖：控制面板 → 登录门户 → 反向代理  

## 目录结构

```text
cmd/pilotserver/     # 入口
internal/            # api / athena / upload / ota / auth / adminapi …
web/admin/           # 管理页（embed）
synology/            # DSM SPK 打包
deploy/              # 部署与 Nginx 示例
docs/                # 设备 fork、OTA、群晖说明
dist/                # 构建出的 .spk（本地生成，默认不入库）
```

## 开发

```bash
go test ./...
go build -o bin/pilotserver ./cmd/pilotserver
```

设计与实现计划（可选阅读）：

- [docs/superpowers/specs/2026-08-11-self-hosted-openpilot-server-design.md](docs/superpowers/specs/2026-08-11-self-hosted-openpilot-server-design.md)
- [docs/superpowers/plans/2026-08-11-pilotserver-phase1.md](docs/superpowers/plans/2026-08-11-pilotserver-phase1.md)

## 许可与声明

个人自建、仿官方协议兼容实现；与 comma.ai 无官方关系。请遵守当地法律与车辆安全相关规定，仅在合法授权的设备与数据上使用。运行服务后可通过 `/admin/licenses/dragonpilot.txt` 和 `/admin/licenses/openpilot.txt` 查看回放 cereal schema 的原始许可证声明。

# 群晖 DSM 7.2 安装 PilotServer（x86_64 SPK）

适用于官方群晖与黑群晖（x86_64）。套件为第三方未签名包，需手动安装。

## 构建 SPK（在开发机）

```bash
cd /path/to/pilotserver
./synology/build-spk.sh
# 产物：dist/pilotserver-*-x64.spk
```

默认构建版本为 `1.0.19-1`；如需自定义可使用 `PILOTSERVER_SPK_VERSION=版本 ./synology/build-spk.sh`。

## DSM 安装与升级

1. 套件中心 → 设置 → 勾选 **信任未知发布者**（黑群晖必备）
2. 手动安装 → 选中 `dist/pilotserver-1.0.19-1-x64.spk`（不要用旧的 `*-x86_64.spk`）
3. 向导填写 **管理密码**；可选填 **公网 HTTPS 访问地址**（不是配 DDNS）与配对二次口令
4. **DDNS** 仍用 DSM「外部访问 → DDNS」

从旧版本升级时也在套件中心选择“手动安装”，上传 `pilotserver-1.0.19-1-x64.spk` 并按提示升级；升级前仍建议备份 `/var/packages/pilotserver/var/data/`。`1.0.19-1` 提供统一的概览 / 设备 / 行程 / 设置控制台，支持简体中文与英文切换并持久保存语言选择。此次升级不涉及 API 或数据库迁移，原有数据与设备端配置可继续使用。安装或升级完成后确认套件状态为“运行中”，再打开 `/admin/`。

若安装失败，SSH 查看：

```bash
sudo tail -100 /var/log/synopkg.log
sudo cat /var/packages/pilotserver/var/install.log
```

把其中 `pilotserver` 相关行发出来便于继续排查。

## 安装后

服务默认只监听 `127.0.0.1:18780`，**局域网 IP 直连会被拒绝**；请先配好下面的 DSM 反向代理，再经 `https://…/admin/` 访问。

若要临时用局域网 IP 直连（仅内网建议），SSH 编辑 `/var/packages/pilotserver/var/data/pilotserver.env`：

```
PILOTSERVER_LISTEN=0.0.0.0:18780
PILOTSERVER_ALLOW_NON_LOOPBACK=1
```

然后在套件中心重启 PilotServer，即可访问 `http://群晖IP:18780/admin/`。

若向导未填公网地址：

1. 启动套件
2. 经反代打开 `/admin/` 登录
3. 在「公网访问地址」中填写 `https://你的DDNS域名` 并保存

也可编辑 `/var/packages/pilotserver/var/data/pilotserver.env` 中的 `PILOTSERVER_PUBLIC_BASE_URL`（管理页保存优先写入数据库）。

日志：`/var/packages/pilotserver/var/pilotserver.log`。

## 行程在线回放

1. 经反向代理打开 `/admin/` 并登录。
2. 在设备列表中打开某台设备的行程，再进入行程文件列表。
3. 点击回放入口，可播放整条行程或切换到单个视频分段。

回放使用 `qcamera.ts` 播放视频，并从 `qlog.zst` 显示同步的车速、控制状态、告警、GPS 轨迹和速度曲线。首次打开某个分段时需要解析 qlog，耗时取决于日志大小和 NAS 性能；后续会读取缓存。缓存位于套件数据目录下：

```text
/var/packages/pilotserver/var/data/replay-cache/{dongleID}/{route}/{segment}.v2.json
```

qlog 文件变化后缓存会自动失效并重建。

### 地图配置

在 `/admin/` 的“服务配置”中选择：

- `none`：不需要 Key，显示内置无底图轨迹；
- 高德地图：填写 Web Key，按服务商要求填写 security code；
- 腾讯地图：填写 Web Key。

地图 Web Key 必然会发送到浏览器，请在高德或腾讯控制台限制允许访问的域名。未配置 Key 或地图 SDK 加载失败时，视频和遥测仍可使用，并自动回退到无底图轨迹。

### 回放故障排查

- **缺少视频或无法播放**：确认行程分段中存在完整的 `qcamera.ts`，并检查浏览器网络面板中的 HLS 清单和 `.ts` 请求；不完整或仍在上传的文件不会进入播放列表。
- **遥测错误**：确认同一分段存在完整的 `qlog.zst`；查看 `pilotserver.log` 中对应 segment 的解析错误。单段遥测失败不会阻止其他视频分段播放。
- **地图 SDK 失败**：确认 provider、Web Key、允许域名和高德 security code 配置正确；页面应自动显示无底图轨迹。
- **反向代理媒体路径失败**：反向代理必须把 `/admin/`、`/admin/api/` 和 `/media/` 原样转发到 `http://127.0.0.1:18780`，不要重写 HLS 清单中的媒体路径。

套件运行时仍只有一个 Go 可执行文件，不依赖 Node、Python、FFmpeg、Cap'n Proto 编译器或代码生成器。

## DSM 反向代理

控制面板 → 登录门户 → 高级 → 反向代理：

| 源 | 目的 |
|----|------|
| HTTPS + 你的 DDNS 主机名 + `/` | `http://127.0.0.1:18780` |

并为 WebSocket 增加或勾选对 `/ws/` 的 Upgrade 支持（不同 DSM 主题界面文案略有差异）。  
证书可用 DSM 证书或已有 Let’s Encrypt。

## SSH 隧道端口

管理端「开 SSH」会使用本机 `41000–41099`。DSM 反向代理只管 HTTP(S)，请在防火墙放行该范围，必要时用额外 TCP 转发。详见仓库 `deploy/nginx.example.conf`（若你在 DSM 外另跑 Nginx）。

## 数据与卸载

- 数据目录：`/var/packages/pilotserver/var/data/`（行程、OTA、env）
- 卸载套件会保留 `var` 下数据与否取决于 DSM；重要行程请自行备份

## 管理入口

- 内网/外网：`https://你的DDNS/admin/`（经 DSM 反向代理）
- 局域网 IP 直连需按上文放开 `PILOTSERVER_ALLOW_NON_LOOPBACK`

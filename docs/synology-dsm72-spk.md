# 群晖 DSM 7.2 安装 PilotServer（x86_64 SPK）

适用于官方群晖与黑群晖（x86_64）。套件为第三方未签名包，需手动安装。

## 构建 SPK（在开发机）

```bash
cd /path/to/pilotserver   # 本仓库 feature 分支 / worktree
./synology/build-spk.sh
# 产物：dist/pilotserver-1.0.0-1-x86_64.spk
```

可选：`PILOTSERVER_SPK_VERSION=1.0.1-1 ./synology/build-spk.sh`

## DSM 安装

1. 套件中心 → 设置 → 勾选 **信任未知发布者**（黑群晖必备）
2. 手动安装 → 选中 `.spk`
3. 向导只填 **管理密码**（可选填配对二次口令）
4. **不要**在套件里配 DDNS（用 DSM「外部访问 → DDNS」即可）

## 安装后必做（一次）

编辑：

`/volume1/pilotserver/pilotserver.env`

设置与反向代理一致的公网 HTTPS 根地址，例如：

```bash
PILOTSERVER_PUBLIC_BASE_URL=https://your-ddns.example.com
```

其余项安装时已自动生成。然后在套件中心 **启动** PilotServer。

未填写 `PILOTSERVER_PUBLIC_BASE_URL` 时服务会拒绝启动（日志在 `/var/packages/pilotserver/var/pilotserver.log`）。

## DSM 反向代理

控制面板 → 登录门户 → 高级 → 反向代理：

| 源 | 目的 |
|----|------|
| HTTPS + 你的 DDNS 主机名 + `/` | `http://127.0.0.1:8080` |

并为 WebSocket 增加或勾选对 `/ws/` 的 Upgrade 支持（不同 DSM 主题界面文案略有差异）。  
证书可用 DSM 证书或已有 Let’s Encrypt。

## SSH 隧道端口

管理端「开 SSH」会使用本机 `41000–41099`。DSM 反向代理只管 HTTP(S)，请在防火墙放行该范围，必要时用额外 TCP 转发。详见仓库 `deploy/nginx.example.conf`（若你在 DSM 外另跑 Nginx）。

## 数据与卸载

- 数据目录：`/volume1/pilotserver/`（行程、OTA、env）
- 卸载套件 **默认保留** 该目录，避免误删行程

## 管理入口

- 本机：`http://群晖局域网IP:8080/admin/`
- 外网：`https://你的DDNS/admin/`

# PilotServer 管理页网页 SSH 终端设计

## 目标

管理员在 `/admin/` 设备页对**在线** comma 设备打开网页终端，经现有 Athena `startLocalProxy` 隧道远程登录车上 `comma` 用户。私钥只存在 NAS，浏览器不持有 SSH 钥匙。

不修改设备端程序、不修改现有设备 HTTP/Athena 协议、不引入 npm 或前端框架。

## 非目标

- 浏览器或本机粘贴私钥
- 用管理密码代替 SSH 认证
- 多用户、审计录像、文件互传、端口转发
- 替换现有「复制 ssh 命令」能力（终端顶栏仍提供该命令作备用）
- 在设备上安装任何额外二进制

## 使用流程

1. 设置页展示 NAS 生成的 SSH 公钥（Ed25519）。私钥文件不出 NAS。
2. 管理员把公钥加到车上 `comma` 用户的授权钥匙中，并在设备设置里打开 SSH。
3. 设备 Athena 在线时，设备行点「开终端」。
4. 服务端沿用现有隧道（`127.0.0.1:41000–41099`，约 10 分钟），再用服务端 SSH 客户端以该私钥连接 `comma@127.0.0.1:<临时端口>`。
5. 管理页在主工作区打开终端（保留左侧/底部导航）。顶栏可复制原来的 `ssh -p <port> comma@<公网主机>`。
6. 关闭终端或隧道到期后会话结束；再次点击可重开。

## 架构

```text
浏览器 xterm  ←admin JWT WebSocket→  pilotserver
                                      ├─ OpenSSHTunnel (现有 Athena startLocalProxy)
                                      └─ golang.org/x/crypto/ssh → 127.0.0.1:临时端口
                                                                      ↓
                                                              车上 sshd:22 (comma)
```

Athena 已认证设备身份。本机到 `127.0.0.1` 的 SSH 宿主密钥校验采用「忽略远程 host key」：TCP 只绑回环，对端就是刚经 Athena 打开的那台设备。不把 host key 策略暴露给浏览器。

## 组件

### 钥匙

- 路径：`$PILOTSERVER_DATA_DIR/ssh/id_ed25519` 与 `id_ed25519.pub`
- 权限：私钥 `0600`，公钥 `0644`
- 首次读取公钥时若不存在则生成；设置页可「重新生成」（旧私钥立即失效，车上需改授权钥匙）
- GET 设置或专用接口只返回公钥字符串，绝不返回私钥

### 管理 API

保持现有：

- `POST /admin/api/devices/{dongleID}/ssh` → `{host,port,expires_in}`（复制命令仍用它）

新增：

- `GET /admin/api/ssh-key` → `{public_key}`（缺失则生成）
- `POST /admin/api/ssh-key/rotate` → 重新生成后返回 `{public_key}`
- `GET /admin/api/devices/{dongleID}/ssh/pty`（WebSocket，需管理员 JWT）  
  服务端：校验在线 → 开隧道 → SSH 登录并申请 PTY → 把会话与 WebSocket 互转。

WebSocket 认证：`Authorization: Bearer` 或查询参数 `access_token`（浏览器无法给 WS 设请求头时用后者）。

会话协议：

1. 客户端连接后发送一条文本帧：`{"cols":80,"rows":24}`
2. 服务端成功后发送一条文本帧：`{"host":"...","port":41017,"expires_in":600}`
3. 之后双方只发二进制帧（PTY 字节）；客户端可再发文本帧 `{"cols":n,"rows":n}` 以调整窗口

失败时服务端发送文本帧 `{"error":"<i18n-stable-code>"}` 后关闭。稳定码包括 `offline`、`public_base_unconfigured`、`auth_failed`、`tunnel_failed`。前端映射为本地化文案，不展示原始 SSH 错误细节。

### 管理页

- 设置：公钥只读框、复制、重新生成（二次确认）
- 设备：在线可点「开终端」；离线禁用
- 工作区内终端视图：xterm 画布、状态、关闭、复制 ssh 命令
- 将 `xterm` 与许可证像 `hls.min.js` 一样放入 `web/admin/vendor/`，`admin.go` embed，不加包管理器
- 全部新文案走现有 `i18n.js`（`zh-CN` / `en`）

## 错误处理

| 情况 | 表现 |
|------|------|
| 设备离线 | 按钮禁用；若会话中掉线则关闭终端并提示 |
| 未配置公网地址 | 与现有开 SSH 相同，工作区提示 |
| 车上未收公钥 / SSH 未开 | 终端提示认证失败，并提醒检查车上授权钥匙 |
| 隧道或 PTY 超时 | 关闭会话并提示可重试 |
| 未登录 / JWT 失效 | WebSocket 拒绝，走现有 401 清会话 |

页面级错误写入现有 `#workspace-status`，设置页钥匙操作结果写在设置表单附近。

## 测试

- 钥匙：生成、轮换、私钥文件权限、JSON 不含私钥
- 离线 / 无公网地址：与现有 SSH 测试一致
- 假 SSH 服务：有效钥匙能建会话并回显；错误钥匙失败
- WebSocket：无 JWT 拒绝；有 JWT 且在线时完成一轮输入输出
- 管理页：含终端容器、vendor 脚本、i18n 键；开终端不把私钥写入 DOM

## 约束

- 不改设备 HTTP API、Athena JSON-RPC 形状、HLS、遥测
- 不新增运行时 npm 依赖
- 交互控件至少 40px
- 每次改生产代码后 `go build -o bin/pilotserver ./cmd/pilotserver`
- 未经用户要求不创建 git commit

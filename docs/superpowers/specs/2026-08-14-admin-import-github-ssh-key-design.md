# PilotServer 导入 GitHub SSH 私钥设计

本文修正 `2026-08-14-admin-web-ssh-design.md` 中的钥匙模型。网页终端、Athena 隧道、PTY WebSocket 协议保持不变。

## 目标

车上只填 GitHub 用户名，设备从 `https://github.com/<用户名>.keys` 拉取公钥。管理员把电脑上对应的 GitHub SSH **私钥**粘贴进 NAS 管理后台；「开终端」用这把私钥登录车上 `comma` 用户。

## 非目标

- NAS 自动生成或轮换钥匙
- 把 NAS 公钥拷到车上 `authorized_keys`
- 从 GitHub 网页拉取私钥（GitHub 只存公钥）
- 带口令的私钥
- 用管理密码加密落盘
- 多把钥匙、按设备选钥匙
- 改变隧道、xterm、复制 `ssh -p` 命令

## 使用流程

1. 车上设置 GitHub 用户名，并打开 SSH。
2. 管理后台「设置」粘贴 `~/.ssh/id_ed25519`（或已添加到 GitHub 的那把未加密 OpenSSH 私钥）并保存。
3. 设备 Athena 在线时点「开终端」：现有隧道 + 已保存私钥登录 `comma@127.0.0.1:<临时端口>`。
4. 本机终端备用命令仍用电脑上同一把私钥。

## 钥匙存储

- 路径：`$PILOTSERVER_DATA_DIR/ssh/id_ed25519`
- 权限：目录 `0700`，私钥 `0600`
- 不自动生成。文件不存在视为未配置。
- 不再使用、不再生成 `id_ed25519.pub`。导入或清除时若磁盘上还有旧 `.pub`（此前 NAS 自生成留下的），一并删除。
- 保存用户私钥时覆盖同路径已有文件。
- 指纹：对解析后的公钥做 `ssh.FingerprintSHA256`（`SHA256:…`）。

## 管理 API

均需管理员 JWT。任何响应都不得包含私钥正文。

- `GET /admin/api/ssh-key` → `{configured:false}` 或 `{configured:true,fingerprint:"SHA256:…"}`。文件存在但无法解析 → 500，提示清除后重新保存。
- `PUT /admin/api/ssh-key` body `{private_key:"-----BEGIN …"}`
  - 用 `ssh.ParsePrivateKey` 校验；失败或带口令 → 400
  - 成功后返回与 GET 相同的 configured/fingerprint JSON
- `DELETE /admin/api/ssh-key` → 删除私钥（及残留 `.pub`）；未配置也返回成功
- 删除 `POST /admin/api/ssh-key/rotate`

`GET /admin/api/devices/{dongleID}/ssh/pty` 不变。未配置私钥时在开隧道前发送 `{"error":"key_unconfigured"}` 并关闭。`auth_failed` 文案改为检查车上 GitHub 用户名、GitHub 是否有对应公钥、SSH 是否打开。

## 管理页

设置页 SSH 区：

- 私钥粘贴框（空；已配置后也不回填）
- 状态：未配置 / 已配置 + 指纹
- 「保存钥匙」「清除钥匙」（清除需二次确认）
- 去掉复制公钥、重新生成

设备页「开终端」、终端视图、xterm 不变。新文案走 `i18n.js`（`zh-CN` / `en`）。

## 错误处理

| 情况 | 稳定码 / 表现 |
|------|----------------|
| 未保存私钥 | `key_unconfigured` |
| 设备离线 | `offline` |
| 未配公网地址 | `public_base_unconfigured` |
| 钥匙不被车上接受 | `auth_failed` |
| 隧道或会话断开 | `tunnel_failed` |
| 非法或带口令私钥 | PUT 400，设置页提示 |

页面不展示 SSH 原始错误。设置页结果写在钥匙区域；会话错误写在终端状态。

## 测试

- PUT 合法私钥 → GET `configured` + 指纹；JSON 与 HTML 不含私钥正文
- 非法私钥、带口令私钥 → 400，磁盘不写入
- DELETE 后 GET `configured:false`；未配置开终端 → `key_unconfigured`
- 已保存钥匙的 PTY 回显测试仍通过
- i18n 含新键；不再断言公钥框 / rotate

## 约束

- 不改设备 HTTP API、Athena JSON-RPC、HLS、遥测
- 不新增运行时 npm 依赖
- 每次改生产代码后 `go build -o bin/pilotserver ./cmd/pilotserver`
- 未经用户要求不创建 git commit
- 不读取、不提交用户本机 `~/.ssh/` 私钥

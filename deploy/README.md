# 部署说明

群晖 DSM 7.2（x86_64）套件安装见 [docs/synology-dsm72-spk.md](../docs/synology-dsm72-spk.md)。

## 必需配置

- `PILOTSERVER_JWT_SECRET`：至少 32 字节，仅用于管理 JWT 和上传签名
- `PILOTSERVER_ADMIN_PASSWORD`：至少 8 字节，无默认值

公网地址（上传签名 / SSH 展示用）：

- `PILOTSERVER_PUBLIC_BASE_URL`：可选；安装向导可填，或启动后在 `/admin/` 设置
- 运行时优先使用数据库中的设置（管理页保存）；env 仅在库中为空时作为初始值

可选配置：

- `PILOTSERVER_PAIRING_TOKEN`：至少 8 字节；设置后作为独立二次校验，通过请求 body 的 `pair_code` 或 `X-Pairing-Password` 请求头提交。`register_token` 始终是设备私钥签名的注册 JWT。

`PILOTSERVER_LISTEN` 默认 `127.0.0.1:18780`。可在 `/admin/` 修改；保存后进程热切换监听端口，并写入 `pilotserver.env`。服务默认拒绝监听非回环地址；
只有明确设置 `PILOTSERVER_ALLOW_NON_LOOPBACK=1` 才允许，例如 `0.0.0.0:18780`。

SSH 隧道默认临时监听 `127.0.0.1:41000-41099`。部署时需允许 Nginx
`stream`（或同等 TCP 代理）访问并映射该端口范围；公网防火墙只应放行实际需要的入口。

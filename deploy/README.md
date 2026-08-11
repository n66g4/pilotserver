# 部署说明

## 必需配置

- `PILOTSERVER_PUBLIC_BASE_URL`：对外 HTTPS 根地址
- `PILOTSERVER_JWT_SECRET`：至少 32 字节，仅用于管理 JWT 和上传签名
- `PILOTSERVER_ADMIN_PASSWORD`：至少 8 字节，无默认值
- `PILOTSERVER_PAIRING_TOKEN`：至少 8 字节；设备配对请求的 `register_token` 必须完全匹配

`PILOTSERVER_LISTEN` 默认 `127.0.0.1:8080`。服务默认拒绝监听非回环地址；
只有明确设置 `PILOTSERVER_ALLOW_NON_LOOPBACK=1` 才允许，例如 `0.0.0.0:8080`。

SSH 隧道默认临时监听 `127.0.0.1:41000-41099`。部署时需允许 Nginx
`stream`（或同等 TCP 代理）访问并映射该端口范围；公网防火墙只应放行实际需要的入口。

# OTA 元数据与产物

运行时数据目录（默认 `./data`，由 `PILOTSERVER_DATA_DIR` 配置）下：

```
{DataDir}/ota/
  {channel}/version.json   # 渠道版本元数据
  files/                   # 发布产物（tarball 等）
```

`version.json` 示例：

```json
{
  "version": "0.9.1",
  "notes": "release notes",
  "download_url": "https://your-host/ota/files/release.tar.gz"
}
```

## 接口

- `GET /ota/{channel}/version` — 返回对应渠道的 `version.json`
- `GET /ota/files/...` — 静态文件服务（产物下载）

服务启动时会自动创建 `{DataDir}/ota/files/`。各渠道目录在人工写入 `version.json` 时自行创建。

## 部署方式

**Go 内置（默认）：** pilotserver 直接提供上述两个路由。

**Nginx 静态（可选）：** 大文件可由 Nginx 直接映射 `alias {DataDir}/ota/files/;`，版本元数据仍走 Go。

## 发布流程（第一期）

1. 将 release tarball 放入 `{DataDir}/ota/files/`
2. 在 `{DataDir}/ota/{channel}/version.json` 写入版本信息与 `download_url`
3. 设备通过 `GET /ota/{channel}/version` 检查更新并下载

不做 GitHub 自动同步；后续可扩展。

## 与 Git updater 的边界

DragonPilot/openpilot 的常见 updater 直接使用 Git remote 拉取提交。上述 phase-1
HTTP 接口不是 Git smart/dumb HTTP 服务，不能仅通过修改 `API_HOST` 替代 Git remote。

使用方式二选一：

1. fork 修改 updater，使其读取 `/ota/{channel}/version` 并下载发布产物；
2. 保持原 Git updater，将 fork 仓库托管到可访问的 Git 服务并修改其 remote。

未修改 updater 时，本 HTTP 接口只适合人工下载或托管构建产物，不代表设备会自动更新。

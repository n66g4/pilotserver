# HTTP OTA 接口（非第一期）

第一期设备软件更新走 **Git updater**（把 fork 的 Git remote 指到可达仓库）。本服务不承担设备自动 OTA。

仓库里仍保留 `GET /ota/{channel}/version` 与 `GET /ota/files/...`，仅作可选遗留：人工下包或以后若改 updater 再用。设备默认 **不会** 请求这些接口。

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

服务启动时会自动创建 `{DataDir}/ota/files/`。不要把这些路径配进车上 updater。

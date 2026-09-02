# Cabana（不集成）

pilotserver **不**集成 Cabana（不做 `/cabana/` 网页，也不在 SPK 内嵌 Qt）。

- 行程视频与 `qlog` 遥测：用管理端自研回放。
- CAN / DBC / 完整 `rlog`、新车型 port：在本机使用官方 [Qt Cabana](https://github.com/commaai/openpilot/tree/master/openpilot/tools/cabana)（或独立桌面发行版），读取已下载或 NAS 上的 `uploads/` 即可。

旧的 Web Cabana（[commaai/cabana](https://github.com/commaai/cabana)）已废弃，不要往本目录拷静态资源。

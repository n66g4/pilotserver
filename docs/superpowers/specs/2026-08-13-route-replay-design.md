# PilotServer 行程在线回放设计

## 1. 目标

在现有管理端行程浏览中分两步增加在线回放：

1. 在线播放已上传的 `qcamera.ts`，支持单个 60 秒分段和整条行程连续播放。
2. 原生解析 `qlog.zst`，展示速度、GPS 轨迹、openpilot 控制状态和告警，并与视频时间同步。

完整 `rlog.zst` 信号分析和 Cabana 集成不在本次范围内，后续复用本设计中的媒体鉴权、文件定位和遥测缓存能力。

## 2. 已确认约束

- 运行时保持单一 Go 可执行文件，不增加 Python、FFmpeg、Node 或容器。
- SPK 继续以 `CGO_ENABLED=0` 构建 linux/amd64 静态程序。
- 设备侧不增加任何可执行文件，也不改现有上传协议。
- 支持桌面 Chrome/Edge、Android Chrome、macOS/iPhone Safari。
- 同时支持分段播放和整条行程连续播放。
- 采用“视频优先”布局。
- qlog 首次访问时解析并缓存，后续读取缓存。
- 地图提供商可配置为高德或腾讯；没有 Key 时显示无底图轨迹。
- 现有原文件下载功能继续保留。

## 3. 文件事实

openpilot/dragonpilot 将一条行程切成约 60 秒一个 segment：

- `qcamera.ts`：526×330、H.264、MPEG-TS、约 256 kbps，可选音频；用于低带宽预览。
- `qlog.zst`：zstd 压缩的 Cap’n Proto Event 流，是 `rlog` 的降采样子集。
- `rlog.zst`：完整高频事件流，本次只保留下载。

qlog 中本次使用的事件：

- `carState.vEgo`：车辆速度。
- `selfdriveState.enabled/active/state`：系统控制状态。
- `selfdriveState.alertText1/alertText2`：当前告警。
- `gpsLocationExternal`：经纬度、方向、精度、GPS 时间。
- `qRoadEncodeIdx`：`segmentNum`、`segmentId`、`timestampSof` 等视频帧索引。

`Event.logMonoTime` 与 `qRoadEncodeIdx.timestampSof` 都使用设备单调时钟纳秒值，可以直接建立视频与遥测时间映射。

## 4. 总体架构

### 4.1 后端模块

新增 `internal/replay`，包含以下清晰边界：

- **Locator**：只负责在 `{DataDir}/uploads/{dongle}/{route}/{segment}/` 下安全定位 `qcamera.ts` 和 `qlog.zst`。
- **Parser**：流式解压、解码 qlog，并输出与前端无关的遥测模型。
- **Cache**：校验缓存指纹、并发去重、原子写入和读取。
- **Timeline**：根据 qRoad 编码索引计算每段时长、整条行程累计偏移及遥测时间。
- **HLS**：生成单段或整条行程 VOD 清单。
- **Ticket**：签发并验证短期、只读、限定设备与行程的媒体票据。

`internal/adminapi` 只做 HTTP 参数、管理员鉴权、错误码和 JSON 编解码，不承担 qlog 解析逻辑。

### 4.2 cereal schema

仓库保存与目标 dragonpilot 版本对应的 cereal schema 快照和许可说明。开发阶段使用：

- `capnproto.org/go/capnp/v3`
- `capnpc-go`
- Cap’n Proto 编译器

生成 Go 类型并提交生成文件。SPK 构建和运行不需要 Cap’n Proto 编译器。解析器对 zstd 解压后的非 packed Cap’n Proto 消息流使用流式 decoder。

Cap’n Proto 对新增字段天然向前兼容；未知 Event union 分支直接跳过。schema 快照变更必须同步提升解析器和缓存版本。

### 4.3 前端资源

- 随仓库固定并内嵌 `hls.js` 生产构建和许可证，不依赖运行时 CDN。
- `web/admin/admin.go` 扩展 embed 范围以包含播放器脚本和静态资源。
- 地图 SDK 由浏览器按管理设置动态加载；未配置或加载失败时使用内置无底图轨迹。

## 5. HTTP 接口

### 5.1 管理员鉴权接口

以下接口继续使用现有 `Authorization: Bearer <admin JWT>`：

```text
POST /admin/api/devices/{dongleID}/routes/{route}/media-ticket
GET  /admin/api/devices/{dongleID}/routes/{route}/replay
GET  /admin/api/devices/{dongleID}/routes/{route}/segments/{segment}/telemetry
```

`media-ticket` 请求指定 `mode=route` 或 `mode=segment`；segment 模式必须给出数字分段号。响应包含播放清单 URL 和过期时间。

`replay` 返回按数字排序的分段摘要：

```json
{
  "route": "00000010--2cbbf69c9f",
  "duration": 720.0,
  "segments": [
    {
      "number": 0,
      "duration": 60.0,
      "has_video": true,
      "has_telemetry": true,
      "telemetry_error": ""
    }
  ]
}
```

`telemetry` 返回当前 segment 的紧凑时间序列；时间单位统一为相对该 segment 起点的秒：

```json
{
  "segment": 0,
  "duration": 60.0,
  "speed": [{"t": 0.0, "mps": 0.0}],
  "controls": [
    {
      "t": 0.0,
      "enabled": false,
      "active": false,
      "state": "disabled",
      "alert_text_1": "",
      "alert_text_2": ""
    }
  ],
  "gps": [
    {
      "t": 0.0,
      "latitude": 0.0,
      "longitude": 0.0,
      "bearing_deg": 0.0,
      "accuracy_m": 0.0
    }
  ]
}
```

### 5.2 媒体票据接口

以下接口不读取管理员 JWT，只验证媒体票据：

```text
GET /media/hls/{ticket}/index.m3u8
GET /media/hls/{ticket}/{segment}.ts
```

票据声明包含：

- dongle ID
- route
- mode（route 或 segment）
- 允许的 segment（仅 segment 模式）
- 过期时间
- 随机票据 ID

票据默认有效 15 分钟。前端在到期前刷新票据；刷新 HLS source 时保留当前播放时间。票据不能用于下载 qlog、rlog 或访问其他设备/行程。

## 6. HLS 播放

### 6.1 清单

服务端动态生成 RFC 8216 VOD media playlist：

- `#EXT-X-PLAYLIST-TYPE:VOD`
- `#EXT-X-MEDIA-SEQUENCE:0`
- `#EXT-X-TARGETDURATION` 使用所有已纳入分段时长的向上取整最大值
- 每个 qcamera 文件对应一个 `#EXTINF`
- 分段之间加入 `#EXT-X-DISCONTINUITY`
- 结束时加入 `#EXT-X-ENDLIST`

分段必须按数字排序，不使用字符串字典序。正在上传或缺少 `qcamera.ts` 的分段不进入当前清单；刷新回放页后重新构建。

单段模式只生成一个 TS 条目。整条模式包含所有可播放分段；缺段在 replay 摘要中标记并在 UI 时间线上显示缺口。

### 6.2 时长计算

每段从 qlog 中读取 `qRoadEncodeIdx`：

1. 过滤 `segmentNum` 对应当前目录分段号的索引。
2. 用第一帧与最后一帧 `timestampSof` 差值计算基础时长。
3. 加上相邻帧时间差的中位数，包含最后一帧。
4. 没有有效索引时回退为 60 秒，并在摘要中标记 `duration_estimated=true`。

### 6.3 浏览器策略

- Safari（macOS/iOS）检测到原生 HLS 后直接设置 `<video src>`。
- Chrome/Edge/Android 在支持 MSE 时使用 `hls.js`。
- 两者都不支持时显示明确说明并保留下载按钮。
- 播放器按需预加载，不先把整条视频下载到浏览器内存。

## 7. qlog 解析与缓存

### 7.1 解析流程

每个 qlog 独立解析：

1. 打开 `qlog.zst`。
2. 使用纯 Go zstd reader 流式解压。
3. 使用 Cap’n Proto stream decoder 逐条读取 Event。
4. 只提取本设计列出的事件，其余事件立即丢弃。
5. 以第一条有效 qRoad 帧 `timestampSof` 为 segment 零点。
6. 只保留落在该视频 segment 时间窗内的遥测数据。
7. 输出紧凑 JSON 缓存。

速度保留 qlog 原始降采样频率；GPS 和控制状态仅在有新消息时追加，不人为插值。前端显示时使用“时间点之前最近一条值”。

### 7.2 缓存

缓存路径：

```text
{DataDir}/replay-cache/{dongleID}/{route}/{segment}.v1.json
```

缓存头保存：

- parser/cache 版本
- qlog 文件大小
- qlog 修改时间
- schema 版本标识

任一项变化即重建。写入同目录临时文件，`fsync` 后原子 rename。并发首次访问同一 segment 时合并为一次解析。

### 7.3 限制与降级

- 单个 qlog 最大解压量：256 MiB。
- 单个 qlog 最大 Event 数：2,000,000。
- 超限、损坏或截断时停止该段解析并返回可定位的段号与错误类型。
- 未知 Event 跳过，不视为错误。
- 有 TS、无可用 qlog：视频照常播放，遥测区显示不可用。
- 有 qlog、无 TS：允许查看曲线和轨迹。
- 某段损坏不阻止其他分段播放。

## 8. 视频与遥测同步

每个 segment 缓存保存：

- segment 视频时长
- qRoad 第一帧单调时间
- segment 在整条行程中的累计偏移

前端监听 `video.currentTime`：

1. 根据累计偏移定位当前 segment。
2. 将整条视频时间转换为 segment 相对时间。
3. 对速度、控制状态和 GPS 数组做二分查找。
4. 更新当前车速、控制状态、告警、速度曲线游标和地图车辆位置。

拖动视频、上一段/下一段和点击速度曲线都更新同一 `currentTime`，避免维护两套播放状态。

## 9. 管理页面

### 9.1 布局

采用用户确认的“视频优先”方案：

桌面：

1. 返回行程列表、行程名、整条/分段切换、分段导航。
2. 大尺寸 16:9 视频。
3. 当前车速、已启用/未启用、告警状态条。
4. 地图与速度曲线左右并排。
5. 控制启用区间和缺段标记时间轴。

手机：

1. 视频
2. 状态条
3. 地图
4. 速度曲线和控制时间轴

### 9.2 地图适配器

管理设置新增：

- `map_provider`：`none`、`amap`、`tencent`
- `map_web_key`
- `map_security_code`：高德需要时使用

后端只向已认证管理员返回这些设置。Web Key 在浏览器运行地图 SDK 时必然可见，文档要求在地图控制台限制允许域名。

缓存始终保存 WGS-84 原始坐标：

- 高德/腾讯显示前转换为 GCJ-02。
- `none` 模式直接在内置画布上绘制 WGS-84 相对轨迹。
- SDK 加载失败自动退化到 `none`，不影响视频和遥测。

## 10. 安全与错误处理

- 所有 dongle、route、segment 参数均由数据库元数据解析，不直接拼接未验证的用户路径。
- 最终路径必须位于目标设备 uploads 根目录内。
- 媒体响应只允许普通文件且文件名必须为 `qcamera.ts`。
- 媒体票据过期、签名错误或范围不匹配统一返回 401/403。
- 管理接口不存在的设备、行程或分段返回 404。
- HLS/TS 响应设置准确 Content-Type，并支持 HEAD 和 Range。
- 单个 TS 加载失败时 UI 显示分段号，允许重试或跳到下一段。
- 地图错误、qlog 错误和视频错误分别显示，不互相覆盖。

## 11. 测试策略

### 11.1 Go 单元与集成测试

使用当前 cereal 生成类型在测试中构造不含真实位置的最小 Event 流，再做 zstd 压缩，避免提交用户行程数据。

覆盖：

- carState、selfdriveState、GPS、qRoadEncodeIdx 提取。
- segment 相对时间与整条累计时间。
- 损坏 zstd、截断 Cap’n Proto、未知 Event。
- 解压量和 Event 数上限。
- 缓存命中、文件变化失效、schema 版本失效。
- 并发首次解析只生成一个完整缓存。
- 分段数字排序、缺视频、缺 qlog、缺段。
- 单段和整条 HLS 清单。
- 媒体票据签名、过期、设备/行程/分段越权。
- TS 的 HEAD、Range、Content-Type。
- 管理设置中的地图 provider 校验。

### 11.2 构建验证

每次代码修改按项目规则执行：

```bash
go test ./...
go build -o bin/pilotserver ./cmd/pilotserver
./synology/build-spk.sh
```

检查 SPK 中：

- 仍只有一个服务端可执行文件。
- hls.js、播放器静态资源和 cereal 生成代码均已包含在 Go 二进制中。
- 无 Node、Python、FFmpeg 运行依赖。

### 11.3 浏览器验收

- 桌面 Chrome/Edge：hls.js 单段/整条、拖动、跨段。
- Android Chrome：触控播放、纵向布局、地图降级。
- macOS/iPhone Safari：原生 HLS、票据续期、后台恢复。
- 无地图 Key、高德配置、腾讯配置。
- 视频缺失、qlog 损坏、媒体票据过期。

## 12. 分步交付

### 第一步：qcamera 在线播放

- 媒体票据。
- HLS 单段/整条清单。
- TS 安全流式响应。
- hls.js + Safari 原生 HLS。
- 视频优先响应式播放器。
- 保留下载入口。

成功标准：目标四类浏览器可播放单段和整条行程，不整文件加载到浏览器内存，未登录或越权不能读取视频。

### 第二步：qlog 遥测同步

- cereal schema 快照和 Go 生成代码。
- zstd/Cap’n Proto 流式解析。
- 遥测缓存和 replay API。
- 车速、控制状态、告警、速度曲线。
- 可配置高德/腾讯地图和无底图降级。
- 视频/遥测双向时间同步。

成功标准：播放或拖动视频时，车速、控制状态、曲线游标和地图位置同步更新；缺失或损坏的单段不会阻断整条行程其他部分。

## 13. 后续 Cabana 边界

完整 `rlog.zst`、原始 CAN 信号和 dbc 分析继续交给后续 Cabana 集成。Cabana 可复用：

- 安全 Locator
- 媒体/文件票据
- 行程与分段摘要
- qlog 缓存时间轴

本次不把完整 rlog 转换为 JSON，也不在自研管理页复制 Cabana 的信号分析能力。

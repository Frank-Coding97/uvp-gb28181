---
title: OpenAPI 设备能力源清单
aliases:
  - GB/T 28181-2022 OpenAPI 能力矩阵
status: in_progress
date: 2026-10-11
---

# 范围与状态

本表以 `server/app/gb28181/routes/routes.go` 的设备管理路由和对应应用服务为源，区分“对外 OpenAPI”与“平台内部已经有入口”。内部 JWT/Casbin controller 不是第三方可调用的服务契约。

- **已对外**：代码路由、scope、catalog 和 YAML 已存在；仍需单独做认证 API、catalog release 和设备验收。
- **本地新增**：本次工作区新增；尚未发布 catalog release，也未做线上或真机验收。
- **待开放**：内部已有实现或可复用服务，但尚无完整的 OpenAPI 鉴权、数据范围、adapter、幂等和契约闭环。
- **需补实现**：现有入口缺少可供 machine client 使用的领域服务，或生命周期/权限归属尚未闭合。
- **排除**：平台管理、内部诊断或设备上行回调，不属于第三方设备业务 API。

# 已公开契约

| 能力 | scope | 状态 | 备注 |
|---|---|---|---|
| 设备列表、详情、状态 | `device:list`, `device:detail`, `device:status` | 已对外 | 读接口通过 client 部门数据范围过滤。 |
| 通道列表、详情、状态 | `channel:list`, `channel:detail`, `channel:status` | 已对外 | 通道必须同时匹配请求设备与 client 可见部门。 |
| 实时点播 | `play:live` | 已对外 | 媒体/quota 由既有播放 dispatcher 管理；响应表示平台播放会话，不代替首帧验收。 |
| PTZ 预置位列表/保存/调用/删除、PTZ 操作查询 | `ptz:preset:list`, `ptz:preset:save`, `ptz:preset:call`, `ptz:preset:delete`, `ptz:operation:read` | 已对外 | 保存、调用、删除使用幂等键；设备回执状态仍按 operation 语义返回。 |
| 通道控制能力三态查询 | `channel:capabilities` | 本地新增 | 只读取平台已保存的能力声明；`unknown` 不表示支持。YAML 和部门范围测试已补，本地 release/真机状态未验证。 |

# 设备侧能力族

| 能力族 | 现有内部入口 | 标准/平台语义 | OpenAPI 状态与必要边界 |
|---|---|---|---|
| 目录、设备/通道信息及状态 | `/device-mgmt/devices`, `/device/:id`, `/channels`, `/channel/:id`, `/channel/:id/device-status`, `/device/:id/catalog/refresh`, `/device/:id/catalog/refresh/:operationId`, `/device/:id/status-events` | 目录查询、异步刷新、在线状态和平台生命周期记录 | 基础列表/详情/状态已对外；刷新状态及生命周期事件待开放。主动 `DeviceInfo` 刷新尚无清晰的对外服务契约。 |
| 设备能力、控制能力和存储状态 | `/channel/:id/control-capabilities`, `/channel/:id/storage-cards`, `/channel/:id/ptz/precise-status` | 能力来源是设备显式上报、PTZType 或平台采集结果；未知状态必须保留 | 控制能力和精准 PTZ/看守位/巡航轨迹读取已本地新增；存储卡状态待开放并补数据更新时间/未知状态。 |
| 普通 PTZ、精准/扩展 PTZ、镜头动作 | `/channel/:id/ptz`, `/ptz/precise`, `/ptz/extended`, `/ptz/wiper` | 由设备 `EffectiveVersion` 选择协议 profile；2022 精准控制与通用辅助动作有明确参数限制；雨刷是单独受限动作 | 普通、精准/扩展、雨刷、看守位设置、巡航控制与目标跟踪已本地新增独立 scope；尚缺真机/模拟器验收及少量轨迹操作。调用方不能指定设备协议版本。 |
| 预置位、看守位、巡航 | `/ptz/presets*`, `/ptz/home-position`, `/ptz/cruise`, `/ptz/cruise/tracks*`, `/ptz/operations/:operationId` | 保留 PTZ service 的缓存、版本选择、操作记录和异步状态 | 预置位/操作结果已对外；看守位/巡航控制和读取已本地新增。创建巡航轨迹及单条轨迹读取仍待开放；均需重新验证 device/channel 归属。 |
| 通用设备控制 | `/channel/:id/device-control` | 现有动作覆盖关键帧、设备录像启停、报警布撤防/复位、重启和拖框变倍等；应按协议响应策略区分已下发、设备应答和结果未知 | 录像启停、报警布撤防/复位及设备重启已有独立 OpenAPI adapter/确认约束，并在 SIP 代际绑定；关键帧及其他设备控制待开放。不能暴露通用 `action` 透传。 |
| 视频参数、设备配置 | `/channel/:id/video-params`, `/channel/:id/device-configs` | 读走 ConfigDownload、写走 DeviceConfig；通用配置和视频参数分开对账；不接受任意 XML | 待开放。现有内部入口下发后回读对账，OpenAPI 的版本/能力门禁和操作结果契约尚未实现。 |
| GB/T 28181-2022 目标跟踪 | `/channel/:id/target-track` (GET/POST) | 2022 目标跟踪为无应答命令；GET 返回平台最近一次下发意图，`deviceAcknowledged=false`，不能伪报执行成功 | 待开放。现有构造器在 2016 profile 也可构造，写入未按三态能力拒绝；OpenAPI 需明确新增版本/能力门禁，再使用独立 scope、参数白名单及幂等。 |
| GB/T 28181-2022 存储卡 | `/channel/:id/storage-cards`, `/channel/:id/storage-cards/format` | 状态查询与格式化分离；格式化会清除录像；`cardIndex` 必须显式提供，`0` 合法且表示全部存储卡 | 待开放。格式化使用专属 `destructive` scope、`confirmed=true`、审计及幂等键；缺字段才拒绝，确认提示必须明确“全部卡”。 |
| 设备重启、软件升级 | `/device/:id/reboot`, `/device/:id/maintenance-operations`, `/device/:id/firmware-upgrade`, `/device/:id/firmware-upgrades` | 远程重启需显式确认；当前固件升级服务仅支持 GB/T 28181-2022 profile，并须检查设备型号与批准固件 | 重启、维护 operation 查询和固件升级已有本地 adapter 并随 SIP 代际绑定；未完成 catalog release、设备型号验收。重启/升级分权；固件仓库管理本身排除。 |
| 报警、状态事件、订阅和移动位置 | `/device/:id/alarms`, `/device/:id/status-events`, `/device/:id/subscriptions`, `/subscriptions/:kind`, `/subscriptions/:kind/renew` | 当前订阅含 Catalog、MobilePosition、Alarm、PTZ 精准位置；解析器兼容 2016 扁平通知与 2022 MobilePosition 列表/Alarm `Info` 嵌套字段 | 告警、状态、位置及 client 隔离的事件订阅/续订/取消/拉取已有本地 adapter；需要 master key 与 SIP subscription runtime 才绑定。事件受保留期限制，暂不承诺无漏读或 webhook 推送。 |
| 录像检索、回放与下载 | `/channel/:id/record-query/options`, `/record-query`, `/playback-sessions*`, `/download-sessions` | 录像查询可能多批响应；历史媒体会话需按设备版本走 MANSRTSP/INFO；SIP 应答不代表媒体已到达 | 检索、回放和下载会话已有 OpenAPI adapter 并随 SIP 代际绑定；下载返回媒体会话状态，不提供文件取回或真实下载进度。2016/2022 检索按设备协商版本构造请求。options 路由及模拟器/媒体验收仍待完成。 |
| 图像抓拍与取图 | `/channel/:id/snapshot-sessions*`；图像库 `GET /snapshots`、`GET /snapshots/:id/content`、`DELETE /snapshots`；设备上行 `/device-snapshots/uploads/*` | 设备上传 token 是短期上行凭证；历史图片、批量删除和新抓拍会话是不同生命周期 | 抓拍/取图 adapter 与跨方言迁移已本地补充，但尚未接 runtime；基线 IR 与生成 SQL 尚未通过开发库重捕获校准，固定且设备可达的上传 URL 配置/bootstrap 接线仍缺。完成这些检查前不发布抓拍 scope。设备上传回调不成为第三方 API，批量删除另作平台数据删除能力评估。 |
| 对讲与广播 | `/channel/:id/talk-sessions*`, talk uplink route | talk/broadcast 均有媒体会话生命周期，广播含异步 SIP/SDP 与媒体发布 | 已有本地 OpenAPI adapter 和 client 会话隔离；仍需确认 adapter 随 SIP runtime 绑定、媒体发布/撤销完整性及模拟器/真机验收。分别授权，不能返回可跨 client 复用的媒体凭证。 |
| 平台存储/云录像配置、挂载与时间线 | `/channel/:id/cloud-recording`, `/channel/:id/mounts`, `/channel/:id/timeline` | 平台录制策略与设备录像命令是不同能力；挂载/时间线依赖媒体服务 | 平台管理/平台数据分类，暂不计入设备侧 OpenAPI 全能力；若将来开放，另行定义平台数据范围和媒体契约。 |

# 内部入口排除项

| 内部路由/能力 | 分类理由 |
|---|---|
| `/catalog/tree*`, `/directory/tree`, custom-groups, permission-workbench | 平台目录编排、部门/用户授权或权限管理。 |
| 设备/通道新增、删除、批量删除、通道资料编辑与 stream-transport 修改 | 平台资源管理或接入配置，不是第三方针对已归属设备的标准操作。 |
| `/firmware-repository*` 上传、列表、状态、删除、下载链接 | 固件仓库管理；设备升级只能引用平台已批准且匹配型号的固件。 |
| snapshot 上传回调及 `/device-snapshots/uploads/*` | 设备上行入口，使用平台短期 token；不向 OpenAPI client 注册。 |
| SIP trace capture | 平台诊断能力，包含信令内容与运维权限，不属于设备业务 API。 |
| `/map/*`、catalog anomaly 与统计 | 平台展示/运维投影；需另行验证字段来源和数据范围后再考虑作为只读业务 API。 |

# 版本和验收边界

设备版本必须来自注册协商保存的 `EffectiveVersion`（GB/T 28181-2022 的 `X-GB-Ver` 标识为 `3.0`），不能由第三方请求体选择。版本支持不等于具体设备型号支持；控制能力采用 supported/unsupported/unknown 三态。无应答控制只报告下发/平台意图，不报告设备已执行。

当前工作区的路由注释给出了若干 2022 附录定位（目标跟踪、存储卡格式化、视频参数、存储卡状态）；正式标准 PDF 当前不在技能所记路径，因此本矩阵没有把这些注释视为逐条标准符合性证明。完整 2022 可选命令矩阵需逐字段回看正式标准，并将协议抓包、模拟器和型号实测分别记录。

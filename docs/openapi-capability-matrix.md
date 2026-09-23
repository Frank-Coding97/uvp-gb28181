# OpenAPI 对外能力盘点矩阵

- 状态：实现前 inventory，只读盘点，不是接口承诺或数据库变更清单
- 盘点日期：2026-09-22
- 目标：围绕设备、通道、播放、设备控制、图像库和录像形成可验证的对外能力闭环

## 1. 结论与边界

1. 外部接口不应直接复用内部 Controller、Gin 路由或 `sys_api` 权限码；应由 OpenAPI Adapter 承接 HMAC、client/租户、部门数据范围、限流、审计和外部错误码，再调用已有领域 Service。
2. `sys_api` 是后台 API 目录（path、method、title、api_group），不是外部 scope。现有 capability registry 只把稳定外部 scope 显式映射到内部 path/method，并在构建目录时校验对应 `sys_api`，新增 scope 必须在实现前 inventory 阶段确认。
3. 对外 client 的数据范围只允许沿用现有定义：`3=本部门`、`4=本部门及以下`。范围必须贯穿设备、通道、状态、播放授权、控制目标和会话资源，不能只在列表接口生效。
4. 下表的“已实现 scope”仅表示当前代码已有契约；“待 inventory”不是建议名称，也不代表已经批准发布。

## 2. 现状矩阵

| 业务域 | 现有 Service 与内部路由/sys_api | 当前 OpenAPI 能力 | 目标外部能力 | 复用、需下沉与缺口 | 风险与验证证据 |
|---|---|---|---|---|---|
| 设备、通道 | `openapi/resource.Service` 已有设备/通道列表、详情、状态查询。内部主要是 `/api/gb28181/device-mgmt/devices`、`device/:id`、`channels`、`channel/:id`、`channel/:id/device-status`，`sys_api` 分组为设备管理。后台按钮权限与外部 scope 不等价。 | `device:list/detail/status`、`channel:list/detail/status` 已实现；外部路径为 `/openapi/v1/devices...`。 | 设备列表、设备详情/状态、按设备查询通道、通道详情/状态；后续补目录/分组等稳定读模型。 | 直接复用资源查询结果；将设备/通道查询抽为平台级 Application Service，避免内部 Controller 与 OpenAPI 各写 GORM。统一传递 `DepartmentScope`。缺口是当前 Service 仍位于 OpenAPI resource 包，业务域尚无统一共享入口。 | 部门范围越权、重复设备/通道主键和状态口径漂移。证据：`server/app/openapi/resource/resource.go:21-55,136-292`；`server/app/openapi/client/capability_registry.go:46-58`；`server/app/gb28181/routes/routes.go:885-905`。验证：部门 3/4 隔离、列表/详情一致性、sys_api 漂移门禁。 |
| 实时播放 | 领域已有 `play.Service.Start/Stop`；内部播放授权为 `/api/gb28181/play/:deviceId/:channelId/authorization`，sys_api 分组为多屏播放。 | `play:live:apply` 已登记，但 `api/openapi-v1.yaml` 明确 `x-uvp-enabled=false`、当前返回 503。 | 播放授权、协议/地址/有效期、会话查询与停止（会话能力需先定归属和停止策略）。 | 复用 `play.Service` 的 `EnsureLive`/播放鉴权链路；OpenAPI Adapter 负责资格票据、client 归属、授权和撤销。需下沉可被外部会话使用的共享停止/查询接口。 | 当前出现“service unavailable；能力授权暂不可用”是契约刻意关闭或运行时依赖未就绪，不能用 200 假装可播放。证据：`api/openapi-v1.yaml:356-404`；`server/app/openapi/media/application.go:96-215`；`server/app/gb28181/play/service.go:581`。验证：未配置/未就绪必为 503，真实设备、节点、协议和授权全链路另行验收。 |
| PTZ 与设备控制 | 内部路由覆盖 `/device-mgmt/channel/:id/device-control`、`ptz`、`ptz/precise`、`ptz/extended`、预置位、巡航、雨刷、看守位、状态和操作查询；sys_api 分组主要为设备控制，后台权限包括 `gb28181:ptz:view/control`、预置位和巡航相关码。已有 `ptz.Service`，升级/对讲也有独立 Service。 | 当前仅开放 `ptz:preset:list/save/call/delete` 和 `ptz:operation:read`。 | 按动作细化：方向/镜头/停止、精确控制、扩展控制、预置位、巡航/轨迹、看守位、雨刷、状态、操作结果；维护、重启、升级、对讲按独立资源和风险分组。 | 复用 `ptz.Service`、`upgrade.Service`、`talk.Service`；抽取统一目标解析、设备版本、在线状态、epoch、部门范围和错误映射。不要把内部复合 `device-control` action 原样暴露成不可演进的大接口。 | 控制类必须幂等键、操作状态、超时和审计；不能把后台 `sys_api`/Casbin 码直接当外部 scope。证据：`server/app/openapi/ptz/dispatcher.go:59-121`；`server/app/gb28181/routes/routes.go:930-986`；`server/resource/database/gb28181/button-permissions.json` 中 PTZ 条目。验证：目标越权、版本 2016/2022、重复请求、设备离线和 SIP 回执。 |
| 抓拍、图像库 | 内部有 `/device-mgmt/channel/:id/snapshot-sessions`、任务查询、图库列表/内容路由及上传内容路由；`devicecapture.Registry` 和持久化快照记录已存在，sys_api/按钮权限使用 `gb28181:device:snapshot`。 | 尚无外部抓拍和图库 scope。 | 抓拍创建、任务状态/结果、按设备/通道/时间/来源查询图库、图片内容读取；区分设备抓拍与浏览器截图。 | 复用 PTZ/设备抓拍下发和图库读模型；抽取外部任务/结果 Application Service，内容下载改成 AK/SK 可验证的短时凭证。任务 Registry 目前为内存态，需决定持久任务状态和重启恢复。 | 图片内容越权、永久 URL、任务重启丢失和大文件响应风险。证据：`server/app/gb28181/routes/routes.go:910-922,1017-1019`；`server/app/gb28181/controllers/device_snapshot.go`。验证：任务重试/超时/重启、部门隔离、内容凭证过期和真实设备回执。 |
| 设备录像查询、回放、下载 | 内部路由为 `/device-mgmt/channel/:id/record-query`、`playback-sessions`、`download-sessions` 及会话操作；已有 `recordquery.Service`、`playback.Service`，sys_api 权限包括 `gb28181:device-record:query/play/download`。 | 尚无外部 scope。 | 录像时间段查询、回放会话创建/查询/控制/停止、下载会话创建/进度/内容。 | 复用 `recordquery.Service` 和 `playback.Service`；抽取外部 `recordKey`、session owner、回放/下载生命周期和 AK/SK 内容授权。现有查询快照和会话 owner 偏后台用户，需要改为 client/租户归属模型。 | SIP 聚合超时、RTP/节点不可用、会话泄露和下载重试语义。证据：`server/app/gb28181/recordquery/service.go:16-79`；`server/app/gb28181/playback/service.go:259-330`；`server/app/gb28181/routes/routes.go:906-925`。验证：代表性设备录像回执、超时/取消/重复请求、跨 client 访问和内容下载。 |
| 云端录像、录像库 | 内部 `/api/gb28181/cloud-recordings/*` 与 ZLM recording runtime 路由已覆盖文件列表、详情、下载、停止、删除和对账；已有 `recording.Service`、`recording.CatalogService`、`recordingplan`。sys_api 分组为云端录像，权限含 `gb28181:recording:view/download/stop/delete`。 | 尚无外部 scope。 | 云端录像文件/目录查询、详情、下载任务及内容、必要的停止/删除；录像计划是否开放需单独 inventory。 | 复用录制生命周期、Catalog 和文件索引；将后台 `userID + Casbin` 访问改为 client/部门范围，下载从 Cookie/JWT 改成 AK/SK 短时授权。运行时录制控制与历史文件读取分开。 | 节点离线、文件缺失、下载任务归属、删除/停止破坏性风险。证据：`server/app/gb28181/recording/service.go:94-138`；`server/app/gb28181/recording/catalog_service.go:117-150`；`server/app/gb28181/routes/routes.go:1017-1023,1115-1121`。验证：文件索引与节点实物、下载续取/过期、删除审计和跨 client 隔离。 |
| 对讲、维护、订阅及其他核心能力 | 内部已有对讲 session、设备重启/升级、订阅查询/更新/续订、目录刷新等路由；对应 Service/运行时注入已存在，sys_api 分组多为设备控制、设备维护或订阅管理。 | 尚无外部 scope。 | 对讲会话、设备维护/升级、订阅/目录刷新等按资源和破坏性风险逐项开放；日志、诊断和内部运维能力默认不外露。 | 复用 `talk.Service`、`upgrade.Service`、订阅和目录服务；先统一设备目标、client 归属、审计和异步任务，再加 Adapter。 | 升级、重启、订阅变更属于高风险动作；不能仅因内部接口存在就发布。证据：`server/app/gb28181/routes/routes.go:895-902,960-964,994-996`；`server/app/gb28181/routes/routes.go:452-460`。验证：显式 scope、幂等、审批/审计、设备真实回执和失败补偿。 |

## 3. 复用边界

| 层 | 允许复用 | 不允许直接复用 |
|---|---|---|
| 领域能力 | `play.Service`、`ptz.Service`、`recordquery.Service`、`playback.Service`、`recording.Service`、`CatalogService`、`talk.Service`、`upgrade.Service` | 内部 Controller、Gin Context、后台用户 ID、Cookie/JWT 下载会话 |
| 资源查询 | 设备/通道模型、状态和持久化记录；抽成共享 Application Service 后供内外适配器调用 | OpenAPI 当前 resource 包中的重复 GORM 查询直接扩散到更多 Adapter |
| 权限目录 | `sys_api` 仅用于内部路由元数据和漂移校验；后台 `sys_menu.permission` 仅用于后台授权 | 把 `sys_api.path`、`api_group` 或 `sys_menu.permission` 当成外部 `scope` |
| 外部边界 | `/openapi/v1/**`、HMAC、client scope、部门范围、限流、审计、稳定错误码 | 让外部请求携带 owner 部门、后台 userID 或绕过 capability registry |

## 4. 实现前必须确认的 inventory 项

1. 首批发布清单：设备/通道、直播、PTZ 全量、抓拍/图库、设备录像、云端录像；每个动作单独确认是否只读、控制、破坏性或异步。
2. 每个新能力的外部 path、请求/响应模型、幂等策略、错误码和 scope；未完成确认前统一记为“待 inventory”，不得在代码中提前硬编码为正式 scope。
3. `DepartmentScope` 从 client 解析到每个资源/会话/内容读取路径的完整链路；至少覆盖数据范围 `3` 和 `4`。
4. 设备控制目标解析、设备版本、epoch/授权、操作状态和审计字段的共享 Application Service 接口。
5. 直播、抓拍、录像、下载会话的 owner、持久化、重启恢复、撤销和内容凭证策略。

## 5. 验证分层

- 源码/契约：OpenAPI YAML、capability registry、Adapter 单测、sys_api path/method/title/group 漂移测试。
- 数据库：开发库的 client scope、部门关系、sys_api/sys_menu_api/Casbin 关联；不把 seed 文件存在当成运行库证据。
- 运行时：HTTP 认证、403/404/503、限流、审计、节点/SIP/ZLM 可用性和会话生命周期。
- 模拟设备：代表性真实形状的 SIP/MANSCDP/RTP 回执，用于控制、抓拍、录像和播放链路。
- 真实设备/生产：另行记录设备型号、协议版本、网络、媒体节点和验收时间；模拟通过不等于真机或生产通过。


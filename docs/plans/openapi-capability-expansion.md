---
title: OpenAPI 设备全能力开放技术方案
aliases:
  - GB/T 28181-2022 能力开放方案
status: approved
date: 2026-10-11
---

# 背景与边界

需求规格已确认：对外开放平台支持的完整设备侧业务能力，覆盖 GB/T 28181-2022 能力和平台扩展。设备型号、协议版本和能力上报决定具体操作是否可用；平台管理、任意内部 API、原始 SIP/MANSCDP 透传不在本方案范围。

当前对外显式注册 12 个操作。OpenAPI 已有 HMAC、时间戳/nonce 防重放、客户端 scope、速率限制、部门数据范围、审计，以及不可变能力 release。执行层已有设备/通道资源 adapter，实时点播和 PTZ 使用专用 gateway dispatcher。当前能力定义还分散于公开路由、catalog bootstrap、兼容 scope registry、YAML 和前端描述映射。

# 架构决策

## 1. 建立代码拥有的能力契约清单

建立一个轻量的代码注册表，逐项定义稳定 scope、业务分组、名称/描述、HTTP method/path、adapter key/contract version、资源类型、风险等级、幂等模式、协议版本/标准依据、是否需要明确确认，以及请求/响应 schema。注册表只描述代码已实现的业务能力，不由数据库记录创建执行能力。

该清单供 Gin 路由注册、catalog bootstrap、adapter 注册校验、兼容 scope 查询和契约测试共用。api/openapi-v1.yaml 继续作为对接文档，通过集成测试逐项与代码清单比对，保持当前显式公开边界；不在运行时根据数据库动态创建路由。

能力描述和 GB/T 版本要求随不可变 release 快照对外返回。优先扩展现有 snapshot_json，避免只为描述元数据扩展 MySQL、PostgreSQL、SQL Server 三套 catalog 表；若 implementation 发现编辑态必须持久化且快照无法承载，再在任务阶段评估最小迁移。

## 2. 用统一签名网关和 catalog adapter 执行设备操作

保留现有唯一公开边界 /openapi/v1/**、HMAC 签名、nonce、防重放、授权、限流、数据范围、审计和请求超时。在 Gateway 上扩展签名 JSON 请求与 path/query 参数解析、精确路由匹配和统一 Invocation，由 active release 确认 scope/method/path/adapter，再由进程内 adapter registry 解析代码注册的执行器。

将实时点播和 PTZ 现有专用 dispatcher 接入该统一执行入口，但保留 app/openapi/play 的 viewer quota/grant 生命周期及 app/openapi/ptz 的设备命令/操作记录实现；只增加薄 adapter，不把业务状态搬进 Gateway。新 adapter 调用现有领域 service/repository，不通过 HTTP 回调内部 /api/** controller，也不复制 controller 的 JWT 与 Casbin 逻辑。

每个 adapter 用第三方客户端的 DepartmentScope 重新解析外部 DeviceID/ChannelID，并在读取/修改关联任务、会话、操作、文件和图片时再次验证设备归属。会话 ID 或下载票据不能替代所属设备校验。

## 3. 保持设备协议语义，不暴露原始报文

OpenAPI 使用稳定 JSON 业务模型。adapter 根据注册设备的有效协议版本、设备上报能力及平台实现能力选择命令；不支持或能力未知时明确拒绝，不猜测支持，也不向低版本设备发送较高版本专属命令。对无应答命令返回“已下发/结果不可确认”的语义，不能把平台落库或 SIP 发送成功说成设备执行成功。

高风险能力用独立 scope；新增 destructive 风险分类并让授权界面显著展示。重启、固件升级、存储卡格式化等接口要求契约中的显式确认字段，并执行审计。写操作按领域支持情况标记 Idempotency-Key 必需或可选；不以请求 nonce 代替业务幂等。

## 4. 对外事件采用可恢复拉取

沿用平台收到的设备事件和持久化告警/状态记录，提供按客户端数据范围过滤的订阅类型、游标拉取和续读。第一阶段不由平台向任意客户 URL 发 HTTP 回调，避免引入 SSRF 面和另一套投递服务；文档说明轮询延迟和事件保留期。对源数据不能提供稳定游标的事件，在任务拆分时确定最小持久化游标/事件快照方案。

## 5. 兼容现有客户端与 release 生命周期

保持当前 12 个 scope 的名称、path、method、请求签名和响应语义。新增 scope 由代码清单、bootstrap 和 adapter 注册一起引入，通过现有不可变 catalog release 发布；新 scope 对既有客户端默认没有 grant，必须由管理员明确授权。版本已有的 release 不原位修改；使用已有 additive bootstrap/publish、active pointer 和 fail-closed hydrate 机制。

前端继续按服务端分组渲染能力目录，将 scope 描述、风险、幂等与协议版本信息改为 release 数据，不再用 scope 的硬编码描述映射。控制台管理路由继续使用原来的 JWT/Casbin 边界。

# 模块与依赖

| 模块 | 职责与改动方向 |
| --- | --- |
| server/app/openapi | 代码能力契约、统一签名执行入口、注册表/catalog 校验与统一错误/审计；复用现有 client、resource、catalog 和 adapter contracts |
| server/app/gb28181 | 以现有 device-management services 为执行边界，补齐缺少的 service API；不从 OpenAPI 调用 Gin controller |
| server/app/openapi/catalog/bootstrap | 从统一契约清单种子化所有设备能力，校验 route、scope、adapter、风险及幂等一致 |
| api/openapi-v1.yaml | 为新增 JSON 操作编写显式 OpenAPI 契约，集成测试对齐代码路由、scope、method、schema 元数据 |
| server/app/openapi/integration | 端到端路由/契约、鉴权、active release 和 fail-closed 边界测试 |
| web/src/views/gb28181/openapi-client | 动态能力分组、协议/风险/确认信息和 scope grant 编辑；只管理权限，不执行设备操作 |
| server/resource/database | 首选只扩展已有 release snapshot JSON；新增事件游标持久化时才增加必要模型及 MySQL/PostgreSQL/SQL Server 迁移和 SQLite 测试基线 |

# 实施顺序

1. 完成设备操作源清单：从 device-management 路由、service、已有 controllers 和 GB/T 28181-2022 命令类别逐项映射到“已开放/可复用待开放/需补实现/设备不支持/平台管理排除”。
2. 建立单一能力契约清单与统一 adapter 执行入口，先迁移并锁定当前 12 项的等价行为，再扩展签名 JSON 请求、幂等与能力目录元数据。
3. 按依赖顺序接入查询/配置/告警，PTZ 与 2022 设备控制/维护，录像检索/回放/下载/实时媒体，抓拍/语音/位置，事件订阅拉取。需求范围不变，各组逐项发布并可独立授权。
4. 对齐 catalog release、管理员授权视图和 api/openapi-v1.yaml；验证旧客户端行为和新增 scope 默认未授权。
5. 先完成模拟器与集成验证，再逐型号做 SIP/MANSCDP/MANSRTSP、媒体、抓拍、语音和维护命令接受测试。设备支持情况单独出表，不从代码测试推断。

# 验证策略

- 契约闭合：代码注册表、Gin 公共路由、bootstrap/release、管理员能力目录、YAML 的 scope/method/path/adapter/schema 全量一致；任何未登记 /openapi/v1/** 路由使测试失败。
- 鉴权闭合：正确签名可访问；scope 未授权、release 缺失、nonce 重放、过期签名、越权部门资源及伪造子资源 ID 均拒绝；active release 无法 hydrate 时 fail closed。
- 请求与幂等：GET 查询参数白名单；JSON POST 精确字段、重复字段、body hash/长度与签名一致；必需幂等键缺失报错，同键重放不会重复控制/创建任务。
- 服务 adapter：以领域 service stub 和数据库 fixture 验证映射、错误、超时、设备/通道作用域与结果语义，不直接测试 controller HTTP 间接调用。
- 协议版本：分别覆盖 GB/T 28181-2016、2022、未知版本以及设备未上报能力；核实 2022 的版本协商、扩展命令、有/无应答差异和回放 INFO/MANSRTSP 行为。
- 异步与资源：轮询状态、终止、超时、失败、游标续读、过期票据和跨客户端/部门访问；订阅对象撤销后不再返回事件。
- 前端及存储：OpenAPI 管理相关 Vitest；catalog/release/bootstrap 集成测试；若有迁移，MySQL/PostgreSQL/SQL Server 脚本及 SQLite baseline 均验证升级和回滚边界。
- 真实验收：源码/单测、数据库 release、已认证 API、媒体首帧、设备命令回执/实际行为分别报告；任一层不替代另一层。

# 风险与权衡

- 能力目录扩展会增加 scope、route、schema、adapter 与标准版本矩阵。统一清单和跨层一致性测试用于防止当前重复定义造成的漂移。
- Gateway 当前通用只读路径和 PTZ/实时点播专用路径不同。先以测试固定现有 12 项，再统一签名请求与 adapter 入口，降低一边扩展一边改变旧调用语义的风险。
- 平台存在内部路由不代表已有可复用领域服务。缺少带第三方 DepartmentScope 的 service 时必须补在领域层；不能绕过资源权限直接复用管理 controller。
- GB/T 标准中的“宜/可”及厂商扩展不保证具体设备实现。能力目录应展示协议条件，调用失败区分设备不支持、未应答和平台错误。
- 事件拉取比 webhook 增加轮询延迟，但省去任意 URL 出站回调的 SSRF 和投递运维风险；若业务明确要求实时推送，应另行评估签名 webhook、目标地址 allowlist、重试与死信队列。
- 目标范围大，建议按能力族形成可独立测试、独立授权的纵向切片；全部能力清单仍是总体完成标准。

# 已确认的技术决策

1. 高风险 destructive 接口采用每次调用 body 中的 confirmed: true，并要求独立 scope；OpenAPI 请求签名已覆盖 body。
2. 事件采用游标拉取而非第一阶段 webhook；通知延迟由客户端轮询周期决定。

技术方案已确认，以下任务清单按这些决策执行。

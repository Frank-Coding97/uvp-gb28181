---
title: OpenAPI 设备控制与媒体能力复用盘点
aliases:
  - OpenAPI PTZ 播放录像能力盘点
---

# OpenAPI 设备控制与媒体能力复用盘点

## 结论

当前平台不需要为对外的每一个能力重新实现一套设备业务 Service。正确的分层是：

```text
AK/SK + scope + data_scope
        |
OpenAPI Gateway / 外部 DTO / 错误与审计
        |
固定的 catalog adapter（代码注册，数据库只选择）
        |
现有领域 Application Service（PTZ、播放、录像、抓拍）
        |
GB28181/SIP、媒体节点、持久化
```

`sys_api` 可以复用为内部接口的路径、方法、标题和菜单元数据，但不能直接作为公网代理目标：内部接口通常使用后台 JWT、用户 ID、Cookie、上传令牌或不适合外部的复合 action。对外能力仍必须有稳定的 scope、外部请求/响应 DTO、资源范围校验、风险级别、幂等策略和 OpenAPI 契约。

因此，“对外 100 个接口”不等于“重新写 100 个 Service”。通常是新增外部契约和 adapter，按能力复用现有 Service；只有当现有 Service 的 owner、授权、生命周期与 OpenAPI client 不兼容时，才抽一层 Application Service，而不是把 Controller 直接搬出去。

## 当前状态

### 目录与分组

- 初始 catalog bootstrap 目前只创建设备/通道 6 个只读能力，见 `server/app/openapi/catalog/bootstrap/bootstrap.go:60-94`。
- 目录已有“设备管理”“多屏播放”“设备控制”三组，后两组目前主要是预留分组，见 `bootstrap.go:54-58`。
- 历史公网 PTZ 和直播路由仍由 `server/app/openapi/routes/public.go:33-44` 显式注册。
- 当前 active release 如果只发布 6 个只读 scope，历史 PTZ 路由会因为 `ScopePublished` 检查而被拒绝。目录发布与历史路由必须在迁移期保持一致，不能只新增数据库行。

### 能力复用矩阵

| 领域 | 可复用核心 | 当前判断 | 主要缺口 |
| --- | --- | --- | --- |
| 设备/通道查询 | `openapi/resource.Service` | 已具备外部闭环 | 继续补齐目录发布和契约验收 |
| PTZ 预置位 | `openapi/ptz.Dispatcher` + `gb28181/ptz.Service` | 可作为第一批控制能力 | 需保证 active release、body DTO、scope 一致 |
| PTZ 全量控制 | `gb28181/ptz.Service.Execute` | 可复用执行内核 | 不能把内部 `action` 原样开放，必须按动作拆 scope |
| 直播授权 | `openapi/media.GatewayDispatcher` + `media.LiveApplication` | 代码链路已具备，当前故意 503 | 需接入启动期资格 provider、grant 和媒体节点就绪链 |
| 录像查询 | `gb28181/recordquery.Service` | 暂不直接开放 | `OwnerUserID uint` 与 OpenAPI `ClientID int64` 不是同一主体 |
| 回放/下载会话 | `gb28181/playback.Service` | 核心 owner 可用字符串 | 依赖录像 `RecordKey`，还需统一 client owner 和授权快照 |
| 抓拍/图像库 | `devicecapture.Registry`、`GbChannelSnapshot` | 元数据读路径可后续开放 | 抓拍任务是内存态，内容不能复用后台 JWT/upload token |
| 云端录像 | `recording.CatalogService` | 文件列表/详情可作为后续只读 | 访问控制绑定后台 `userID` 和 Casbin，需 client-scoped resolver |

## 分域判断

### 1. PTZ 与设备控制

现有 OpenAPI PTZ 已覆盖：预置位列表、保存、调用、删除和 operation 查询，`server/app/openapi/ptz/dispatcher.go:59-77`。目标解析会复用部门范围、设备/通道唯一性、协议版本、在线状态和设备 epoch，见 `dispatcher.go:79-121`。实际执行由 `gb28181/ptz.Service.Execute` 完成，已具备持久 operation、幂等冲突检查、目标可用性检查和 SIP 下发，见 `server/app/gb28181/ptz/operation.go:229-291`。

建议首批按以下粒度发布：

- `ptz:direction`：上、下、左、右及停止；
- `ptz:precise`：精确方向、速度、缩放；
- `ptz:lens`：聚焦、光圈、变倍；
- `ptz:preset:list/save/call/delete`：预置位读写和调用；
- `ptz:cruise:read/start/stop`：巡航读取及启停；
- `ptz:home-position:read/save/call`：看守位；
- `ptz:wiper`：雨刷；
- `ptz:operation:read`：异步操作状态。

所有写动作继续要求 `Idempotency-Key`，响应返回 `operationId`。重启、布撤防、录像开关等设备维护动作不得混入 PTZ scope，应单独分组、单独风险确认和单独权限。

### 2. 实时播放

直播契约明确标记为关闭，`api/openapi-v1.yaml:356-367` 规定当前返回 503。不能为了让接口返回 200 而直接调用 `play.Service.Start`，那会绕过资格票据、节点绑定、client grant 和撤销链。

正确接线是复用 `media.GatewayDispatcher`。它会把认证阶段的目标和资格票据交给 `LiveApplication`，再由 `EnsureLive`、grant issuer 和最终 URL 校验组成完整链路，见 `server/app/openapi/media/gateway_dispatcher.go:17-95`、`application.go:164-239`。在 provider、grant runtime 和媒体节点均未 ready 前，503 是正确的失败关闭行为。

### 3. 设备录像查询、回放和下载

录像查询快照按 `OwnerUserID + ChannelID` 建立并生成 `RecordKey`，见 `server/app/gb28181/recordquery/snapshot_store.go:85-161`。这套 owner 语义是后台用户，不应把 `ClientID` 强转成用户 ID，否则不同 client 可能发生所有权冲突或错误复用。

回放 Service 的 `OwnerID string` 更适合承载 OpenAPI client owner，但它仍依赖前置 `RecordKey` 和 `AuthorizationSnapshot`。落地前需要先抽出统一主体模型，例如：

```text
PrincipalType = user | openapi-client
PrincipalID   = 后台用户 ID | OpenAPI client ID
OwnerKey      = PrincipalType + ":" + PrincipalID
```

然后统一录像查询快照、回放会话、下载任务的 owner 校验，并用 client 的部门范围重新解析设备/通道。可在此基础上实现：`record:query`、`record:playback:create/read/control/stop`、`record:download:create/read`。

### 4. 抓拍与图像库

`devicecapture.Registry` 是内存态，按后台用户 owner，重启丢失并且会在容量上限时裁剪；它不适合作为对外任务状态源。持久化的 `GbChannelSnapshot` 元数据可以先抽出按部门范围查询的 Repository。

建议顺序：先做图像库列表/详情只读能力，再设计抓拍任务持久化、重启恢复和 client owner。图片内容需要短期 OpenAPI 内容凭证或受控内容 URL，不能复用内部 JWT、Cookie 或上传 token。

### 5. 云端录像

`recording.CatalogService` 已有文件列表、详情、下载任务、内容和活动会话操作，但访问控制仍依赖 `userID uint`、后台权限和 Cookie/JWT。文件列表/详情可以作为后续只读能力；下载、内容访问、停止、删除必须先有 client-scoped AccessResolver，不能直接把后台 Controller 暴露出去。

## 推荐交付顺序

1. **完成设备/通道只读闭环**：确认 `data_scope=3/4` 在所有资源、通道、状态和播放目标解析路径生效，并确认 active release 与路由 scope 一致。
2. **迁移 PTZ 预置位**：保留现有 dispatcher 和 PTZ Service，只补 catalog adapter、外部 DTO、发布状态和契约测试。
3. **扩展 PTZ 细粒度控制**：按动作拆 scope，补每个动作的参数边界、幂等冲突、operation 查询和风险分组。
4. **接通直播资格链**：完成 provider、grant、节点 readiness 的启动接线；未满足条件时继续 503。
5. **统一媒体 owner 模型**：先解决录像查询快照与 client owner，再实现录像查询、回放和下载。
6. **图像库与云端录像**：先只读元数据，再处理内容凭证、下载和高风险操作。

## 本次不应直接做的事情

- 不把 `sys_api` 的 path/method 直接当成公网可执行代理；
- 不把内部 `/device-control` 的复合 `action` 参数原样开放；
- 不把 OpenAPI `ClientID` 强转后台 `userID`；
- 不绕过媒体资格票据、grant 和撤销链直接返回播放地址；
- 不把内存态抓拍 Registry 直接当成外部任务数据库；
- 不在 active release 尚未包含 scope 时声称 PTZ 或播放已经可用。

## 验收门槛

- 每个外部 scope 都有唯一外部路由、固定 adapter key/version、内部 `sys_api` 元数据引用和分组；
- 读能力用本部门、下级部门、其他部门、重复归属、已删除资源覆盖测试；
- 写能力覆盖缺少幂等键、重复幂等键、同键不同参数、设备离线和设备 epoch 变化；
- 录像/回放覆盖不同 principal 不能读取、控制或下载彼此任务；
- 直播未 ready 时稳定返回 503，ready 后仍必须通过资格、grant、节点和撤销链；
- 目录发布、旧 scope 兼容、OpenAPI YAML、真实路由注册和数据库快照全部通过契约测试。

## 当前结论

本迭代安全可交付的范围仍是设备/通道只读目录闭环，以及在目录发布一致性确认后迁移已有 PTZ 预置位能力。直播、录像/回放、图像库和云端录像已经有可复用内核，但尚未满足 OpenAPI client owner、内容授权和生命周期要求，不应通过硬编码或 Controller 直连提前开放。

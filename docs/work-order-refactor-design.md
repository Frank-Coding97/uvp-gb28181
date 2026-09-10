# 多屏录像作业（作业单）重构设计

> 状态：**待评审**　最后更新：2026-09-10
> 范围：`web/src/views/gb28181/multi-screen-playback` + `server/app/gb28181/workrecording`

---

## 1. 背景

「录像作业」目前存在**两条并行链路**，同一件事有两套状态机、两套刷新策略、两套停止逻辑：

| | 路径 A · 单通道 | 路径 B · 批次 |
|---|---|---|
| 前端状态 | `useWorkRecording.ts`（按通道 Map） | `index.vue` 内联 `ref` |
| 刷新 | 每 3 秒轮询 + 聚焦再刷一次 | 无轮询 |
| 后端入口 | `POST /work-recordings`、`GET /status`、`POST /:id/stop` | `POST /work-recordings/batches` 等 5 个 |
| 表单 | 录完再补台账 | 录完再补台账 |

由此产生的既有缺陷：

1. `index.vue:99` `slots.slice(0, 4)` —— 硬编码取**前四格**，与实际选中画面脱钩。
2. `workBatchRecording` 是本地 `ref`，**不从服务端派生** → 刷新/重进页面后前端显示"未录制"，服务端其实还在录。
3. 状态文案重复三份且不一致（`index.vue:132-146`、`WorkRecordingJobs.vue:13`、`WorkRecordingBatches.vue:14`）。
4. `BatchService.Start` 无事务（`batch.go:51-113`），`startErr` 在循环中被覆盖，**首个失败原因丢失**。
5. `BatchService.snapshot` 为 N+1；`List` 又对每条批次再调一次 `snapshot`。
6. 作业单状态**双来源**：DB 列 `batch.state` + 读取时 `combineBatchState` 重算。
7. 路数上限硬编码两处：`index.vue:103` 与 `contract.go:41`。
8. 工具栏一行 9 个录像控件 + 8 个权限 `computed`，认知负荷过高。

---

## 2. 决策记录（2026-09-10 已确认）

| # | 决策 | 结论 |
|---|---|---|
| 1 | 表单与录制的先后 | **先填再开始录**。表单必填校验通过后才能开录，取消"草稿"语义 |
| 2 | 录制范围 | **只录正在播放的画面**。无任何正在播放的画面时，录制按钮**禁用并给出原因提示** |
| 3 | 路数上限 | **不限制业务路数**，录所有正在播放的通道 |
| 4 | 接口路径 | 新建 **`/api/gb28181/work-orders`** |
| 5 | 表名 | **沿用** `gb_work_recording_batch` 等现有表，零迁移风险 |
| 6 | ZIP 打包 | 单次作业「几十分钟」量级 → **同步流式 + `zip.Store`**，不做异步产物（已实现） |

---

## 3. 领域模型

### 3.1 概念

- **作业单（Work Order）**：一次现场作业的录像台账。**唯一对外实体**。
- **通道录像（Camera Job）**：作业单下每一路通道的录像生命周期（沿用 `gb_work_recording`）。
- **录像分片（Slice）**：ZLM 产出的单个 MP4 文件（沿用 `gb_recording_file`）。
- **打包产物**：整单 ZIP，**按需实时流式生成，不落盘**。

### 3.2 作业单状态机

```
                ┌─────────────┐
   提交表单 ───▶ │  starting   │  开始中
                └──────┬──────┘
                       │ 全部通道进入录制
                       ▼
                ┌─────────────┐
     结束录像 ──▶│  recording  │  录像中
                └──────┬──────┘
                       ▼
                ┌─────────────┐
                │  stopping   │  结束中  ──▶ stopped  已结束
                └─────────────┘
        任一步失败 ──▶ failed   失败
        归属不可核实 ──▶ unknown  待核实
```

**单一真相（Single Source of Truth）**：作业单状态由 **DB 列 `gb_work_recording_batch.state`** 唯一决定。
子行（通道录像）状态变化时，在同一事务内调用一个集中的 `recomputeBatchState(tx, batchID)` 更新父行。
读取路径**只返回 DB 值，不再重算** —— 这同时解决"双来源"和"刷新后状态丢失"。

**落地方式（已实现）**：录制引擎里所有子作业状态写入都汇聚在 `Service.updateJob`，
因此在该处挂一个**可选观察者**（`Service.SetStateObserver`，由 `bootstrap` 接到
`BatchService.ObserveJobState`）。子行状态一变，观察者立刻把聚合结果折回作业单行。
观察者是**尽力而为**的：刷新失败绝不能让触发它的那次录制失败。
`foldChildStates` 负责"最严重状态优先"（unknown > failed > starting/stopping > recording > 其他），
且无子行时视为 `starting`。

---

## 4. 数据模型

沿用现有表，仅调整**语义**，不新增/改名。

### 4.1 `gb_work_recording_batch` —— 作业单主表

| 字段 | 类型 | 说明 |
|---|---|---|
| `id` | varchar(36) | 作业单号（UUID），对外唯一标识 |
| `created_by` | uint | 创建人 |
| `request_id` | varchar(128) | 幂等键，与 `created_by` 组成唯一索引 |
| `state` | varchar(20) | **作业单状态（唯一真相）** |
| `version` | uint64 | 乐观锁版本，每次状态变更 +1 |
| `form_state` | varchar(20) | **取值固定为 `submitted`**（取消 `draft` 语义，字段保留以零迁移） |
| `form_version` | uint64 | 创建时为 1 |
| `schema_version` | uint | 表单结构版本，保留 |
| `device_id` | varchar(20) | 保留 |
| `form_json` | text | 表单内容（16 字段的 JSON） |
| `last_error` | varchar(500) | 最近一次失败原因 |

> **为什么主表没有 `channel_id`**：一张作业单同时录制 **1~N 路**正在播放的画面（决策 3 不限制路数），
> 所以"通道"是**多对一**关系，必须由子表 `gb_work_recording.channel_id` 承载。
> 若在主表存一个 `channel_id`，它只能表示"其中某一路"，且在部分通道启动失败时会与服务端实际状态不一致。
>
> 因此主表**不落通道字段**，通道信息通过两种方式获取：
>
> - 列表页：一次聚合查询得出 `cameraCount`，并按需返回 `cameras: [{ channelId, channelName }]`
> - 详情页：子行 + 分片按 `channelId` 分组
> - 按通道反查历史作业：列表接口支持 `channelId` 筛选（走子表 `EXISTS` 子查询，无需冗余列）

### 4.2 `gb_work_recording` —— 通道录像（子行）

关键字段：`batch_id`（指回作业单）、`channel_id`、`state`、`desired_action`、`version`、
`recorder_claim_version`、`node_id` / `vhost` / `app` / `stream`、`recording_root`、
`started_at` / `stopped_at` / `last_checked_at`、`file_state`、`last_error`。

> 该表同时是 `workrecording.Service`（录制引擎）的持久化载体，**引擎层不改**。

### 4.3 `gb_work_recording_file` / `gb_recording_file` —— 分片关联

`gb_work_recording_file(file_id, work_recording_id)` 连接两者，据此可按作业单聚合全部分片。

### 4.4 表单必填范围（已实现，改动集中在一个函数）

现有 `Form.Validate()`（`workrecording/form.go`）**只校验长度，没有任何必填校验**。
"先填再录"因此新增了独立方法 `ValidateForStart()`，必填规则集中在 `missingRequiredFields()`：

| 必填 | 字段 |
|---|---|
| ✅ | `projectName` 项目名称 |
| ✅ | `stationArea` 站区 |
| ✅ | `anchorSectionNo` 锚段号 |
| ✅ | `workLeader` 作业负责人 |
| ✅ | `workPersonnel` 作业人员（至少 1 名非空白） |
| ✖ | 其余 11 个字段选填 |

> **为什么不直接改 `Validate()`**：legacy「先录后补作业单」流程下草稿必然不完整，
> 把必填塞进 `Validate()` 会立刻打断 `SaveDraft`。P4 删掉草稿路径后再合并两个方法。
>
> 纯空白不算填了；校验失败的错误信息会**逐项列出缺失字段**（如
> `作业表单缺少必填项: 站区、作业负责人`），前端直接展示。
>
> 前端 `WorkRecordingFormDialog.vue` 的 rules 必须与此**同步**（与 `CLAUDE.md` 密码规则同一原则）。

### 4.5 路数上界（防御性，非业务限制）

决策 3 是"不限制业务路数"，但请求体必须有界。设：

```
WorkOrderMaxCameraCount = 16   // = 多屏最大布局（1/4/6/8/9/16）的 16
```

即"随你录多少正在播放的画面"，同时挡住畸形请求。**若确实要完全无界，请告知**。

---

## 5. 接口契约

统一前缀 `/api/gb28181/work-orders`。响应沿用 `{ code, data, message }`。
**仅创建人本人可见自己的作业单**（沿用 `created_by` 过滤）。

### 5.1 创建作业单并开录（先填再录）

```
POST /api/gb28181/work-orders
```

请求体：

```json
{
  "requestId": "3f2a1c9e-...",
  "channelIds": [12, 15, 27],
  "form": {
    "projectName": "沪宁线放线作业",
    "major": "接触网",
    "stationArea": "南京南—江宁",
    "mileage": "K12+300",
    "anchorSectionNo": "A-12",
    "startAnchorPillarNo": "101",
    "endAnchorPillarNo": "118",
    "workLeader": "张伟",
    "workPersonnel": ["张伟", "李强"],
    "tensionWireCarModel": "JQ-130",
    "tensionWireCarNo": "苏A12345",
    "setTension": "13.5",
    "straightenerStatus": "正常",
    "straightenerInspector": "王敏",
    "wireLayingProcess": "由小里程向大里程",
    "remark": ""
  }
}
```

响应 `200`：

```json
{ "code": 0, "message": "success", "data": { /* WorkOrderSnapshot，见 5.5 */ } }
```

| 状态码 | 场景 | `message` |
|---|---|---|
| `400` | 通道列表为空 / 超过 16 / `requestId` 不合法 | 作业单请求不合法 |
| `400` | **表单必填校验失败** | 具体字段缺失提示 |
| `403` | 存在不可见的通道 | （沿用 `visibleChannel` 语义） |
| `409` | 通道已被其他作业占用（`gb_recorder_claim`） | 录像资源已被其他任务占用 |
| `409` | 部分通道启动失败（整单回滚） | 首个失败原因 |
| `200` | `requestId` 幂等命中 | 直接返回已存在的作业单 |

**一致性与回滚语义**（实现约束，比"同一事务"更准确）：

1. **表单校验先于一切副作用**：`OrderStartRequest.Validate()` 在创建任何记录之前执行，
   表单不合规 → 不建作业单、不占用任何通道（已由测试锁定）。
2. **子行关联必须原子**：所有子行 `batch_id` 的回填走**单条 `UPDATE ... WHERE id IN (...)` 包在事务里**，
   `RowsAffected` 与预期不符即整单回滚。原实现是"逐条 update、失败只记录不中断"，
   会留下"一半通道挂在作业单上"的中间态。
3. **录制器占用（`gb_recorder_claim` / ZLM）无法回滚**：它跨越数据库边界，属外部副作用。
   因此失败路径采用**补偿**：停掉已启动的子作业 → 整单置 `failed`。
4. **保留首个失败原因**：原实现 `startErr` 在循环中被后一个错误覆盖，导致 `last_error`
   记录的是"最后一个"而非"根因"。现在取首个非空错误，并且：
   - `last_error` 存**具体原因**（如 `camera start failed`）
   - 返回的错误**同时**包含哨兵 `ErrBatchIncomplete`（保证 HTTP 仍是 409）**与**具体原因（可日志追溯）
5. **失败的摄像头仍留在台账**：子行照常关联，便于操作员看到"哪一路没起来"。

### 5.2 作业单列表

```
GET /api/gb28181/work-orders?page=1&pageSize=20&state=&keyword=&channelId=&startTime=&endTime=
```

- `state` 可选，逗号分隔多值
- `keyword` 匹配**作业单号**或**项目名称**
- `channelId` 可选：只看**含该通道**的作业单（走子表 `EXISTS` 子查询，不需要主表冗余列）
- 时间区间作用于 `created_at`

响应 `data`：

```json
{
  "items": [
    {
      "id": "9b057ecd-215f-4caf-af40-6ca570ff1f62",
      "state": "recording",
      "projectName": "沪宁线放线作业",
      "workLeader": "张伟",
      "cameraCount": 3,
      "cameras": [
        { "channelId": 12, "channelName": "K12+300 左线" },
        { "channelId": 15, "channelName": "K12+300 右线" },
        { "channelId": 27, "channelName": "K12+300 全景" }
      ],
      "createdBy": 3,
      "createdByName": "任工",
      "createdAt": "2026-09-10T08:12:00+08:00",
      "startedAt": "2026-09-10T08:12:05+08:00",
      "stoppedAt": null,
      "totalBytes": 3221225472,
      "lastError": ""
    }
  ],
  "total": 42, "page": 1, "pageSize": 20
}
```

**性能要求**：列表**不得**逐条调用 `snapshot`（修缺陷 5）。
`cameraCount` / `totalBytes` 用一次聚合查询得出。

### 5.3 进行中作业单（多屏页恢复按钮状态用）

```
GET /api/gb28181/work-orders/active
```

响应 `data`：`WorkOrderSnapshot | null`。
这是修缺陷 2 的关键——**页面加载即拉取，按钮状态由服务端派生**。

### 5.4 作业单详情

```
GET /api/gb28181/work-orders/:id
```

响应 `data`：

```json
{ "snapshot": { /* WorkOrderSnapshot，见 5.5 */ }, "form": { /* BatchFormRecord */ } }
```

把作业单与表单**一次性返回**，详情页不必拼两次响应。表单读取失败不影响整单返回（`form` 缺省）。

### 5.5 `WorkOrderSnapshot` 结构

```json
{
  "id": "9b057ecd-215f-4caf-af40-6ca570ff1f62",
  "requestId": "3f2a1c9e-...",
  "state": "recording",
  "formState": "submitted",
  "formVersion": 1,
  "schemaVersion": 1,
  "projectName": "沪宁线放线作业",
  "createdBy": 3,
  "createdAt": "2026-09-10T08:12:00+08:00",
  "startedAt": "2026-09-10T08:12:05+08:00",
  "stoppedAt": null,
  "lastError": "",
  "form": { /* 16 字段 */ },
  "cameras": [
    {
      "channelId": 12,
      "channelName": "K12+300 左线",
      "deviceId": "34020000001320000001",
      "jobId": "9710bd64-...",
      "state": "recording",
      "fileState": "pending",
      "startedAt": "2026-09-10T08:12:05+08:00",
      "stoppedAt": null,
      "lastError": "",
      "sliceCount": 3,
      "totalBytes": 104857600,
      "files": [
        {
          "id": 88,
          "channelId": 12,
          "fileName": "12_20260910081200.mp4",
          "startTime": "2026-09-10T08:12:00+08:00",
          "timeLen": 300.5,
          "fileSize": 52428800,
          "state": "ready"
        }
      ]
    }
  ]
}
```

> 不返回播放/下载 URL：由前端用已有约定拼装（**必须带 `?token=`**，见 §7）。

### 5.6 结束整单

```
POST /api/gb28181/work-orders/:id/stop
```

逐个结束仍在 `recording` / `unknown` / `starting` 的子作业。
`attribution_unknown` 不视为失败（沿用现有语义）。响应返回更新后的 `WorkOrderSnapshot`。

### 5.7 下载整单 ZIP（已实现并加固）

```
GET /api/gb28181/work-orders/:id/download
```

- 前置校验：所有通道 `stopped`；分片已归档**且磁盘文件存在**（新增 `os.Stat` 预检）
- ZIP 条目：`camera-<channelId>/<原文件名>`，**`zip.Store` 不压缩**
- 归档名：`<项目名>_<yyyyMMdd>_<单号前8位>.zip`（中文走 RFC 5987）
- 仅创建人可下载

### 5.8 单分片在线播放

```
GET /api/gb28181/work-orders/:id/files/:fileId
```

`Content-Type: video/mp4`，`Content-Disposition: inline`，支持 **Range**（可拖进度条）。

### 5.9 删除与批量删除（后续增补）

```
DELETE /api/gb28181/work-orders/:id
POST   /api/gb28181/work-orders/batch-delete      body: { "ids": ["<uuid>", ...] }
```

- **只删非进行中**的作业单（`starting`/`recording`/`stopping`/`unknown` 一律跳过并在响应里回报）。
- 事务内删除 `gb_work_recording_batch` + 子表 `gb_work_recording` + 关联表 `gb_work_recording_file`。
- **保留 `gb_recording_file` 归档索引，且不删除媒体节点上的文件**：删索引会让录像无从查找与清理，
  删文件不可逆。要清理磁盘应另做存储策略。
- 响应：`{ "deleted": 1, "skipped": ["<uuid>"], "missing": ["<uuid>"] }`。
  只剩跳过项时返回 409（提示先结束录像），一条都没命中时返回 404。

### 5.10 权限点

| 权限标识 | 覆盖接口 |
|---|---|
| `gb28181:work-order:create` | `POST /work-orders` |
| `gb28181:work-order:stop` | `POST /work-orders/:id/stop` |
| `gb28181:work-order:view` | `GET /work-orders`、`/active`、`/:id`、`/:id/download`、`/:id/files/:fileId` |
| `gb28181:work-order:delete` | `DELETE /work-orders/:id`、`POST /work-orders/batch-delete` |

菜单与权限通过**新增迁移 SQL**落地（三套库各一份 + 各自 `-down`）：
`sys_menu` / `sys_api` / `sys_menu_api` / `sys_role_menu` / `sys_casbin_rule` 五处都要写，漏一处权限不通。

---

## 6. 前端设计

### 6.1 多屏播放页 —— 工具栏收敛为一个按钮

| 现在（9 个控件） | 目标 |
|---|---|
| 批次开始 / 批次结束 | **一个**「开始录像 / 结束录像」按钮 |
| 作业台账 / 作业记录 / 作业表单 | 移除（迁移到作业单菜单） |
| 单通道开始 / 结束 | **移除**（删掉路径 A） |
| 目标通道名 / 状态文案 / 刷新 / 错误 | 收敛为按钮旁一行状态提示 |

交互规则：

- **候选通道 = `slots.filter(s => s.status === "playing" && s.channel)`** 的通道，去重。
- **候选为空 → 按钮 `disabled`，`title` 提示「请先播放需要录制的画面」**（这是决策 2 明确要求的按钮约束）。
- 点击「开始录像」→ 打开**作业单表单弹窗**，弹窗顶部只读展示"本次将录制 N 路画面"及其通道名，便于确认。
- 表单校验通过 + 提交成功 → 开录，按钮变「结束录像」，状态行显示作业单号 + 通道数。
- 页面加载 / 聚焦时调 `GET /work-orders/active` 恢复按钮状态（修缺陷 2）。
- 若已有进行中作业单，点按钮 = 结束该单。

### 6.2 新增菜单「作业单」

- 路径 `/gb28181/work-orders`，`icon: lucide:ClipboardList`，`sort: 14`（GB28181 菜单是根级平铺）
- component：`gb28181/work-orders/index`（对应 `web/src/views/gb28181/work-orders/index.vue`）
- **列表页**：筛选（状态 / 时间区间 / 关键字）+ 分页 + 列（单号、项目名称、作业负责人、通道数、起止时间、总大小、状态、操作人）+ 操作（查看、下载 ZIP、结束录制）
- **详情页**：上半部表单全部字段；下半部**按通道分组的录像列表**（分片名 / 时长 / 大小 / 在线播放 / 单文件下载），顶部提供「下载整单 ZIP」与「结束录制」

### 6.3 UI 统一要求（`CLAUDE.md` 强制）

1. 使用任何 Arco 组件前，先确认 `web/src/style/arco-overrides.less` 是否已做全局覆盖；**未覆盖的先补**，再写业务。
2. 表单照 `CLAUDE.md` 的 7 条硬规则核对：`allow-clear` / 不用 `a-input-number` 自动 clamp /
   定长字段位数指示 / 校验走 `blur`（`touched` 标记）/ 密码强度条（本表单无密码字段）/
   `<a-form-item label required>` / 禁用原生 `alert|confirm|prompt`。
3. 状态标签统一走**一个**共享映射表，删除三份重复文案（修缺陷 3）。

### 6.4 待收敛的重复实现

`WorkRecordingJobs.vue`（75 行）整份删除；`WorkRecordingBatches.vue`（72 行）改造成作业单列表组件；
两者的 `list + loading + error + sequence + stop` 骨架合并为**一个** `useWorkOrders` composable。

---

## 7. 必须遵守的既有约定

**浏览器直达链接必须自带 `?token=`。**

`window.open` / `<a href>` / `EventSource` 无法携带 `Authorization` 头，后端
`common.GetAccessToken` 支持 `c.Query("token")` 兜底。既有范例：`api/gb28181.ts` 的
`sipDashboardStreamUrl`。本次已修复 `workRecordingBatchDownloadUrl` / `workRecordingBatchFileUrl`
漏带 token 的问题（此前**下载与在线播放实际会 401**）。

---

## 8. 实施顺序

| 阶段 | 内容 | 状态 |
|---|---|---|
| **P0** | ZIP 加固（`zip.Store`、磁盘预检、消 N+1、归档名、中文文件名）；浏览器链接补 token | ✅ 已完成 |
| **P1a** | 表单必填校验（`Form.Validate` 内置必填 + 长度上限，`ErrFormRequired`） | ✅ 已完成 |
| **P1b** | `OrderStartRequest` / `StartOrder`：表单先行、子行关联原子化、保留首个错因、表单落库为 `submitted` | ✅ 已完成 |
| **P1c** | 列表聚合查询消 N+1 + `state`/`keyword`/`channelId`/时间筛选 | ✅ 已完成 |
| **P1d** | 作业单状态单一真相（引擎状态观察者 + `ReconcileState`） | ✅ 已完成 |
| **P1e** | `/work-orders` 路由与控制器 | ✅ 已完成 |
| **P1f** | 作业单菜单与权限迁移 SQL（三套库 + 快照文件） | ✅ 已完成 |
| **P2** | 前端收敛：多屏页 9 控件 → 1 个按钮；删双轨前端；共享状态文案 | ✅ 已完成 |
| **P3** | 作业单菜单页（列表 + 详情 + 关联录像）+ API 层 | ✅ 已完成 |
| **P4** | 删除旧单通道链路（前端组件 + 后端路由与处理器）与死代码 | ✅ 已完成 |
| **P4b** | 退役旧的单通道/批次权限元数据（`2026-09-10-zzzz-work-recording-retire.sql`，三方言 + fail-closed down + 快照追加） | ✅ 已完成 |

---

## 9. 风险

1. **表单必填范围若定错**，现场会因表单卡住无法开录 —— §4.4 需业务确认。
2. 取消 4 路限制后，16 分屏全开 = 16 路并发录制，**ZLM 节点与磁盘 IO 压力上升**，
   需评估单节点承载（与 `zlm` 调度策略相关）。
3. `workrecording.Service` 是录制引擎，**P1 只动暴露层，不动引擎层**；误删会导致录制、
   归属校验、重连恢复一起失效。
4. 旧数据仍留在 `gb_work_recording_batch`，`form_state` 可能为 `draft`：
   P1 需兼容读取（不迁移数据）。

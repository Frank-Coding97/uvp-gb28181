# UVP-GB28181 字典化盘点报告（硬编码枚举 → sys_dict）

- **盘点时间**：2026-09-30
- **代码基线**：分支 `develop`
- **盘点对象**：`server/`（Go 后端）+ `web/src/`（Vue3/TS 前端）
- **目标**：找出全仓「值 → 中文名」的硬编码映射，评估哪些应改由字典表 `sys_dict` / `sys_dict_item` 驱动
- **本报告只做盘点，不含改造**。改造范围与批次见 §五

---

## 一、现有字典机制（改造要落到的地基）

| 层 | 落点 | 说明 |
| --- | --- | --- |
| 表 | `sys_dict` / `sys_dict_item` | `code`（字典编码）+ `value`→`name`；`status=1` 为可选 |
| 后端模型 | `server/app/models/sysdict.go`、`sysdictitem.go` | `FindByCode` / `FindByDictCode` |
| 后端接口 | `server/app/controllers/sysdict.go` | `GET /sysDict/getAllDicts`、`/sysDict/getByCode/:code`、`/sysDictItem/getByDictCode/:code` |
| 前端 API | `web/src/api/dictionary.ts` | `getAllDictsAPI` / `getDictItemsByDictCodeAPI` |
| 前端 store | `web/src/store/modules/system.ts` | `useSystemStore().dict`（已持久化，全量字典） |
| 管理页 | `web/src/views/system/dictionary/dictionary.vue` | 字典 + 字典项增删改（已有；字段型字典建议标只读） |
| 种子真源 | `server/resource/database/baseline/seeds/sys_dict.jsonl` + `sys_dict_item.jsonl` | ⛔ 增量迁移已退役：新增字典必须改 seeds 并重生成三方言（`generate_sql.py --check` 必绿） |

**现有 13 个字典 code**：`gender`、`status`、`post`、`taskStatus`、`ptz_type`、`gb28181_playback_protocol`、`playback_media_state`、`playback_client_state`、`playback_stage`、`playback_lifecycle_state`、`playback_fact_state`、`playback_event_source`、`playback_event`

---

## 二、已经是字典驱动（改造时照抄这个范式）

| 模块 | 位置 | 字典 code | 范式 |
| --- | --- | --- | --- |
| 设备管理·摄像头类型（PTZType） | `web/src/views/gb28181/device-mgmt/index.vue:702` | `ptz_type` | `getDictItemsByDictCodeAPI("ptz_type")` → `ptzTypeOptions`，列头用 `cameraTypeText()` 查表 |
| 国标服务配置·默认播放协议 | `web/src/views/gb28181/playbackProtocol.ts:6`、`sip/ServiceConfig.vue:428` | `gb28181_playback_protocol` | ⭐ **推荐范式**：`xxxOptionsFromDictionary(items)` + `FALLBACK_XXX_OPTIONS` 兜底 |
| 播放日志·7 个状态字典 | `web/src/views/gb28181/playback-log/playbackLogState.ts:6-12` | `playback_*` | 每个维度一个 `*_DICT_CODE` 常量 |
| 基础字段（性别/状态/岗位/任务状态） | `system/dictionary/dictionary.vue`（管理端） | `gender` / `status` / `post` / `taskStatus` | 业务页尚未接入，见 F25 |

> ⭐ **`playbackProtocol.ts` 是本仓正确的字典化范式**：字典为空/接口失败时回落到代码内常量，展示不塌。后续每一项改造都应对齐它。

---

## 三、应改为字典 —— 硬编码枚举清单

### 3.1 前端（`web/src/`）

| # | 模块 | 位置 | 符号 / 形式 | 当前值域 | 建议 code |
| --- | --- | --- | --- | --- | --- |
| F1 | 通道属性·室内外 | `views/gb28181/device-mgmt/channelAttributeText.ts:22` | `ROOM_TYPE_TEXT` 常量映射 | 1室外 / 2室内 | `channel_room_type` |
| F2 | 通道属性·补光方式 | 同上 `:31` | `SUPPLY_LIGHT_TYPE_TEXT` | 1无补光 / 2红外 / 3白光 / 4激光 / 9其他 | `channel_supply_light_type` |
| F3 | 通道属性·监视方位 | 同上 `:40` | `DIRECTION_TYPE_TEXT` | 1东…8西北（8 项） | `channel_direction_type` |
| F4 | 通道属性·位置类型（2016） | 同上 `:52` | `POSITION_TYPE_TEXT` | 10 项（省际检查站…交通干线） | `channel_position_type` |
| F5 | 通道属性·用途（2016） | 同上 `:66` | `USE_TYPE_TEXT` | 1治安 / 2交通 / 3重点 | `channel_use_type` |
| F6 | 通道属性·光电成像类型（2022） | 同上 `:76` | `PHOTOELECTRIC_IMAGING_TYPE_TEXT` | 6 项（可多值 `/` 分隔） | `channel_photoelectric_imaging_type` |
| F7 | 视频参数·编码格式 | `views/gb28181/videoParamCodec.ts:32` | `VIDEO_FORMAT_TEXT` | 1 MPEG-4 / 2 H.264 / 3 SVAC / 4 3GP / 5 H.265 | `video_format` |
| F8 | 视频参数·分辨率 | 同上 `:40` | `RESOLUTION_TEXT` | 1 QCIF…6 1080P | `video_resolution` |
| F9 | 视频参数·码率类型 | 同上 `:49` | `BIT_RATE_TYPE_TEXT` | 1 CBR / 2 VBR | `bit_rate_type` |
| F10 | 设备配置抽屉·同三项 | `device-mgmt/DeviceConfigDrawer.vue:183 / 190 / 199` | 3 个选项数组 | 与 F7–F9 **重复实现**（且漏了 SVAC） | 同 F7–F9 |
| F11 | 播放控制台·视频参数卡 | `components/play-console/PictureVideoParamCard.vue:26` | `VIDEO_FORMAT_OPTIONS` | 同 F7（**第 3 处重复**） | `video_format` |
| F12 | 设备配置·OSD 时间格式 | `device-mgmt/deviceConfigGroups.ts:243` | `OSD_TIME_TYPE_OPTIONS` | ""跟设备走 / 0 / 1 | `osd_time_format` |
| F13 | 设备配置·画面镜像 | 同上 `:256` | `MIRROR_OPTIONS` | 0不启用 / 1水平 / 2上下 / 3中心 | `frame_mirror` |
| F14 | 设备配置·码流编号 | 同上 `:265` | `STREAM_NUMBER_OPTIONS` | 0主码流 / 1–3子码流 | `stream_number` |
| F15 | 通道坐标来源 | `device-mgmt/channelPositionForm.ts:21` | `POSITION_SOURCE_TEXT` | catalog目录 / mobile实时 / manual人工 | `channel_position_source` |
| F16 | 图像库·图片来源 | `snapshot-library/snapshotLibraryState.ts:40` | `SOURCE_LABELS` | device设备抓拍 / zlm平台抓帧 / browser本地截图 | `snapshot_source` |
| F17 | 设备在线状态 | 约 17 处内联三元：`device-mgmt/index.vue:2414,2733,2737,2870,2874,3324,3702`；`device-assignment/index.vue:567`；`DeviceFirmwareUpgradeDrawer.vue:80`；`DeviceMaintenanceRecordsDrawer.vue:418`；`DeviceConfigDrawer.vue:1881`；`recording-schedules/RecordingPlansPanel.vue:290`、`components/ChannelAssignmentDialog.vue:129,168`；`device-record-playback/index.vue:566`；`cascade/index.vue:46,481` | `online ? "在线" : "离线"` | 在线 / 离线 | `device_status` |
| F18 | 级联·上级平台状态 | `cascade/cascadeState.ts:13` | `cascadePresentation()` 六分支 | 已停用 / 在线 / 注册已过期 / 心跳超时 / 等待心跳 / 等待注册 | `cascade_register_state` |
| F19 | 多屏回放·可播性 | `multi-screen-playback/PlaybackSchemePanel.vue:176` | 内联映射 | available可播放 / offline离线 / missing通道不存在 / forbidden无权访问 | `playable_state` |
| F20 | 流媒体节点状态 | `device-mgmt/index.vue:461`、`zlm/components/NodeStateBadge.vue:14`、`zlm/components/LifecycleDot.vue:27`、`zlm/workbench/overview/MediaOverviewPanel.vue:106`、`zlm/workbench/scheduling/SchedulerLogPanel.vue:83`、`zlm/workbench/nodes/NodeListPanel.vue:281` | 多处 `switch` / 内联映射 | active可用 / maintenance维护中 / offline离线 | `media_node_state` |
| F21 | 首页·运行态会话类型 | `home/components/drilldown/MediaRuntimeLedgerDialog.vue:63` | `networkSessionTypeLabels` | 9 项 `mediakit::*` → 中文 | `zlm_session_type` |
| F22 | 系统·任务执行策略 | `system/sysjobs/sysjobslist.vue:503` | `policyMap` | 0单次执行 / 1重复执行 | `job_execute_policy` |
| F23 | 系统·任务阻塞策略 | 同上 `:512` | `policyMap` | 0丢弃 / 1并行 | `job_blocking_policy` |
| F24 | 系统·登录失败原因 | `system/login-log/index.vue:20` | `reasonLabels` | 7 项（验证码错误…服务器错误） | `login_failure_reason` |
| F25 | 系统·启用/禁用 | `dictionary.vue:44,150`、`account.vue:120`、`role.vue:42`、`division.vue:48`、`menu.vue:106`、`userinfo.vue:35`、`select-user/index.vue:88`、`select-department/index.vue:80`、`sysjobslist.vue:29` | 三元 / `a-tag` | 启用 / 禁用 | **`status`（已有字典，只差改用）** |
| F26 | 开放平台·客户端数据范围 | `api/gb28181-openapi.ts:32` | `OPENAPI_CLIENT_DATA_SCOPE_OPTIONS` | 3本部门 / 4本部门及以下 | `openapi_data_scope` |
| F27 | 开放平台·能力说明 | `openapi-client/OpenAPICapabilityWorkbench.vue:26` | `capabilityDescriptions` | 11 条 scope → 说明 | `openapi_capability_desc`（或改由后端下发） |
| F28 | 订阅能力 | `components/PlayConsoleLinked.vue:1555` | 三态文案 | supported支持 / unsupported不支持 / unknown尚未确认 | `subscribe_capability` |
| F29 | 维护操作结果 | `DeviceRebootDialog.vue:78`、`DeviceFirmwareUpgradePanel.vue:155`、`StorageCardFormatDialog.vue:186`、`DeviceMaintenanceRecordsDrawer.vue:148` | `unknown: "结果未知"` 等 | 成功 / 失败 / 结果未知 | `maintenance_operation_result` |
| F30 | 设备配置·回读新鲜度 | `device-mgmt/DeviceConfigDrawer.vue:469` | `FRESHNESS_TEXT` | 尚无回读 / … | `config_read_freshness` |
| F31 | 云录制·持有态 | `cloud-recordings/recordingRuntimeState.ts:25` | `unknown: "未知持有"` 等 | … | `recording_holder_state` |
| F32 | 探针诊断结论 | `views/gb28181/probeDiagnosis.ts:87,94` | 结论文案映射 | 未知 / 无法判断 / … | `probe_diagnosis` |

### 3.2 后端（`server/`）

| # | 模块 | 位置 | 符号 / 形式 | 当前值域 | 建议 code |
| --- | --- | --- | --- | --- | --- |
| B1 | 告警·警情级别 | `app/gb28181/controllers/alarm.go:467` | `alarmPriority()` `map[int]string` | 1一级警情…4四级警情 | `alarm_priority` |
| B2 | 告警·报警方式 | 同上 `:476` | `alarmMethod()` map | 7 项（电话/设备/短信/GPS/视频/设备故障/其他） | `alarm_method` |
| B3 | 告警·报警类型 | 同上 `:487`–`526` | `alarmType()` **嵌套 map**（按 method 2/5/6 分三组，共 20 项） | 视频丢失…存储设备风扇故障 | `alarm_type`（需带 `method` 维度） |
| B4 | 设备状态事件类型 | `app/gb28181/models/gb_device_status_event.go:36` | `DisplayName()` switch | 注册上线/正常注销/心跳超时/心跳恢复/注册续订/链路断开 | `device_status_event_type` |
| B5 | SIP 事务类型中文名 | `app/gb28181/metrics/types.go:58` | `TxKind.LabelZh()` switch | 注册/心跳/目录/点播/录像/报警/控制/挂断/未知 | `sip_transaction_kind` |
| B6 | 任务调度三态文案 | `app/utils/schedulerhelper/job.go:79,106,117` | 三处返回中文 | 启用/禁用、重复执行/单次执行、丢弃/并行 | `status` / `job_execute_policy` / `job_blocking_policy`（**与 F22/F23 同源，必须统一落点**） |
| B7 | 开放平台·能力分组名 | `app/openapi/client/capability_registry.go:76` | `capabilityGroupNames` map | 分组 code → 中文名 | `openapi_capability_group` |
| B8 | 开放平台·客户端状态 | `app/openapi/controllers/client.go:201` | 内联 map | enable / disable / revoke | `openapi_client_status` |
| B9 | 操作日志·结果 | `app/middleware/operationlog.go:304,337` | 返回中文 | 其他 / 请求处理失败 | `operation_log_result` |
| B10 | 网关·结果码文案 | `app/openapi/auth/gateway.go:170` | `messages` map | 11 项（OK→success…） | `openapi_result_code` |

---

## 四、不应字典化（协议常量 / 结构性常量）

⚠️ 这一组**改字典会伤协议**，改造时必须排除：

| 类别 | 位置 | 原因 |
| --- | --- | --- |
| GB28181 报文元素名 | `app/gb28181/manscdp/catalog.go`（`PTZType` / `RoomType` / `<Info>` 等 XML tag） | 元素名由标准固定，改字典即断解析 |
| 目录属性落库归一 | `app/gb28181/catalog/dto.go`、`catalog/upsert.go:173,244` | `0` = 「设备未上报」的**语义判据**，不是展示文案 |
| 云台指令码 | `app/gb28181/ptz/**` | 三族指令码参数位置不同（预置位字节6 / 巡航扫描字节5），写死在协议层 |
| 版本 profile 判定 | `app/gb28181/protocol/profile.go` | 2016/2022 行为分支，非展示 |
| 值域**校验白名单** | `videoParamCodec.ts:96-123`（`isValidResolutionCode` 等）、`deviceConfigGroups.ts` | 校验的是「值合不合法」，不是「值叫什么」；字典化会让非法值被"翻译"掉 |
| 结构性 / 顺序常量 | `WEEKDAY_LABELS`、`MASK_AXES`、`POINT_AXES`、`PTZ_STEPS`、`MAX_*` | 与顺序、坐标口径、标准上限绑定 |
| 落库状态枚举 | `models/gb_device.go:15`、`models/gb_channel.go:12`（`DeviceStatusOnline` 等） | 库内枚举，**展示**才走字典 |
| 纯 UI 枚举 | `viewOptions`、`deviceSegOptions`、`lucideMenuIcons`、`CardTitle` icons | 界面导航，非业务元数据 |
| 协议原始值占位 | `"UDP"/"TCP"`、`"2016"/"2022"` 存库值 | 传输协议/版本是协议字面量，仅展示层可字典化 |

---

## 五、改造建议（决策点 + 批次）

### 5.1 三个必须先拍板的口径

1. **翻译发生在哪一层？**
   - 现状：告警（B1–B3）由**后端**返回带 `label` 的枚举对象；`ptz_type`、播放协议由**前端**查字典译。
   - 建议：新改造统一走**前端译**（后端只返回码值），避免后端被字典数据耦合；告警这 3 项若要动，需同步改 DTO 与既有前端消费点。
2. **同一个 code 多处重复实现必须先收敛**：`video_format` 在 F7/F10/F11 出现 **3 次**且值域不一致（F10 漏 SVAC）；`media_node_state` 在 F20 有 **6 个**落点；`device_status`（F17）散落 **17 处**。先收敛成单点导出，再换数据源，否则改字典等于放大不一致。
3. **字段型字典应标只读**：`ptz_type`、`channel_*`、`alarm_*` 等的值域由 GB/T 28181 标准固定。落进 `sys_dict` 后应标记为**系统级（不可删改）**，否则现场改坏值域会导致解析/对账静默错位。

### 5.2 落地顺序（建议）

| 批次 | 范围 | 理由 |
| --- | --- | --- |
| P0 | **F25**（启用/禁用 9 处）改用已有 `status` 字典 | 字典已存在，纯替换，零新增 |
| P1 | **F7–F11** 视频参数三码值（先收敛 3 处重复） | 重复最严重，收益最高 |
| P2 | **F17 / F20 / F18** 设备状态、节点状态、级联状态 | 散落最广，可读性收益最大 |
| P3 | **F1–F6** 通道属性 6 张表 + **B1–B3** 告警 3 张表 | 协议值域，需先定只读策略 |
| P4 | 其余（F12–F16、F19、F21–F24、F26–F32、B4–B10） | 按需 |

### 5.3 每项改造的固定动作

1. `seeds/sys_dict.jsonl` + `sys_dict_item.jsonl` 追加（`status=1`，`created_by=1` 标系统级）
2. 跑 `generate_sql.py --check`，确认三方言全量脚本同步（⛔ 增量迁移已退役，不要写新迁移）
3. 前端统一 `useDictOptions(code)` + `FALLBACK_*` 常量兜底（对齐 `playbackProtocol.ts`）
4. 单测锚点：字典为空 / 值域外原值回显 / 中文名不落在代码里

---

## 六、口径提醒（改造时容易踩）

- ⛔ **增量迁移已退役**：新字典改 `baseline/seeds/*.jsonl` 重生成，不走迁移目录。
- ⛔ **`channelAttributeText.ts` 的「0 / "" = 未上报」是语义判据**：字典化后仍要保留 `reported` 标记，不能把未上报译成某个中文名。
- ⛔ **协议值域外要原样回显**（`未知(7)` 这类），别吞掉——排障时要看得见设备报了什么。
- ⚠️ `getAllDictsAPI` 已在 store 里全量持久化；新字典若走全量，注意登录态刷新时机。

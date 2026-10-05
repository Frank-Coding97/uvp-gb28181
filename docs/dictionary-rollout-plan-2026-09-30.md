# 硬编码枚举 → 字典化 执行计划（UVP-GB28181）

- **建立时间**：2026-09-30
- **基线**：`develop`
- **盘点来源**：`docs/dictionary-inventory-2026-09-30.md`（前端 32 项 + 后端 10 项）
- **本文件是执行台账**：批次、文件清单、新增字典、判据、状态。盘点结论以盘点报告为准，不在此重复。

---

## 〇、统一口径（先立规矩，后面每批都按它做）

| # | 口径 | 说明 |
| --- | --- | --- |
| 1 | **翻译发生在展示层** | 后端只返回码值，前端用字典译。已字典化的 `ptz_type` / 播放协议就是这个形态；新改造一律对齐。<br>⛔ 例外：告警（B1–B3）现由**后端**返回带 `label` 的对象，要动必须同步改 DTO —— 见 §三 待拍板。 |
| 2 | **先收敛，再换源** | 同一个 code 多处重复实现的（`video_format` 3 处、`media_node_state` 6 处、`device_status` 17 处）**先并成单点导出**，再换数据源。否则改字典 = 放大不一致。 |
| 3 | **字典缺失要兜底** | 一律 `useDictOptions/useDictLabel(code, FALLBACK_*)`。兜底常量按**协议值域**写死，由单测逐条锁定；与开发库字典项的一致性属**人工核对**（单测不连库）。 |
| 4 | **值域外原样回显** | `useDictLabel` 未命中时返回原值（`未知(7)` 这类排障信息要看得见），不吞成空。 |
| 5 | **只动数据翻译** | `a-switch` 的 `#checked`/`#unchecked` 槽位、`是否禁用` 这类布尔是/否、校验白名单、协议原始码 —— **都不动**。 |
| 6 | **新增字典直接补开发库** | ⭐⭐ 2026-10-05 口径变更：开发阶段**直接往开发库 `sys_dict` / `sys_dict_item` 插数据**即可，⛔ **不写任何 SQL 脚本**（不写迁移、不改 `seeds/*.jsonl`、不重生成三方言）。发布阶段再由开发库整理全量脚本、打版本基线；后续升级才走增量脚本。 |
| 7 | **每批都要防回归锚点** | 纯函数用 vitest 单测；整页改造用 `web/src/test/source-assert.ts` 的 `hasMarkup`/`squash` 钉 token，⛔ 不钉排版。 |

---

## 一、基础设施（✅ 已完成）

**`web/src/hooks/useDictOptions.ts`** —— 全仓唯一的字典消费层，范式对齐 `views/gb28181/playbackProtocol.ts`。

| 导出 | 用途 |
| --- | --- |
| `dictOptionsFromItems(items, fallback)` | 字典项 → 下拉选项（滤停用/空/重复，空则回落兜底） |
| `dictLabelsFromItems(items, fallback)` | 字典项 → `{value: label}` 查表（兜底铺底，字典覆盖） |
| `useDictItems(code)` | 取原始字典项，响应式 |
| `useDictOptions(code, fallback)` | 取下拉选项，响应式 |
| `useDictLabelMap(code, fallback)` | 取查表，响应式 |
| `useDictLabel(code, fallback)` | 取翻译函数，未命中回显原值 |
| `DICT_CODE_STATUS` / `STATUS_LABEL_FALLBACK` / `useStatusLabel()` | 通用启用/禁用字典（库内置） |

**测试**：`useDictOptions.test.ts`（11 例）。含「字典改名后界面必须跟着变」这条分水岭用例。

---

## 二、批次台账

| 批次 | 范围 | 涉及文件 | 新增字典 code | 状态 |
| --- | --- | --- | --- | --- |
| **P0** | 启用/禁用展示层 | 7 个文件（见 §二·P0） | 无（复用 `status`） | ✅ **已完成** |
| **P1** | 视频参数三码值 | `videoParamCodec.ts`、`DeviceConfigDrawer.vue`、`PictureVideoParamCard.vue`、新增 `useVideoParamDict.ts` | `video_format`、`video_resolution`、`bit_rate_type` | ✅ **已完成** |
| **P2** | 设备/节点/级联状态 | 18 处在线状态 + `zlm/**` 8 处节点状态 + `cascadeState.ts` | `device_status`、`media_node_state`、`cascade_register_state` | ✅ **已完成** |
| **P3a** | 通道属性（只读展示） | `channelAttributeText.ts`、新增 `useChannelAttributeDict.ts`、`device-mgmt/index.vue` | `channel_*` ×6 | ✅ **已完成** |
| **P3b** | 告警 | `alarm.go`、`alarm-management/*` | `alarm_priority`、`alarm_method`、`alarm_type` | ⏸ **暂缓**（2026-10-05 老板决定暂时跳过；重启前置见 §3.4B） |
| **P4a** | 系统类三件套 | `sysjobs/sysjobslist.vue`、`system/login-log/index.vue`（+ 各 2 个新纯函数/注入层文件） | `job_execute_policy`、`job_blocking_policy`、`login_failure_reason` | ✅ **已完成**（顺带补 `sysjobslist` 里 F25 的漏项） |
| **P4b** | 设备配置族（**会下发给设备**） | `device-mgmt/deviceConfigGroups.ts`、`DeviceConfigDrawer.vue`、`components/PlayConsoleLinked.vue`、`components/play-console/PictureVideoParamCard.vue`（+ 2 个新纯函数/注入层文件） | `frame_mirror`、`stream_number`（`osd_time_format` **已跳过**，见 §3.5-D） | ✅ **已完成** |
| **P4c** | 纯展示批 | 见 §3.5 的 A 组 | 按项（约 8 项） | ⏳ 待做 |
| **P4d** | 维护操作结果收敛 | `DeviceRebootDialog` / `DeviceFirmwareUpgradePanel` / `StorageCardFormatDialog` / `DeviceMaintenanceRecordsDialog` | `maintenance_operation_result` | ⏳ 待做（⚠️ **4 份值域并不相同**，需先定口径） |
| **B4–B10** | 后端文案 | 见盘点报告 | — | ⛔ **不做**（§8「后端不做字典」）；仅当前端另有一份重复时，以前端为准收敛 |

### 二·P0 明细（✅ 已完成）

**改了 9 处状态标签文案**，全部改用 `status` 字典（种子 id=2：`0`=禁用 / `1`=启用）：

| 文件 | 落点 |
| --- | --- |
| `views/system/account/account.vue` | 表格「状态」列 |
| `views/system/role/role.vue` | 表格「状态」列 |
| `views/system/division/division.vue` | 表格「启用状态」列 |
| `views/system/dictionary/dictionary.vue` | 字典列表 + 字典项列表（2 处） |
| `views/system/userinfo/userinfo.vue` | 用户资料 `status` 字段 |
| `components/select-user/index.vue` | 选人弹窗表格「状态」列 |
| `components/select-department/index.vue` | 选部门树节点标签 |

**统一形态**（colors 仍留代码，字典只管文案）：

```vue
<a-tag bordered size="small" :color="record.status === 1 ? 'arcoblue' : 'red'">
  {{ statusLabel(record.status) }}
</a-tag>
```

**为什么这批零风险**：这 7 个页面**本来就在用 `dictFilter("status")` 渲染筛选下拉**，只有表格文案写死 —— 字典必然已加载。

**明确排除（并已用测试钉住）**

| 排除对象 | 原因 |
| --- | --- |
| `a-switch` 的 `#checked`/`#unchecked` 槽位（6 处） | 控件交互文案，不是数据翻译 |
| `menu.vue` 的「是否禁用」 | 读的是 `record.disable`，语义与 `status` **相反**（1=禁用），且渲染的是布尔是/否 |

**防回归锚点**：`useDictOptions.rollout.test.ts`（23 例）—— 逐文件断言走统一字典层、标签里不再写死中文、`STATUS_LABEL_FALLBACK` 与 `sys_dict_item.jsonl` 逐字一致、上述两个排除项保持原样。

**验证**：`useDictOptions*.test.ts` 34 例 + 关联测试共 **50 例全绿**；`vue-tsc --noEmit` 零错误；`eslint` 零告警。

---

### 二·P1 明细（✅ 已完成 2026-10-05）

**新增 3 个字典**（`sys_dict` id 14/15/16，字典项 id 123–135）。依据 = GB/T 28181-2022 附录 G 的 SDP `f` 字段：

| code | 名称 | 值域 |
| --- | --- | --- |
| `video_format` | 视频编码格式 | `1`=MPEG-4 / `2`=H.264 / `3`=SVAC / `4`=3GP / `5`=H.265 |
| `video_resolution` | 视频分辨率 | `1`=QCIF / `2`=CIF / `3`=4CIF / `4`=D1 / `5`=720P / `6`=1080P |
| `bit_rate_type` | 码率类型 | `1`=CBR / `2`=VBR |

**实测比盘点多一处落点**：同一协议值域在本仓被定义了**三份**（盘点只记了两处）。

| 文件 | 原形态 | 问题 |
| --- | --- | --- |
| `views/gb28181/videoParamCodec.ts` | `VIDEO_FORMAT_TEXT` / `RESOLUTION_TEXT` / `BIT_RATE_TYPE_TEXT` | 完整，但写死 |
| `device-mgmt/DeviceConfigDrawer.vue` | `VIDEO_FORMAT_OPTIONS` 等三个 | **只有 4 项，漏 `3`(SVAC)** |
| `components/play-console/PictureVideoParamCard.vue` | 同名三个 | 完整，但它是第三份 |

**改造形态**

- `videoParamCodec.ts` 保持**纯函数模块**（⛔ 不 import Vue/pinia，否则那 41 个用例都要先起 pinia）：三张表改为**可注入参数**，并导出 `VIDEO_FORMAT_LABEL_FALLBACK` / `RESOLUTION_LABEL_FALLBACK` / `BIT_RATE_TYPE_LABEL_FALLBACK` 作兜底。
- 新增 `views/gb28181/useVideoParamDict.ts` —— 唯一的 Vue 侧出口：`useVideoFormatOptions()` / `useVideoResolutionOptions()` / `useBitRateTypeOptions()` / `useVideoParamLabels()`。
- 三个消费点（配置抽屉、播放控制台卡片、播放控制台对账）全部改走它。

**⛔ 顺手修掉一处隐性耦合（本批最要紧的一条）**

`PlayConsoleLinked.vue` 的「设备回读 vs 画面实测」对账，此前是**从人读串反推**：

```
normalizeCodecToken(videoFormatText(码值))        // 编码对比
VIDEO_RESOLUTION_TIERS[resolutionText(码值)]      // 分辨率对比
```

人读串的真源一改成字典，这两处就**静默失效** —— 现场把 `2` 的名字换个写法，编码对比退化成"未读取"，且看不出原因。现已改为认码值：新增 `videoFormatCodecToken()` / `resolutionPixels()`（另有 `pixelsOf` / `normalizeCodecToken` 上移到 `videoParamCodec`）。**字典只改展示名，不再是任何判定的输入。**

**明确不动**：值域**校验白名单**（`isValidVideoFormat` / `isValidResolutionCode` / `isValidBitRateType` / `isValidFrameRate` / `isValidVideoBitRate`）—— 字典管"叫什么"，不管"合不合法"。

**防回归**

- `useVideoParamDict.test.ts`（13 例）：三个**协议值域逐条锁定**（⚠️ 原为"与 `sys_dict_item.jsonl` 逐字比对"，10-05 口径变更后改为正向锁定）；字典空/改名/停用三态；三处收敛扫描（`MPEG-4` 指纹）；耦合点反向断言；校验白名单未被字典化。
- `videoParamCodec.test.ts` 扩到 41 例：新增注入查表、码值换算、以及「字典改名 ⇒ 码值换算结果不变」的分水岭用例。

**⛔ 顺手补的一处测试基建**

`src/test/setup.ts` 全局补齐两件（**改了消费组件就会用到**）：

- `Object.assign(globalThis, { ref })` —— `store/modules/system.ts` 的 `ref` 是构建期自动导入的，vitest 没有该插件；⚠️ 测试文件里 `import { ref } from "vue"` **不管用**，必须挂 `globalThis`。
- `beforeEach(() => setActivePinia(createPinia()))` —— ⛔ **不要只塞 `config.global.plugins`**：那是同一个实例，前一个用例写的 dict 会漏到下一个。

**踩坑记录**：改完只跑了"自己挑的 3 个文件"（95 例全绿）就以为收工，**全量才发现 `PlayConsoleLinked.test.ts` 188 例全灭** —— 只因 `PlayConsoleLinked.vue` 多了一个 `useSystemStore()`。
⇒ 口径：**改了消费组件，必须先把「所有 mount 它的测试文件」列出来跑一遍**，再跑全量。

**验证**

- 本批相关 **296 例全绿**：`videoParamCodec` 41 + `useVideoParamDict` 12 + `DeviceConfigDrawer` 42 + `PlayConsoleLinked` 201。
- **前端全量 2181 例：2176 通过 / 5 失败**，5 个失败**全是存量**：`uvp-tokens` ×2、`theme-toggle` ×1、`DeviceFirmwareUpgradePanel` ×1、`index.deviceDetailTabs` ×1。
  ⛔ 已确证：把 `DeviceConfigDrawer.vue` 还原到 HEAD 再跑 `index.deviceDetailTabs`，**照旧失败**（1 failed / 12 passed）⇒ 与本批无关（全是样式契约 / 固件模块的在途问题）。
- ~~`generate_sql.py --check`~~ **数据库脚本改动已全部撤回**（口径变更，见 §三·3.1）；`eslint` 零告警；`vue-tsc` 本批改动文件零错误（存量错误集中在 `views/gb28181/firmware-repo/*` 与 `DeviceFirmwareUpgradePanel.*`）。

**提交状态**：✅ 已提交 `a2f0724b feat(dict): 视频参数三码值收敛并字典化（P1）`（10 个文件，+629/−106）。
⛔ **数据库侧零改动**（seeds 2 + 三方言 3 + 迁移 1 已全部撤回）。

### 二·P2 明细（✅ 已完成 2026-10-05）

**新增 3 个字典**（⛔ 直接写开发库 220，不写任何 SQL 脚本 —— 见 §三·3.1）：

| 字典 code | 值域 | 开发库 id |
| --- | --- | --- |
| `device_status` | `online`=在线 / `offline`=离线 | 17（项 136–137） |
| `media_node_state` | `active`=在线 / `maintenance`=维护中 / `offline`=离线 | 18（项 138–140） |
| `cascade_register_state` | `disabled` / `online` / `registration_expired` / `heartbeat_stale` / `awaiting_heartbeat` / `awaiting_registration` | 19（项 141–146） |

**新增 3 个前端模块**（纯函数 + 注入层分层，对齐 P1 的 `videoParamCodec` / `useVideoParamDict`）：

| 文件 | 职责 |
| --- | --- |
| `views/gb28181/deviceStatus.ts` | 字典 code + 兜底 + **归一化**（boolean / `1|0` / `"online|offline"` 三种形状 → 同一把尺子）+ `deviceStatusLabelFrom` |
| `views/gb28181/useDeviceStatusDict.ts` | `useDeviceStatusLabel(unknownText?)` 注入层 |
| `views/gb28181/mediaNodeState.ts` | 字典 code + 兜底 + `mediaNodeRuntimeKey`（**判定**）+ `mediaNodeStateRuntimeText`（**文案**）+ 下拉兜底 |

**F17 设备/通道在线状态（18 处）**：`device-mgmt/{index,DeviceConfigDrawer,DeviceStatusFactsPanel,DeviceFirmwareUpgradeDialog,DeviceMaintenanceRecordsDialog}`、`device-record-playback/index`、`recording-schedules/{RecordingPlansPanel,components/ChannelAssignmentDialog}`、`device-assignment/index`、`multi-screen-playback/PlaybackSourceTree`、`cascade/index`。
⛔ **各站点原有极性原样保留**（`x ? A : B` ⇒ `!!x`；`x === false ? A : B` ⇒ `x !== false`；`status === 1 ? A : B` ⇒ `status === 1`）—— 未知值算在线还是离线是**该站点的语义**，本批不替它决定。
⛔ **排除的非本值域**：`security/preview.vue`（主机防火墙 在线/降级）、`zlm/.../ProxyPanel.vue`（代理 在线/失败·离线）、`multi-screen-playback/PlaybackSchemePanel.vue`（可播放性 4 值）、`components/PlayConsoleLinked.vue:2194`（**错误串匹配**，不是展示）、`device-mgmt/index.vue` 的「已注册 / 运行正常 / 当前离线」（**是更长的成句状态**，不是这个二值）。

**F20 媒体节点状态（8 处）**：`zlm/components/{NodeStateBadge,LifecycleDot}.vue`、`zlm/workbench/components/MediaScopeBar.vue`、`zlm/workbench/scheduling/SchedulerLogPanel.vue`、`zlm/workbench/nodes/NodeListPanel.vue`（筛选下拉**补上 `maintenance`**，此前维护态筛不出来）、`device-mgmt/index.vue`（节点选择器）。
⭐⭐ **收敛了两处重复实现**：`zlm/workbench/overview/MediaOverviewPanel.vue` 与 `zlm/workbench/chart/overviewChart.ts` 各写了一份「state + status 合成文案」，**分支顺序相反、文案也不同** ⇒ 收进 `mediaNodeStateRuntimeText`。
⭐ **文案口径统一**：`active` 此前被写成 **活跃 / 可用 / 在线** 三种，现统一为 **在线**；`maintenance` 的 **维护 / 维护中** 统一为 **维护中**。
⛔ 同时拆掉一个**「拿中文串反判逻辑」**的耦合：`overviewChart` 原本 `if (status === "状态未知")` 用合成出来的中文串做判断 ⇒ 现在判定一律走 `mediaNodeRuntimeKey`。

**F18 级联注册状态（1 处）**：`cascade/cascadeState.ts` —— 先算 `cascadeRegisterStateKey`（6 个派生键），label 走字典，**色与说明（detail）留代码**（不是字典值域）。

**⚠️ 本批唯一的语义变更（收敛的必然代价，需知情）**

`overviewChart` 原把「采集失败」排在「离线」之前，`MediaOverviewPanel` 反之。收敛取**生命周期优先**（离线胜过采集失败），连带的可见变化：

1. `state=offline && status=unavailable` 的节点：图里 `statusText` **采集失败 → 离线**（与卡片口径一致）。
2. 该节点健康矩阵「状态」维度值 **null（未知）→ 0（最差）** —— 离线是**已知事实**，不该算未知。
3. 该节点其余三个维度的 label 从统一的「采集失败」变为各自具体的 不可用 / 不完整（只在 `state` 正常但采集失败时才是「采集失败」）。

依据：`overviewChart.test.ts` 的两条断言已按新口径更新（原断言是旧实现的产物，非产品契约）；「采集失败」标签并未失效，`state` 正常而探针失败时仍会出现。

**验证**

- 本批相关 **118 例全绿**（`deviceStatus` 6 + `mediaNodeState` 8 + `cascadeState` 13 + `overviewChart` + `workbenchCapacity` + `ClusterOverview` + 设备/多屏相关）。
- 开发库：**19 字典 / 111 项**（⚠️ 见 §四·4.1「别用 max(id) 当项数」）；接口 `sysDictItem/getByDictCode/{device_status,media_node_state,cascade_register_state}` 实测逐条核对一致。
- `eslint` 零告警；`vue-tsc` 本批改动文件**零错误**（存量 12 条集中在 `firmware-repo/*` 与 `DeviceFirmwareUpgradePanel.*`）。

**提交状态**：✅ 已提交 `6d730d2c feat(dict): 设备/节点/级联状态字典化并收敛重复实现（P2）`（27 个文件，+586/−111）。
⛔ **数据库侧零改动**（新字典直插开发库，仓库里没有任何 SQL/seeds 痕迹）。

---

### 二·P3a 明细（✅ 已完成 2026-10-05）

**新增 6 个字典**（⛔ 直接写开发库 220，不写任何 SQL 脚本）：

| 字典 code | 值域（= 协议值域，锁进单测） | 开发库 id / 项数 |
| --- | --- | --- |
| `channel_room_type` | 1 室外 / 2 室内 | 20 / 2 |
| `channel_supply_light_type` | 1 无补光 / 2 红外补光 / 3 白光补光 / 4 激光补光（2022 新增）/ 9 其他（2022 新增） | 21 / 5 |
| `channel_direction_type` | 1 东 … 8 西北 | 22 / 8 |
| `channel_position_type` | 1 省际检查站 … 10 交通干线（**2016 独有**） | 23 / 10 |
| `channel_use_type` | 1 治安 / 2 交通 / 3 重点（**2016 独有**） | 24 / 3 |
| `channel_photoelectric_imaging_type` | 1 可见光成像 / 2 热成像 / 3 雷达成像 / 4 X光成像 / 5 深度光场成像 / 9 其他（**2022 独有，多值**） | 25 / 6 |

**改动**：`device-mgmt/channelAttributeText.ts`（6 张数字键表 → 冻结的协议值域兜底常量 + 可注入查表）、
新增注入层 `device-mgmt/useChannelAttributeDict.ts`、唯一消费点 `device-mgmt/index.vue`。

⭐ **本批刻意"不做"的三件事**（都写进了代码注释）：

1. ⛔ `capturePositionType`（采集部位类型）**不加表、不入字典** —— 标准只要求"应符合附录 O"，无权威中文名。
2. ⛔ **`0` / `""` = "本次未上报"这条哨兵语义留在纯函数**（字典表只有"所属字典/值/名字"三位，表达不了"缺失"）。
   单测专门钉死：**即便字典里偏偏有条 `value="0"`，也必须显示"未上报"**。
3. ⛔ **多值拆分/拼回留在纯函数**（`photoelectricImagingType` 允许 `1/2/3`），字典只管"单码 → 中文"。

✅ **判定零改动**：`catalogShapeFromAttributes` 本来就认码值，字典改名不影响它。单测用"把名字全改掉后
`key/label/scope/reported` 逐条不变、只有 `value` 变"来同时守住"字典生效"与"判定不受影响"两件事。

**验证**

- `channelAttributeText.test.ts` **48 例全绿**（原 34 例 + 新增 14 例字典化用例）。
- `device-mgmt` 全目录：**431 例通过，2 例失败**，两个都是**已知历史遗留**（见 §四·4.2 红名单）。
- 开发库：**25 字典 / 145 项**（⚠️ 项数用 `COUNT(*)`，见 §四·4.1）；无重复行、无父 id 落空的孤儿项。
- 接口 `sysDictItem/getByDictCode/<6 个 code>` **逐条与代码兜底比对一致**（脚本化对拍，6/6 ✓）。
- `eslint` 零告警；`vue-tsc` 本批文件**零错误**（存量 108 条，全在 `firmware-repo/*` 与 `DeviceFirmwareUpgradePanel.*`）。

**⚠️ 知情项（老板 2026-10-05 提醒）**：这六项**真实设备极少上报**，本批价值在"枚举口径统一"，
不在覆盖面 —— 别指望它在现场能常看到值。

**提交状态**：✅ 已提交 `c7a9fee0`（5 个文件：3 前端 + 1 测试 + 本台账）。
⛔ **数据库侧零改动**（新字典直插开发库，仓库里没有任何 SQL/seeds 痕迹）。

### 二·P4a 明细（✅ 已完成 2026-10-05）

**范围**：系统类三件套 —— F22 任务执行策略 / F23 任务阻塞策略 / F24 登录失败原因。

| 落点 | 说明 |
| --- | --- |
| `sysjobs/jobPolicy.ts`（纯函数） | `DICT_CODE_*` + `*_LABEL_FALLBACK`（值域唯一真源，**键序 = 下拉序**）+ `jobPolicyLabelsFrom` / `jobPolicyOptionsFromLabels` / `jobPolicyLabelFrom` |
| `sysjobs/useJobPolicyDict.ts`（注入层） | `useJobExecutePolicy()` / `useJobBlockingPolicy()` |
| `login-log/loginFailureReason.ts`（纯函数） | 同上一组导出（key 为字符串） |
| `login-log/useLoginFailureReasonDict.ts` | `useLoginFailureReason()` |
| `sysjobslist.vue` | 两处 `policyMap` + 两处模板硬编码 `<a-option>` ⇒ 全走字典选项；`formatExecutionPolicy/BlockingPolicy` 改为委托 |
| `login-log/index.vue` | `reasonLabels` 常量 ⇒ 组合式；筛选下拉与 `failureLabel` 都走字典 |

⭐ **本批首次采用「白名单式」字典合并**（与 P1/P2 的「字典驱动」不同）：
字典项只按 `value` **改名** —— `value` 不在代码 `fallback` 里的**直接丢弃**、`fallback` 里缺的**自动补齐**
⇒ **档位与顺序永远由代码锁定**。理由：这三项的值都要提交给后端（int 落库 / 后端白名单），
多一档少一档都是契约破坏。单测正反两面都钉了（多给的档位被丢弃 / 缺的档位补回兜底）。

✅ **核实：不存在"两个真源"**。盘点报告 B6 提示 `schedulerhelper/job.go` 有中文与 F22/F23 同源，
但实测那两个 `getXxxName()` 是**包私有**、调用方只有日志（`logger.go` / `zap_logger.go`），
**API 返回的是 int** ⇒ 前端本地译就是对的口径，不需要像告警那样先搬翻译层。

⭐ **顺带补 F25 漏项**：`sysjobslist.vue` 的任务状态筛选当时没接 `status` 字典（P0 漏了）——
它就在执行策略筛选旁边，不补会出现"一格里两种写法"。其 `a-switch` 的 `checked-text/unchecked-text`
**保持硬编码**（属控件交互文案，不在字典化范围，见 §7）。

**验证**：`jobPolicy.test.ts` 8 例 + `loginFailureReason.test.ts` 6 例 + 同目录既有用例，合计 **36 例全绿**；
字典目录 14 例（只读名单已扩到 **14 个 code**）；开发库 **28 字典 / 156 项**（无重复行、无父 id 落空）；
接口 `getByDictCode/*` 三个 code **逐条对拍一致（3/3 ✓）**；`eslint` 零告警。
⚠️ `vue-tsc` 仍按「只看本批文件」验（存量 108 条，全在 `firmware-repo/*` 等）。

### 二·P4b 明细（✅ 已完成 2026-10-05）

**范围**：设备配置族里两个「值是**下发报文码值**」的枚举 —— F13 画面镜像（`FrameMirror`）/ F14 码流编号
（`VideoRecordPlan.streamNumber`）。**F12 OSD 时间格式按 §3.5-D 跳过**（界面渲染 `sample` 展开实例、不显示 `label`）。

| 落点 | 说明 |
| --- | --- |
| `device-mgmt/deviceConfigDict.ts`（纯函数，新） | `DICT_CODE_FRAME_MIRROR` / `DICT_CODE_STREAM_NUMBER` + 两张冻结兜底 + `whitelistedLabel` / `applyOptionLabels`（白名单式合并）/ `streamNumberLabelFrom` |
| `device-mgmt/useDeviceConfigDict.ts`（注入层，新） | `useDeviceConfigDictLabels()` / `useFrameMirrorOptions()` / `useStreamNumberOptions()` / `useStreamNumberLabel()` |
| `deviceConfigGroups.ts` | `ConfigSelectField` / `ConfigMirrorField` 加 `dictCode?`；3 处字段声明 `dictCode`（mirror ×1 + streamNumber ×2） |
| `DeviceConfigDrawer.vue` | `RenderField` 带 `dictCode`；`activeFields` 按 code 做白名单式改名（**槽位/顺序/shortLabel 仍由 `MIRROR_OPTIONS` 锁死**） |
| `components/PlayConsoleLinked.vue` | ② 画面镜像卡的 `:options` 由 `MIRROR_OPTIONS` ⇒ `useFrameMirrorOptions()` |
| `components/play-console/PictureVideoParamCard.vue` | `streamLabel` 由过程式 `num===0?"主码流":\`子码流 ${num}\`` ⇒ `useStreamNumberLabel()`（**第三处**独立实现，本批收敛） |
| `systemDictCodes.ts` | 新增分组 `DEVICE_DOWNLINK_DICT_CODES`（2 个），名单 14 → **16** |

⛔⛔ **本批是本主题风险最高的一批**：这两个值会被 `requiredInt` 收窄后**原样写进发给设备的 XML**。
字典**多一档** ⇒ 设备收到不认识的档位（静默忽略 / 收窄失败）；**少一档** ⇒ 永远选不出来、下发不出去。
⇒ 一律【白名单式合并】：字典**只能改名**，槽位集合与顺序恒由代码锁死（同 P4a 口径，见技能 §5.3）。
⇒ 也因此**必须进「前端只读」名单**（新增的 `DEVICE_DOWNLINK_DICT_CODES` 组）。

⭐ **顺手收敛的第三处重复**：`PictureVideoParamCard.vue` 里 `主码流 / 子码流 N` 是**第三份**独立实现
（另两份是 `STREAM_NUMBER_OPTIONS` 与 `streamNumberLabelFrom`）。但它按设备**实际上报**的码流数逐行取名、
编号可能 > 3，所以合并时保留**过程式兜底**：0–3 走字典、超出回落 `子码流 N`。

**验证**：`deviceConfigDict.test.ts` **13 例**（白名单四个方向：改名生效 / 多给档位丢弃 / 缺的补回 / 不修改入参）
+ `systemDictCodes.test.ts` 6 例；`PlayConsoleLinked.test.ts` **201 例**、`DeviceConfigDrawer.test.ts` 42 例、
`deviceConfigPayload.test.ts` + play-console 相关合计 **92 例** 全绿；
`device-mgmt` 全目录 **448 通过 / 2 失败** —— 两个失败经 `git stash` 对照证明**逐字相同的历史遗留**
（`DeviceFirmwareUpgradePanel` / `index.deviceDetailTabs`，见技能 §10-2b），与本批无关；
开发库 **30 字典 / 164 项**（两个新字典各 4 项，无重复行、无父 id 落空，幂等复跑不增行）；
接口 `getByDictCode/frame_mirror|stream_number` **逐条对拍一致**；`eslint` 零告警、`prettier` 干净。

⚠️ **提交时按 hunk 拆分**：`PlayConsoleLinked.vue` 里混着**他人在建的对讲频谱改动**（`talkSpectrum` / `AudioLevelSnapshot`），
只 stage 本批的 3 个 hunk（0/2/5），已用索引断言核对（`talkSpectrum` 在暂存版本里出现 0 次）。

---

## 三、待拍板（阻塞 P3 及之后）

### 3.1 ⭐⭐ 已决：开发阶段**不写任何数据库脚本**（2026-10-05 口径变更）

**背景**：P1 按旧流程改了 `seeds/*.jsonl` → 重生成三方言 → 另写一条幂等迁移脚本补开发库。老板指出：**开发阶段字典数据直接补开发库就行，不用 SQL 脚本。**

**新口径（自 2026-10-05 起，P2/P3/P4 一律照此）**：

| 动作 | 怎么做 |
| --- | --- |
| 新增 / 修改字典 | **直接往开发库 `sys_dict` / `sys_dict_item` 插数据**（库：`192.168.10.220:3306/uvp_gb28181`） |
| ⛔ 不要做 | 不改 `seeds/*.jsonl`、不跑 `generate_sql.py`、不写 `migrations/*.sql` |
| 为什么 | 发布阶段会根据**开发库**整理全量脚本 → 打版本基线；之后升级才用增量脚本 |

**P1 的处置**：`seeds` 2 个文件 + 三方言 3 个脚本 + 迁移脚本 1 个 —— **全部已撤回**，仓库里零 SQL 痕迹（`generate_sql.py --check` 已复绿，111 表 / 5148 种子行一致）；开发库数据**保留**（`video_format` 5 项 / `video_resolution` 6 项 / `bit_rate_type` 2 项）。

**连带影响**：`useVideoParamDict.test.ts` 里"兜底 == 种子逐字比对"的锚点随之失效，已改为**协议值域正向锁定**（改兜底必然红）。
⚠️ 旧的"基线漂移"议题（提交的 seeds 与三方言不同步）**随本次口径变更自然消解** —— 不再维护 seeds，就无所谓漂移。

### 3.2 ✅ 已定并已实施：字段型字典「前端只读」（2026-10-05 选方案 B）

`ptz_type`、`video_*`、`channel_*` 的值域由 GB/T 28181 或代码固定。字典管理页原先**允许任意增删改** ——
现场改坏会让那一档**选不出来 / 下发不出去**（界面里没有这个选项了）。

**老板 2026-10-05 拍板：走方案 B（前端只读，零改表，后端不拦）。**

实现落点：

| 落点 | 说明 |
| --- | --- |
| `web/src/views/system/dictionary/systemDictCodes.ts` | **唯一真源**：`SYSTEM_DICT_CODES`（**16 个**，随批次增长）+ `isSystemDict(code)`；入名单判据写在文件头注释。分三组：`GB_PROTOCOL_DICT_CODES` / **`DEVICE_DOWNLINK_DICT_CODES`** / `PLATFORM_PROTOCOL_DICT_CODES` + `PLATFORM_ENUM_DICT_CODES` |
| `dictionary.vue` | 外层字典「修改/删除」、详情弹窗「新增/逐项改删」按 code 置灰；⛔ 删除的**确认气泡也要 `:disabled`**（否则点链接会弹出一个"点了也没用"的气泡 —— Arco 的 Trigger 监听在包裹层，链接禁用拦不住它）；每个改动入口另加函数守卫做双保险；编码列加锁标 + 详情弹窗顶部说明条 |
| `uvp-ui-language.scss` | 新增 `.uvp-table-action.arco-link-disabled` 置灰规则 —— ⛔ **必须 3 类选择器**，否则会被各 tone 的 2 类规则按加载顺序盖回彩色；深色主题另带 `body[arco-theme="dark"]` 前缀（那些 tone 有前缀，权重更高） |

**名单口径（16 个）**：
- GB 协议值域（10）：`ptz_type` / `video_format` / `video_resolution` / `bit_rate_type` /
  `channel_room_type` / `channel_supply_light_type` / `channel_direction_type` / `channel_position_type` /
  `channel_use_type` / `channel_photoelectric_imaging_type`
- ⛔⛔ **下发报文码值（2，P4b 新增，后果最重）**：`frame_mirror` / `stream_number`
- 平台协议值域（1）：`gb28181_playback_protocol`
- 平台内部枚举（3，P4a 新增）：`job_execute_policy` / `job_blocking_policy` / `login_failure_reason`

⛔ **刻意排除**：`post`（业务字典，管理员本就该增补）；`gender` / `status` / `taskStatus` / `playback_*` /
`device_status` / `media_node_state` / `cascade_register_state`（纯展示，且判定走派生键，改名无害）。

⛔ **方案 A（真·系统级标记）为什么不走**：原想复用 `created_by = 1`，但**实测 25 个字典的 `created_by` 全是 1**，
无法作判据 ⇒ 真拦必须给 `sys_dict` 加 `is_system` 列（属 schema 改动：`schema.ir.json` + `seeds/` →
`generate_sql.py` → 手工迁移）。老板选择暂不动表结构。

**测试**：`systemDictCodes.test.ts`（名单/判据锁定）+ `dictionary.readonlyGate.test.ts`（源码断言：5 个改动入口都有守卫）
+ `dictionary.readonlyGate.trigger.test.ts`（**真渲染**：禁用时气泡不弹、启用时会弹 → 反证判据不是恒假的假绿）。

### 3.3 ✅ 已决策：告警（B1–B3）翻译层 —— 走前端　（⏸ 整批暂缓，见 §3.4C）

**2026-10-05 拍板：后端不做字典。** 字典只做前端展示翻译；后端只在"输出不经浏览器"的场景（对外 OpenAPI、短信/邮件通知正文、导出文件）才译。

⏸ **2026-10-05 追加：整批 P3b 暂时跳过**（老板原话"那就暂时跳过告警"）—— 翻译层方向定了，但落地还卡在 §3.4B 的三个前置，故不改后端、不动前端，原地挂起。

⚠️ 但告警现由**后端** `alarm.go` 返回带中文 `label` 的对象（属**已有的**后端译），前端直接展示 —— 要收归前端就得改后端 DTO + 前端全部告警消费点，是独立改造，不在 P1/P2 范围内。

原方案 B（后端读字典后返回 `label`）**已作废**：会让后端被字典数据耦合，且需新建 service + 缓存。

⛔ 另注：`alarm_type` 是**按报警方式（method 2/5/6）分组的嵌套 map**（共 20 项），做成字典必须带 `method` 维度，不能拍平成一个 code。

### 3.4 P3 侦察结论（2026-10-05，动手前先看，避免重做）

**A. 通道属性（6 个）—— 可直接做，风险低**

- 事实：6 项全是**设备上报什么就显示什么**（来源 `catalog` 目录应答落 `gb_channel` 属性列），**平台侧没有任何让用户选它的入口，也没有下发路径** ⇒ 改坏的最坏结果只是那条显示 `未知(N)`。
- 唯一消费点：`device-mgmt/index.vue`（+ `channelAttributeText.ts` 本体与单测）。改动面很小。
- ⚠️ 口径校正：P3 的「`channel_*` ×6」= `channelAttributeText.ts` 里现有的 **6 张表**（见下方第 2 条）。同面板还有一个字段 `capturePositionType`（采集部位类型）**本来就没有表**、也不该加 —— 代码里**刻意不硬编码中文名**（标准只说"应符合附录 O"），没有文案可搬进字典。
- ⛔ **两个语义必须留在代码里，不能进字典**：
  1. `0` / `""` = "设备本次未上报"（哨兵，不是"值为 0"）—— 字典表达不了这层含义，须在纯函数里**先判再查表**；
  2. `photoelectricImagingType` 是**多值**（标准允许 `1/2/3` 斜杠分隔）—— 字典只管"单码→中文"，拆段/拼回留在纯函数。
- ✅ 形态判断逻辑（`catalogShapeFromAttributes` 推 2016/2022 形态）**已经认码值**，不受字典改名影响，无需改动。

**B. 告警（3 个）—— ⏸ 暂缓（2026-10-05 老板拍板"暂时跳过"）**

- ⛔ **后端已经在翻译**：`controllers/alarm.go` 硬编码三张表 —— `alarmPriority`(4) / `alarmMethod`(7) / `alarmType(method,value)`(20)，接口以 `alarmEnumValue{value,label}` 下发，前端**直接显示 `label`**。此时前端再加字典 = **两个真源**（要么字典成死代码，要么后端 label 变废负载）⇒ **先定"谁翻译"**。
- ⛔⛔ **同一值域已写两遍**（已逐项机检）：后端 `alarmType()` 20 项 与 前端 `alarm-management/alarmState.ts` 的 `ALARM_TYPE_OPTIONS` 20 项 **逐项完全一致**（三组 5/13/2，含「图像遮挡报警（2022）」的括号）。属 P1 那类"同一值域多处定义"⇒ **先收敛再字典化**。
- ⛔ `alarm_type` 是 **method × type 二维**，而 `sys_dict_item` 只有 (dict_id, value, name) 三位 ⇒ 要么造拼接值（如 `2-1`，我方发明的编码），要么拆 3 个字典。**这是待拍板的设计题。**
- ⭐ **范围比原先估计的小**：真正消费 label 的只有 `alarm-management/components/AlarmDetailDrawer.vue`（priority/method/alarmType 3 个字段）与 `alarm-management/index.vue`（筛选下拉）。`home/dashboardState.ts` 用的是原始描述文本，**不受影响**。

**C. 建议顺序**（含 §3.2 的依赖）

1. ✅ **§3.2「字段型字典只读闸门」—— 已拍板并已实施（方案 B 前端只读）**，见 §3.2 正文；
2. ✅ **P3a 通道属性 6 个字典 —— 已完成**（见 §二·P3a）；
3. ⏸ **P3b 告警 —— 2026-10-05 老板决定暂时跳过**（原话"那就暂时跳过告警"）。⛔ 重启前置三条**缺一不可**：
   ① 先定"谁翻译"（§3.3 已倾向归前端，但落地要动后端 DTO）；② 收敛那 20 项前后端重复值域；③ 拍板
   `alarm_type` 的 method×type 二维怎么落（拼接值 vs 拆 3 字典）。**单独一批**做，不与 P3a 混。

### 3.5 P4 侦察结论（2026-10-05，动手前先看，避免重做）

对 F12–F16 / F19 / F21–F24 / F26–F32 逐项摸过「定义点 / 值域 / 消费点 / 是否下发」。分六类：

**A. 纯展示 · 低风险**（改字典只影响文案）：F15 坐标来源、F19 多屏可播性、F21 会话类型、F27 能力说明、
F28 看守位能力三态、F29 维护操作结果、F30 回读新鲜度、F31 云录制持有态、F32 探针诊断结论。
→ 这些**不进只读名单**（判定走码值/派生键，改名无害）。

**B. 会被写进下发报文（高风险，字典上线后必须进只读名单）**：**F13 画面镜像**、**F14 码流编号**
（经 `requiredInt` 收窄后写 `FrameMirror` / `VideoRecordPlan.streamNumber` 下发设备）。
字典只能改 label，**value 绝不能动**。→ ✅ **P4b 已做**（`frame_mirror` / `stream_number`，
走白名单式合并 + 进 `DEVICE_DOWNLINK_DICT_CODES` 只读组）。

**C. 值是后端/落库契约（不是设备报文，但同样不可增删档）**：F16（筛选参数 `source=`）、F22/F23（int 落库）、
F24（**后端 switch 白名单**）、F26（落库 + DB `CHECK (3,4)`）。→ 一并进只读名单。

**D. ⛔ 不建议字典化的**：
- **F12 OSD 时间格式** —— 界面渲染的是 `sample` 模板**展开成当前时刻**的实例（`DeviceConfigOsdBlocks.vue:103`），
  下拉里**根本不显示 `label`**；字典只能改对账/无障碍用的那串，**价值极低、风险不小**（`sample` 是逻辑输入）。
  ⇒ 建议**跳过**，并在盘点表里标注理由。
- **F27 能力说明** = 11 条**句子**（不是枚举名），且 scope 值域由后端下发、前端另有 `OPENAPI_CLIENT_SCOPES` 一份
  ⇒ 与 §3.4 的长句状态同类，不并入字典。

**E. ⛔ 有"文案/键被当逻辑输入"，改造时必须先拆**：
- **F16**：`SOURCE_LABELS` 的 key 被 `raw in SOURCE_LABELS` 当**校验白名单**（`snapshotLibraryState.ts:59`）；
- **F32**：`ISSUE_META` 的 code 被用于 severity 判定与 verdict 决策（`probeDiagnosis.ts:118,136-163`）；
- **F19**：`slot.availability` 原值直接拼 CSS class（`PlaybackSchemePanel.vue:291`）；
- **F21**：测试直接断言源码里的中文串（`MediaRuntimeLedgerDialog.test.ts:232`）⇒ 改字典必须同步改测试；
- **F24**：`reasonLabels` 的 key **既是筛选选项值又是后端白名单**，只许改名、不许增删 key。

**F. ⛔ F29 的 4 份实现值域并**不相同**（重启 9 / 固件 7 / 格式化 9 / 维护记录 9）—— 不是"同一值域写 4 遍"，
而是**四个有交集的集合** ⇒ 属设计题，先定"并成一个字典还是各自一个"再动手。

**F28 口径订正**：盘点报告写的 F28「订阅能力」，实际是 **`PlayConsoleLinked` 的看守位能力三态**
（`supported/unsupported/unknown`）；该文件全文没有"订阅"三态。按看守位处理。

**⭐ 顺手发现（F25 漏项）**：P0 声称 `status` 字典覆盖 9 处，实际 `views/system/menu/menu.vue` 与
`views/system/sysjobs/sysjobslist.vue` **仍是硬编码**（全仓只有 7 个文件 import 了 `useStatusLabel`）。
P4a 已补 `sysjobslist`（同一行就是本次要改的执行策略筛选，不补会"一格里两种写法"）；`menu.vue` 待确认是否还需要。

---

## 四、提交注意

1. **工作区在 2026-10-05 已清空**（原 722 个在途文件随 `e1c67298` 提交）。今后**只 stage 本次相关文件**，别把无关改动卷进来。
2. 本仓约定：同一文件里的无关 hunk 要**按 hunk 分组 stage**（`git apply --cached`），⛔ 直接 `git commit <path>` 会把整文件的工作区版本一起带走。
3. 推 `develop` 前**必须先问**。
4. ⭐ 常年挂着一个**与本主题无关的在途改动** `device-mgmt/DeviceControlPanel.vue` + `.test.ts`（控制面板 UI 打磨）—— 连续几批都**别 stage 它**。

### 4.1 ⛔⛔ 别用 `max(id)` 当「项数」（2026-10-05 抓到的记录错误）

**踩坑**：本文档与技能/记忆里一度写着「16 字典 / **135** 项」「19 字典 / **146** 项」——
那两个数是 **`MAX(id)`**，不是行数。真实行数是 **100**（P1 后）/ **111**（P2 后）。

```
raw_total = 111,  max_id = 146      # 差 35
```

原因：开发库 id **稀疏**（改名/删除留下的空洞），`AUTO_INCREMENT` 不回填 ⇒ `max(id)` 恒 ≥ 行数。
同一天更早的记录其实写对过（「items 只 **87 行**、max id 已是 **122**」），后面几次却把 max id 当成了项数，
且**连续两批各错一次、错法一模一样** ⇒ 一旦写成结论就会被到处抄。

**正确口径**：

```sql
SELECT COUNT(*) FROM sys_dict;         -- 字典个数
SELECT COUNT(*) FROM sys_dict_item;    -- 字典项个数
-- 验证新字典：按 code 查，别按 id 猜
SELECT d.code, d.name, COUNT(i.id) items FROM sys_dict d
LEFT JOIN sys_dict_item i ON i.dict_id = d.id
WHERE d.code IN ('device_status','media_node_state','cascade_register_state')
GROUP BY d.id, d.code, d.name;
```

⛔ 表 `sys_dict_item` **没有 `deleted_at` 列**（不是软删除表），别去按它过滤。
⭐ 通用教训：**「条数」与「最大 id」是两个量；稀疏 id 表上永远用 `COUNT(*)`，并且把口径写进结论里。**

### 4.2 存量失败红名单（批次前先看，别把历史遗留算到本批头上）

全量/目录跑完若仍红，**必须"摘掉本批改动重跑"来证明它是历史遗留**，不要凭记忆断言：

```bash
git stash push -m "verify" -- $(git diff --name-only)   # 只摘已跟踪改动，未跟踪新文件不碍事
# 原样重跑同一批失败用例 —— Test Files / Tests 两行数字必须与摘之前逐字相同
git stash pop                                            # ⛔ 确认修改文件全回来了
```

⛔ 别用 `git checkout --` 代替 stash。⛔ 别只凭"报错内容像不像本批"，要**比对数字**。

**已知红名单（会变，每次实测）**：

| 用例 | 症状 | 性质 |
| --- | --- | --- |
| `device-mgmt/DeviceFirmwareUpgradePanel.test.ts` | emits 断言 | 历史遗留 |
| `device-mgmt/index.deviceDetailTabs.test.ts` | 第 132 行源码断言 `toContain(".dcg-window--embedded .dcg-nav > .dcg-nav-item {")` —— 该选择器**在 HEAD 里也不存在** | **过期断言**（断言没跟上代码） |
| `layout/.../theme-toggle.test.ts` | `arco-overrides.scss` 的暗色按钮源码断言 | 历史遗留 |
| `vue-tsc` 存量 **108** 条 | 全在 `firmware-repo/*` 与 `DeviceFirmwareUpgradePanel.*` | 历史遗留 |

⭐ 判断"是不是过期断言"的快速办法：若失败断言读的恰好是本批改过的文件，先
`git show HEAD:<file> | grep <断言串>` —— **两边都没有 ⇒ 与本次无关**。

---

## 五、每批的固定动作（清单）

1. 视需要收敛重复实现 → 单点导出
2. **新增字典：直接往开发库 `sys_dict` / `sys_dict_item` 插数据** —— ⛔ 不写 SQL 脚本、不改 seeds、不重生成三方言（见 §三·3.1）
3. 前端接 `useDictOptions(code, FALLBACK)`，兜底常量按**协议值域**写死
4. 加防回归锚点（纯函数单测 + source-assert rollout 测试）
5. `npx vitest run <相关>` + `npx vue-tsc --noEmit` + `npx eslint <改动文件>`
6. ⚠️ 改了消费组件 ⇒ **先列出「所有 mount 它的测试文件」跑一遍**，再跑全量
7. 收尾：开发库侧核对（新字典是否已在库里、界面是否可见）

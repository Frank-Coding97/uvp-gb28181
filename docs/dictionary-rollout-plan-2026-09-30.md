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
| **P3** | 通道属性 + 告警 | `channelAttributeText.ts`、`alarm.go` | `channel_*` ×6、`alarm_priority`、`alarm_method`、`alarm_type` | ⏳ 待拍板 |
| **P4** | 其余 20 项 | 见盘点报告 F12–F32、B4–B10 | 按项 | ⏳ 待开始 |

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
- 开发库：**19 字典 / 146 项**；接口 `sysDictItem/getByDictCode/{device_status,media_node_state,cascade_register_state}` 实测逐条核对一致。
- `eslint` 零告警；`vue-tsc` 本批改动文件**零错误**（存量 12 条集中在 `firmware-repo/*` 与 `DeviceFirmwareUpgradePanel.*`）。

**提交状态**：✅ 已提交 `6d730d2c feat(dict): 设备/节点/级联状态字典化并收敛重复实现（P2）`（27 个文件，+586/−111）。
⛔ **数据库侧零改动**（新字典直插开发库，仓库里没有任何 SQL/seeds 痕迹）。

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

### 3.2 字段型字典是否标「系统级只读」

`ptz_type`、`channel_*`、`alarm_*` 的值域由 GB/T 28181 固定。字典管理页现在**允许任意增删改**这些字典项。现场改坏值域会导致解析 / 对账**静默错位**（如把 `PTZType=1` 的文案改掉，界面就认不出球机）。

⚠️ **2026-10-05 P1 又新增了三个字段型字典**（`video_format` / `video_resolution` / `bit_rate_type`），这条待拍板的紧迫性上升了。

P1 已把**判定**与展示解耦（对账改认码值，见 §二·P1），所以现场改文案**不再影响**「回读 vs 实测」的结论；但**下拉选项**仍会被改坏 —— 字典项被停用/删掉，那一档就再也发不出去（界面里没有这个选项了）。

**待选方案**

| 方案 | 做法 |
| --- | --- |
| A（推荐） | 给字典加「系统级」标记（可复用 `created_by = 1` 或加 `is_system` 列），管理页对系统级**禁改禁删** |
| B | 只在前端管理页对这批 code 做只读，后端不拦 |
| C | 不设限，靠文档和规范约束 |

### 3.3 ✅ 已决策：告警（B1–B3）翻译层 —— 走前端

**2026-10-05 拍板：后端不做字典。** 字典只做前端展示翻译；后端只在"输出不经浏览器"的场景（对外 OpenAPI、短信/邮件通知正文、导出文件）才译。

⚠️ 但告警现由**后端** `alarm.go` 返回带中文 `label` 的对象（属**已有的**后端译），前端直接展示 —— 要收归前端就得改后端 DTO + 前端全部告警消费点，是独立改造，不在 P1/P2 范围内。

原方案 B（后端读字典后返回 `label`）**已作废**：会让后端被字典数据耦合，且需新建 service + 缓存。

⛔ 另注：`alarm_type` 是**按报警方式（method 2/5/6）分组的嵌套 map**（共 20 项），做成字典必须带 `method` 维度，不能拍平成一个 code。

---

## 四、提交注意

1. **工作区在 2026-10-05 已清空**（原 722 个在途文件随 `e1c67298` 提交）。今后**只 stage 本次相关文件**，别把无关改动卷进来。
2. 本仓约定：同一文件里的无关 hunk 要**按 hunk 分组 stage**（`git apply --cached`），⛔ 直接 `git commit <path>` 会把整文件的工作区版本一起带走。
3. 推 `develop` 前**必须先问**。

---

## 五、每批的固定动作（清单）

1. 视需要收敛重复实现 → 单点导出
2. **新增字典：直接往开发库 `sys_dict` / `sys_dict_item` 插数据** —— ⛔ 不写 SQL 脚本、不改 seeds、不重生成三方言（见 §三·3.1）
3. 前端接 `useDictOptions(code, FALLBACK)`，兜底常量按**协议值域**写死
4. 加防回归锚点（纯函数单测 + source-assert rollout 测试）
5. `npx vitest run <相关>` + `npx vue-tsc --noEmit` + `npx eslint <改动文件>`
6. ⚠️ 改了消费组件 ⇒ **先列出「所有 mount 它的测试文件」跑一遍**，再跑全量
7. 收尾：开发库侧核对（新字典是否已在库里、界面是否可见）

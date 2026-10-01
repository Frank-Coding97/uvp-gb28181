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
| 3 | **字典缺失要兜底** | 一律 `useDictOptions/useDictLabel(code, FALLBACK_*)`。兜底常量必须与种子逐字对齐，且有单测钉住（见 §一）。 |
| 4 | **值域外原样回显** | `useDictLabel` 未命中时返回原值（`未知(7)` 这类排障信息要看得见），不吞成空。 |
| 5 | **只动数据翻译** | `a-switch` 的 `#checked`/`#unchecked` 槽位、`是否禁用` 这类布尔是/否、校验白名单、协议原始码 —— **都不动**。 |
| 6 | **新增字典走 seeds** | 改 `server/resource/database/baseline/seeds/sys_dict.jsonl` + `sys_dict_item.jsonl`，重生成三方言。⛔ 不写增量迁移（已退役）。 |
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
| `DICT_CODE_STATUS` / `STATUS_LABEL_FALLBACK` / `useStatusLabel()` | 通用启用/禁用字典（种子内置） |

**测试**：`useDictOptions.test.ts`（11 例）。含「字典改名后界面必须跟着变」这条分水岭用例。

---

## 二、批次台账

| 批次 | 范围 | 涉及文件 | 新增字典 code | 状态 |
| --- | --- | --- | --- | --- |
| **P0** | 启用/禁用展示层 | 7 个文件（见 §二·P0） | 无（复用 `status`） | ✅ **已完成** |
| **P1** | 视频参数三码值 | `videoParamCodec.ts`、`DeviceConfigDrawer.vue`、`PictureVideoParamCard.vue` | `video_format`、`video_resolution`、`bit_rate_type` | ⏳ 待开始 |
| **P2** | 设备/节点/级联状态 | `device-mgmt/index.vue` 等 17 处 + `zlm/**` 6 处 + `cascadeState.ts` | `device_status`、`media_node_state`、`cascade_register_state` | ⏳ 待开始 |
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

## 三、待拍板（阻塞 P1 及之后）

### 3.1 ⚠️ 基线漂移：入库时就存在，需决定怎么处理

**现状**：`generate_sql.py --check` **当前是红的**，但 `server/resource/database/` 在工作区**完全干净** —— 说明**提交进去的 seeds 与三方言脚本本来就不同步**。

**实测差异**：三个 SQL 各 ~13 行，**纯粹是 INSERT 分批边界漂移**（如 `sys_api` 的 349/350 被切进不同 chunk），行内容逐字一致。即：有人改了 seeds 没重生成。

**影响**：P1 起要新增字典就必须动 seeds ⇒ 重生成会把这份存量漂移一起带进 diff。

**待选方案**

| 方案 | 做法 | 代价 |
| --- | --- | --- |
| A（推荐） | 重生成三方言，**顺带把 `--check` 修绿** | diff 里混入 ~39 行无关 churn，但把仓库修对了 |
| B | 只把新增字典行手写进三方言脚本，保留现有漂移 | diff 干净，但 `--check` 仍红，且手写易错 |

### 3.2 字段型字典是否标「系统级只读」

`ptz_type`、`channel_*`、`alarm_*` 的值域由 GB/T 28181 固定。字典管理页现在**允许任意增删改**这些字典项。现场改坏值域会导致解析 / 对账**静默错位**（如把 `PTZType=1` 的文案改掉，界面就认不出球机）。

**待选方案**

| 方案 | 做法 |
| --- | --- |
| A（推荐） | 给字典加「系统级」标记（可复用 `created_by = 1` 或加 `is_system` 列），管理页对系统级**禁改禁删** |
| B | 只在前端管理页对这批 code 做只读，后端不拦 |
| C | 不设限，靠文档和规范约束 |

### 3.3 告警（B1–B3）翻译发生在哪一层

告警现在由**后端** `alarm.go` 返回带中文 `label` 的对象，前端直接展示。要字典化有两条路：

| 方案 | 做法 | 影响 |
| --- | --- | --- |
| A（推荐） | 后端只返回码值，前端查字典译 | 与口径 1 一致；需改后端 DTO + 前端全部告警消费点 |
| B | 后端读字典后返回 `label` | 后端被字典数据耦合；但前端零改动 |

⛔ 另注：`alarm_type` 是**按报警方式（method 2/5/6）分组的嵌套 map**（共 20 项），做成字典必须带 `method` 维度，不能拍平成一个 code。

---

## 四、提交注意

1. **工作区已有 722 个未提交文件**（在途的 `s-number-field` 组件统一 + sysdict 修复等），与本次任务无关。**只 stage 本次相关文件，别一起提交。**
2. 本仓约定：同一文件里的无关 hunk 要**按 hunk 分组 stage**（`git apply --cached`），⛔ 直接 `git commit <path>` 会把整文件的工作区版本一起带走。
3. 推 `develop` 前**必须先问**。

---

## 五、每批的固定动作（清单）

1. 视需要收敛重复实现 → 单点导出
2. `seeds/sys_dict.jsonl` + `sys_dict_item.jsonl` 追加（按 §三·3.2 拍板决定是否标系统级）
3. `cd server/resource/database/baseline && python3 generate_sql.py` → 生成三方言
4. `python3 generate_sql.py --check` 必须绿
5. 前端接 `useDictOptions(code, FALLBACK)`，兜底常量与种子逐字对齐
6. 加防回归锚点（纯函数单测 + source-assert rollout 测试）
7. `npx vitest run <相关>` + `npx vue-tsc --noEmit` + `npx eslint <改动文件>`

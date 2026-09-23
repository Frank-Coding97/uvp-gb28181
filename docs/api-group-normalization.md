# 接口管理 API 分组整治

**范围**：`sys_api.api_group`（「系统管理 → 接口管理」页面唯一的分组维度）
**目标库**：192.168.10.220 `uvp_gb28181`

> **五轮记录**（按时间顺序，后一轮修订前一轮）：
> 第 1 轮 §1–§6 —— 编码/前缀/兄弟组清洗，**39 → 30 组**，口径按前端页面菜单；
> 第 2 轮 §7 —— 物理清除软删数据（5 表 32 行）；
> 第 3 轮 **§8 —— 判定键改为「接口路径优先」**，新增 `设备控制` 组，**30 → 31 组**，
> 并补上三方言前向迁移（客户存量库 + 新装库自此口径一致）；
> 第 4 轮 **§9 —— `sys_api.title` 归一化**：从「按钮名 / 机械占位名」改为「接口功能名」，
> **403 行 → 403 个唯一标题**，机械名 0 处；
> 第 5 轮 **§10 —— 第 3 轮那套路径规则的漏网修正**：`stream-probes` ×2 +
> `play/:streamId/monitor` ×1 从「多屏播放」改归「流媒体管理」（**组数不变 31**），
> 并给「工作台页面不得决定分组」补上结构性断言。
> **当前有效口径以 §8/§9/§10 与 `scripts/api_groups.py`、
> `server/app/models/sysapigroup.go`、`button-permissions.json` 为准。**

**结论（当前）**：**403 行在册 API / 31 个受控分组 / 403 个唯一标题**，编码损坏 0 处，
旧分组名归零，机械占位名归零，存量库与新装库口径一致。

---

## 1. 先说两个前提（决定了这次改动是安全的）

| 前提 | 核实方式 | 结论 |
|---|---|---|
| `api_group` **不参与鉴权** | `grep -rn "ApiGroup\|api_group" server/app server/internal server/cmd` → Go 侧只在模型/控制器/迁移种子里出现，casbin 规则里没有任何引用 | 纯展示元数据，改值不影响权限 |
| 迁移记账**无校验和** | `gb_schema_migrations` 只有 `version` + `applied_at` | 改已应用迁移文件的内容不会报错、不会重跑 |

迁移测试也只断言表结构（`CREATE TABLE sys_api(... api_group TEXT ...)`）与菜单/权限接线，
**没有任何测试断言分组值** —— 所以改值不破测试。

---

## 2. 体检结论：五类问题

| # | 问题 | 证据 |
|---|---|---|
| 1 | **分组名与标题双重编码损坏** | `æŒ‰é’®æƒé™ç›®å½•`（=「按钮权限目录」，UTF-8 被按 cp1252 二次解读）2 行；同两行的 `title` 也坏了（`ä¸‹å‘è§†é¢‘å‚æ•°` = 「下发视频参数」） |
| 2 | **前缀不统一** | `GB28181设备管理`（缺空格）与其余 `GB28181 xxx` 不一致；另有 18 个组完全没前缀 |
| 3 | **同模块被拆成兄弟组** | 云端录像拆 4 组（`云端录像`7 + `云端录像下载`3 + `云端录像删除`2 + `云端录像控制`1）；字典拆 2 组（`字典管理`7 / `字典项管理`7） |
| 4 | **口径混杂** | `按钮权限目录`(87)、`游客权限依赖`(2) 不是业务模块，是「这批接口是迁移加的」的记号 |
| 5 | **分组名与内容背离** | `GB28181设备管理`(7) 的 3 个关联菜单其实是 `共享设备 / 分配设备归属 / 设备权限工作台`，与「设备列表」无关 |

### 5.1 根因：三个「写死兜底分组」的源头

这是本次最关键的发现 —— 数据脏不是一次性的，是**持续生成**的：

| 源头 | 位置 | 写死的值 |
|---|---|---|
| 按钮目录生成器 | `scripts/button-catalog.py`（原 `q("按钮权限目录")`） | `按钮权限目录` —— 每加一批按钮就写一批，累计 **87 行 / 跨 36 个页面** |
| 游客权限生成器 | `scripts/guest-permissions.py`（原两处） | `按钮权限目录` + `游客权限依赖` |
| 后续手写迁移 | `2026-09-15-stream-probe-async`、`2026-09-17-storage-card-status`、`2026-09-18-zlm-node-enabled`、`2026-09-18-channel-video-param`、`2026-09-19-device-config-family`、`2026-09-20-channel-snapshot-library`、`2026-09-20-storage-card-format-permission`、`2026-09-21-target-track`、`2026-09-05-guest-readonly-permissions`（各 × 3 方言） | 沿用 `按钮权限目录` 这个「新按钮权限」惯例 |

前端 `sys_api` 编辑页的 `api_group` 是 **自由文本 `a-input`**，后端只校验非空（`validate:"required"`），
所以没有任何一道闸能拦住漂移。

---

## 3. 目标分组清单（31 个，顺序即前端下拉顺序）

> ⚠️ **本章是 2026-09-21 第一轮（按按钮页面推）的记录，规则已被第 8 章修订**：
> 清单加了 `设备控制`（31 个），判定键改成**接口路径优先**。当前真源见
> `scripts/api_groups.py` 与 `server/app/models/sysapigroup.go`。

**GB28181 业务模块（16）**
`仪表盘` `设备管理` `设备控制` `多屏播放` `告警管理` `图像库` `云端录像` `录像计划` `国标级联`
`SIP 接入信息` `国标服务配置` `国标接入安全` `设备权限工作台` `OpenAPI 客户端` `流媒体管理` `日志中心`

**平台模块（15）**
`用户管理` `角色管理` `菜单管理` `部门管理` `字典管理` `个人中心` `在线用户` `接口管理`
`文件管理` `系统配置` `代码生成` `定时任务` `插件管理` `插件示例` `认证管理`

规则（三条，逐行可复算）：

1. **分组名 = 该接口归属的功能模块**，由接口路径最长前缀匹配判定（见第 8 章，覆盖全量）；
2. **目录型顶层菜单**（`流媒体管理` / `日志中心`）下的页面归到目录名；隐藏路由页
   （`设备录像回放`）并入其入口页（`多屏播放`）；
3. 路径不足以区分的行走**显式例外表**（`api_groups.py` 的 `API_GROUP_OVERRIDES`），
   页面归属只作为审计/兜底依据保留在 `GROUP_BY_PAGE`。

---

## 4. 39 → 30 归并矩阵

| 旧分组 | 行数 | → 目标分组 |
|---|---|---|
| 按钮权限目录 | 85 | 多屏播放(36)、设备管理(30)、仪表盘(7)、国标服务配置(3)、流媒体管理(3)、系统配置(3)、图像库(2)、告警管理(1) |
| GB28181 媒体管理 | 58 | 流媒体管理(58) |
| GB28181 SIP 配置 | 34 | 国标服务配置(28)、SIP 接入信息(6) |
| GB28181 录像计划 | 13 | 录像计划(13) |
| GB28181 接入安全 | 12 | 国标接入安全(12) |
| 代码生成 | 12 | 代码生成(12) |
| GB28181 OpenAPI 客户端 | 11 | OpenAPI 客户端(11) |
| 菜单管理 | 11 | 菜单管理(11) |
| GB28181 国标级联 | 10 | 国标级联(10) |
| 任务调度 | 10 | 定时任务(9)、日志中心(1) |
| 用户管理 | 10 | 用户管理(6)、个人中心(4) |
| 文件管理 | 10 | 文件管理(10) |
| 角色管理 | 9 | 角色管理(9) |
| SIP 日志 | 8 | 日志中心(8) |
| 日志管理 | 8 | 日志中心(8) |
| GB28181 运行监控 | 7 | 设备管理(7) |
| GB28181 云端录像 | 7 | 云端录像(7) |
| 字典项管理 | 7 | 字典管理(6)、国标服务配置(1) |
| GB28181设备管理 | 7 | 设备权限工作台(7) |
| 字典管理 | 7 | 字典管理(7) |
| GB28181 设备分组 | 6 | 设备管理(6) |
| GB28181 多屏播放 | 6 | 多屏播放(6) |
| 国标通道收藏 | 5 | 多屏播放(5) |
| 部门管理 | 5 | 部门管理(5) |
| API管理 | 5 | 接口管理(5) |
| 插件示例 | 5 | 插件示例(5) |
| 认证管理 | 5 | 认证管理(5) |
| GB28181 设备维护 | 4 | 设备管理(4) |
| GB28181 告警管理 | 4 | 告警管理(4) |
| 插件管理 | 4 | 插件管理(4) |
| GB28181 云端录像下载 | 3 | 云端录像(3) |
| 系统配置 | 3 | 系统配置(3) |
| 游客权限依赖 | 2 | 仪表盘(1)、设备管理(1) |
| GB28181 播放鉴权 | 2 | 设备管理(2) |
| `æŒ‰é’®æƒé™ç›®å½•`（乱码） | 2 | 多屏播放(2) |
| GB28181 云端录像删除 | 2 | 云端录像(2) |
| 系统管理 | 2 | 在线用户(2) |
| GB28181 云端录像控制 | 1 | 云端录像(1) |
| GB28181 日志治理 | 1 | 日志中心(1) |

判定依据分布：菜单归属 344 / 多归属覆盖 31 / 路径兜底 26 / 编码损坏修复 2 = **403**。

---

## 5. 改动清单

### 5.1 数据（220 开发库）

| 文件 | 说明 |
|---|---|
| `scripts/api-group-normalization.sql` | 逐行按 id 收敛到目标分组 + 修 2 处编码损坏标题；**幂等可重跑** |
| `scripts/api-group-normalization-down.sql` | 回滚到整治前（含 39 个旧分组名与 2 个坏标题） |

执行前建了带内备份表 `sys_api_apigroup_backup_20260921`（417 行全量快照）。

**对账结果**

| 指标 | 整治前 | 整治后 |
|---|---|---|
| 在册行数 | 403 | 403 |
| 分组数 | 39 | **30** |
| 分组名不在受控清单内的行 | 403 | **0** |
| 分组名编码损坏 | 2 | **0** |
| 旧分组名残留 | — | **0** |
| 实际改动行数 | — | 326 行改分组 + 2 行改标题 |

`down → up` 往返实测：39 组 → 30 组，两脚本退出码均为 0。

### 5.2 后端（分组字典 + 写入校验）

| 文件 | 说明 |
|---|---|
| `server/app/models/sysapigroup.go`（新增） | 30 个分组的**单一真源**；`SysApiGroupNames()`（返回副本）、`IsValidSysApiGroup()` |
| `server/app/controllers/sysapi.go` | 新增 `Groups` 处理器（`GET /sysApi/groups`）；`Add` / `Update` 在**所有数据库访问之前**校验 `apiGroup` 必须属于清单，否则 400 + 业务码 1 |
| `server/app/routes/routes.go` | `sysApi.GET("/groups", ...)`，排在 `/list` 之后、`/:id` 之前（否则 `groups` 会被当 id 吃掉） |

选 Go 常量而不是 `sys_dict` 字典项的理由：分组名与**菜单结构强耦合**（菜单由迁移定义），
放进可被管理员随手改的业务字典反而会造出第二处漂移；常量让写入校验零 DB 依赖、单测好写。

### 5.3 前端（受控下拉）

| 文件 | 说明 |
|---|---|
| `web/src/api/sysapi.ts` | 新增 `getSysApiGroupsAPI()` |
| `web/src/views/system/sysapi/sysapi.vue` | 查询条件 + 新增/编辑表单的「API分组」由 `a-input` 改为 `a-select`（选项来自新接口，无 `allow-create`） |
| `web/src/components/s-api-permission/index.vue` | 权限分配抽屉的搜索条件同步改为下拉 |

> ⚠️ **上线耦合**：前端新下拉依赖 `GET /sysApi/groups`。后端未更新时下拉会是空的（界面上会报「获取API分组清单失败」），
> 届时无法新增/编辑 API。**后端必须先于前端部署**。

### 5.4 生成器（治本：拆掉漂移源头）

| 文件 | 说明 |
|---|---|
| `scripts/api_groups.py`（新增） | 页面→分组、接口→分组的共享映射（`GROUP_BY_PAGE` / `API_GROUP_OVERRIDES` / `API_GROUP_BY_ROUTE`）；**推导不出来直接报错，不再兜底** |
| `scripts/button-catalog.py` | 去掉写死的 `按钮权限目录`，改按按钮所属页面落模块 |
| `scripts/guest-permissions.py` | 去掉写死的 `按钮权限目录` + `游客权限依赖` |
| 6 个生成产物 | `2026-09-05-button-permission-catalog{,-postgresql,-sqlserver}.sql` 与 `2026-09-05-guest-readonly-permissions{,-postgresql,-sqlserver}.sql`，以及 3 份全量基线里被拼接的那两段（`uvp-gb28181.sql` / `postgresql_converted.sql` / `sqlserver_converted.sql`） |

两个生成器的 `--check` 现在都返回 "outputs match"。

### 5.5 防回归测试

| 测试 | 钉住什么 |
|---|---|
| `server/app/models/sysapigroup_test.go` | 清单非空/无重复/无首尾空白；**只允许 ASCII 可见字符与 CJK 汉字**（乱码锚点：`æ`/`’`/`™` 直接落网）；清单外值一律拒绝；返回值是副本 |
| `server/app/controllers/sysapi_groups_test.go` | 真 HTTP：`GET /sysApi/groups` 返回完整清单；`POST /sysApi/add` 对「兜底分组/乱码分组/自造分组」三种输入都回 400 + 业务码 1 |
| `server/app/routes/routes_sysapi_groups_test.go` | `/groups` 走 Casbin、与 `/list` 相邻、且在 `/:id` 之前 |
| `web/src/views/system/sysapi/sysapi.group-select.test.ts` | 两个页面 + api 层：分组必须是受控下拉，**不得退回 `a-input` 自由输入或 `allow-create`**；挂载时必须拉一次清单 |
| `scripts/button-catalog.test.py`（新增 1 条） | 两个生成器**可能写出的所有分组名**必须落在 Go 受控清单内；按钮目录里每个页面、游客迁移里每个接口都必须能推导出分组 |

---

## 6. 复现方式

```bash
# 1) 导出（220 开发库）
mysql --default-character-set=utf8mb4 -h192.168.10.220 -uroot -p uvp_gb28181 -B -N \
  -e "SELECT id,title,path,method,IFNULL(api_group,'') FROM sys_api WHERE deleted_at IS NULL;" > tmp/apigroup/api.tsv
#    （同理导出 sys_menu / sys_menu_api）
# 2) 生成映射与 SQL
python3 tmp/apigroup/normalize.py
# 3) 落库
mysql ... uvp_gb28181 < scripts/api-group-normalization.sql
```

---

## 7. 开发库存量整治：物理清除软删数据（2026-09-21 追加）

**动机**：第 5 章的分组整治只处理了**在册**行。软删行仍带着旧组名
（`GB28181 设备分配` / `GB28181 作业录像` / `按钮权限目录`），成了分组维度里的**幽灵** ——
一旦有人不加 `deleted_at IS NULL` 过滤地 `SELECT DISTINCT api_group`，就会看到它们。

**决策**（老板 2026-09-21）：清 **全部 5 张表**，**只在 220 开发库执行**，**不进三方言迁移**。

| 表 | 删除 | 原总行 | 删后 |
|---|---|---|---|
| `sys_api` | 14 | 417 | 403 |
| `sys_menu` | 10 | 209 | 199 |
| `gb_device` | 4 | 9 | 5 |
| `gb_catalog_node` | 3 | 37 | 34 |
| `gb_recording_plan` | 1 | 2 | 1 |

**删前逐项核实**：5 张表**无任何外键**（含自引用）；这 32 行在 `sys_menu_api` /
`sys_role_menu` 里**引用数均为 0** ⇒ 硬删不留悬挂。删除在**单事务**内完成。

### ⭐ 顺带修掉一个隐性故障：软删设备永久占位

`gb_device` 的唯一索引是 `uk_device_id(device_id)`，**不含 `deleted_at`**。于是软删设备会：

1. 因唯一索引**继续占住** `device_id`；
2. 而 GORM 查询默认过滤软删行 ⇒ 同 ID 设备重新注册时 `First` 查不到旧行 → `INSERT` → **撞唯一约束报错**。

即**该 ID 的设备永远无法重新接入**。被清掉的 4 个正是 2026-07-14 批量清理的测试设备
（`192.168.10.203/204/205`，离线、无厂商型号），与现存 5 个设备 `device_id` 无重叠。

### ⚠️ 反向注意：不是所有表都该物理删

`gb_cascade_platform` 有**明确的复活软删行逻辑**：
`cascade/repository/platform_resurrect.go:59` 显式
`Unscoped().Where("name = ? AND deleted_at IS NOT NULL")` —— 同名平台退役后重新创建时，
**复用**那条软删行而不是新建。这类表的软删行是**功能的一部分**，删了就破功能。

⇒ 全仓只有 2 处这种模式（都在 `platform_resurrect.go`），**都不在本次清理范围内**。
结论：「物理删软删数据」只能是**逐表确认后的一次性动作**，不能做成通用机制或迁移。

### 备份（可回滚）

| 备份 | 行数 | 用途 |
|---|---|---|
| `sys_api_purged_backup_20260921` | 14 | 本次待删行原样 |
| `sys_menu_purged_backup_20260921` | 10 | 同上 |
| `gb_device_purged_backup_20260921` | 4 | 同上 |
| `gb_catalog_node_purged_backup_20260921` | 3 | 同上 |
| `gb_recording_plan_purged_backup_20260921` | 1 | 同上 |
| `sys_api_apigroup_backup_20260921` | 417 | 第 5 章分组整治的整表快照 |
| `tmp/apigroup/purged-soft-deleted-backup-20260921.sql` | 5 条 INSERT | 文件形式，可人工回灌 |

回灌方式：`INSERT INTO <表> SELECT * FROM <表>_purged_backup_20260921;`
（`CREATE TABLE ... AS SELECT` 不含索引，回灌前按需自行补。）

### 验证（真机）

- DB：`sys_api` **403 行**、`COUNT(DISTINCT api_group)` = **30**（**不带**软删过滤也只剩 30，
  幽灵组名消失）；23 个旧组名全表计数 **0**；5 张目标表软删行 **0**。
- 接口：`GET /api/sysApi/groups` → **30 组**；`GET /api/sysApi/list?pageSize=500` → 403 行 / 30 组；
  写接口喂 `按钮权限目录` → `API分组不在受控清单内：按钮权限目录`（行数不变），
  喂 `接口管理` → 放行（验证后探针行已硬删）。

---

## 8. 第三轮：判定键从「按钮页面」改为「接口路径」（2026-09-21 追加）

### 8.1 触发与根因

老板指出「**下发目标跟踪**不属于多屏播放，**下发配置**也是 —— 这个分组设计明显不合理」。
查证实锤，而且比投诉的更直白：

```
585  读取设备配置          GET   /api/gb28181/device-mgmt/channel/:id/device-configs   → 曾判「多屏播放」
591  下发目标跟踪          POST  /api/gb28181/device-mgmt/channel/:id/target-track     → 曾判「多屏播放」
```

**根因不是数据错，是判定键选错了**：第一轮按「按钮挂在哪个页面」推分组，而
**播放控制台是工作台页面** —— 云台/预置位/巡航/对讲/抓拍/配置下发/视频参数/目标跟踪的按钮
全摆在 `/gb28181/multi-screen-playback` 上（23 个按钮），于是整个
`/api/gb28181/device-mgmt/channel/:id/*` 通道级操作族被吸进了「多屏播放」。
`下发目标跟踪` 关联的按钮竟然是 **`云台移动/变倍/聚焦/光圈`** —— 它的路径却明写着 `device-mgmt`。

**全局审计**：同一路径模块被拆到多个分组的有 6 个，最重的是 `device-mgmt`：

| 路径模块 | 被拆到 |
|---|---|
| `device-mgmt` | 设备管理×38、**多屏播放×35**、设备权限工作台×7、日志中心×2、图像库×2 |
| `sip` | 国标服务配置×31、SIP 接入信息×6（**合理**：本就是两个一级菜单） |
| `users` | 用户管理×6、个人中心×4、认证管理×1（**合理**：profile/logout 分属不同功能） |
| `sysDictItem` | 字典管理×6、国标服务配置×1（污染） |
| `play` | 设备管理×3、多屏播放×1（存疑） |
| `sysJobResults` | 定时任务×1、日志中心×1（存疑） |

### 8.2 新口径

**判定键 = 接口自己的路径（最长前缀匹配）**，新增独立分组 **`设备控制`** 承载通道级操作。
`scripts/api_groups.py` 的 `API_GROUP_RULES` 是规则真源（生成器与基线测试共用）：

1. `API_GROUP_RULES`：`(路径前缀, 分组)` 有序表，最长匹配；相邻同组前缀会合并；
2. `API_GROUP_OVERRIDES`：`(method, path)` 显式例外 —— 路径推不出来的（如 `device-mgmt/device/:id/sip-trace-capture`
   实际挂在「SIP 日志」页 → 日志中心）在这里写死；
3. `GROUP_BY_PAGE`：页面→分组映射**保留但降级为审计/兜底**，不再当主键。

结果：**30 → 31 组**，41 行改判。`设备控制` 34 行 = 云台(15) + 回放会话(4) + 对讲(3) +
抓拍(2) + 视频参数(2) + 设备配置(2) + 目标跟踪(2) + 能力状态/下载/存储卡(4)。
`多屏播放` 49 → 17（只剩真·多屏播放：分屏布局 6 + 通道收藏 5 + 录像回放入口 + 探针 + 播放流）。

### 8.3 ⭐ 关键机制发现：新装库 ≠ 导入基线

这次做「新建库与存量库口径一致」的验证，发现一个必须记住的机制：

> **新装库 = 导入基线 + runner 按序执行「未内联进基线」的迁移。**
> 实测 146 个 MySQL 迁移里只有 **27 个**内联进了基线，**119 个**是装库时现跑的。

⇒ **写在基线末尾的 reroute 段对新装库无效**（它跑在那 119 个迁移之前，随后被覆盖）。
真正让新装库收敛的是迁移文件 `2026-09-21-api-group-reroute.sql`（文件名排序最末）。
两条都保留：基线段服务「只导基线不跑迁移」的场景，迁移文件服务真实装库与客户存量库。

### 8.4 ⛔ reroute 集合必须并三源，否则有盲区

只按开发库当前 403 行生成 → **干净库跑完整链路后仍残留旧组名**（实测剩 1 行
`GET /api/gb28181/sip/service-config/default-channel-stream-transport` = `GB28181 SIP 配置`）。
因为这一行**只在迁移文件里插入过**：开发库这一行被后来的动作硬删了（该迁移 220 记过账
但行不在）、基线里也没有 ⇒ 两边都看不到它。所以并集必须覆盖：

| 源 | 覆盖什么 | 实例 |
|---|---|---|
| 开发库当前在册行 | 绝大多数 | 403 行 |
| 基线历史段 INSERT 行 | 退役/改名前的接口 | `cascade/platforms/:id/reconnect`、`channels/share|unshare` |
| **所有迁移文件 INSERT 行** | 装库时才出现、开发库已删的接口 | `default-channel-stream-transport` 的 GET |

最终 **407 行**收进 reroute（403 在册 + 4 只在源文件里）。

### 8.5 改动清单

| 层 | 文件 |
|---|---|
| 规则真源 | `scripts/api_groups.py`（路径优先重写） |
| 受控清单 | `server/app/models/sysapigroup.go`（+`设备控制`，31 组） |
| 两个生成器 | `button-catalog.py` / `guest-permissions.py` 改调 `group_for_api()`；重生成 6 个产物 |
| 前向迁移 | `migrations/2026-09-21-api-group-reroute{,-postgresql,-sqlserver}{,-down}.sql`（up 407 / down 45 条） |
| 三份基线 | 末尾追加 reroute 段（407 条 UPDATE），**历史段一字未改** |
| 防回归 | `button-catalog.test.py` 新增末态断言：基线每行都被 reroute 段覆盖且值等于规则推导值 |

### 8.6 验证证据

| 项 | 结果 |
|---|---|
| 220 开发库 | **403 行 / 31 组**；与 `API_GROUP_RULES` 逐行比对 **403/403 收敛，0 不一致** |
| down→up 往返 | 31 → 30 → 31；585/591 正确在 `多屏播放` ↔ `设备控制` 之间翻转 |
| **干净库完整链路** | 重建库 → 导新基线 → 跑全部 146 个 MySQL 迁移 → **旧组名 0 行**、30 组（缺 `插件示例` 属正常：那 5 行由示例插件自播种，非迁移） |
| DB ↔ Go 枚举 | 两边各 31 组，`comm` 双向为空 = **完全一致** |
| Go | `go build ./...` OK；`models` / `routes` / `controllers -run TestSysApi` 全 `ok` |
| Python | 两个生成器 `--check` 绿；`button-catalog.test.py` 我的新断言全绿 |
| 备份 | `sys_api_apigroup_backup_reroute_20260921`（403 行，重定口径前的值） |

### 8.7 副发现：开发库的行集本身与新装库不一致

| | 行数 | 差异 |
|---|---|---|
| 220 开发库 | 403 | — |
| 干净库（完整链路） | 399 | 少 5 行 `/api/plugins/example/*`（示例插件自播种，非迁移）；**多 1 行** `GET .../default-channel-stream-transport` |

两边**组名口径已完全一致**（同 31 组），差别只在「哪些接口存在」。这属既有状态，
不影响分组维度，但说明**不能拿开发库当新装库的行集真源** —— 判「某接口该不该存在」要回迁移文件。

---

## 9. 第四轮：接口标题（`sys_api.title`）归一化（2026-09-21 追加）

### 9.1 触发

老板在「接口管理」页看到 `仪表盘` 组里 5 行标题**全叫「查看仪表盘」**，问「这 API 标题为什么都
叫做查看仪表盘」。查证后确认**不是写错，是列语义与展示错位**：

| 环节 | 事实 |
|---|---|
| 列语义 | `sys_api.title` 的 `COMMENT` 是 **`权限名称`**（基线 `uvp-gb28181.sql` DDL），**不是「接口名」** |
| 前端展示 | 列表列头写「**API标题**」、新增表单占位符写「请输入API标题」⇒ 与列语义不符 |
| 生成器取值 | `scripts/button-catalog.py`：`title = owners[0]["title"]` ⇒ 落**按钮名** |
| 数据缺口 | `button-permissions.json` 的 `apis[]` **只有 `method`/`path`，没有接口级功能名** ⇒ 生成器没米下锅 |
| 粒度错配 | **按钮 = 权限粒度、接口 = 接口粒度**。`gb28181:home:view` 一个按钮调 5 个接口 ⇒ 5 行同名 |

**为什么别处没暴露**：多数按钮 1:1（`重置仪表盘布局` 只挂 DELETE），按钮名碰巧等于功能名。

### 9.2 ⭐ 顺带挖出的第二类问题：机械占位名（58 行）

`2026-08-30-media-management` 迁移用
`CONCAT('媒体管理 ', method, ' ', path)` 批量拼出占位名，落到 `/api/gb28181/zlm/*` 全族 **58 行**：

```
媒体管理 POST zlm/nodes/:id/recordings/runtime/stop/preflight
媒体管理 DELETE zlm/nodes/:id/proxies/pull/:key
...
```

它们**字符串唯一 ⇒ 躲过了「重复检测」**，但在接口管理页上完全不可读。**同一列上的同类病，
必须同轮解决**（否则只治了重复、没治可读性）。

### 9.3 口径与改法

**新口径**：`title` = **接口功能名**（动宾短语，读得出「这个接口做什么」）；
`button-permissions.json` 的 `apis[]` 支持可选 `title`，**只给「1 个按钮挂多接口」的按钮补**。

| 层 | 改动 |
|---|---|
| 目录 json | `button-permissions.json` 的 `apis[]` 加 `title`（248 条显式名）+ 新增 `orphanApiTitles` 键（16 条无归属按钮的纯查询接口）；`guest-permissions.json` 的 `bindings[].apis[]` 全部显式命名 |
| 生成器 | `button-catalog.py`：`api.get("title") or owners[0]["title"]` 回落；同一接口**多值冲突直接报错**。`guest-permissions.py`：**取消写死**的 `查看与观看依赖`，要求显式名（缺失即报错） |
| 迁移 | 新增三方言 `2026-09-21-api-title-normalization{,-postgresql,-sqlserver}{,-down}.sql`（up **265** / down **118**，`(path,method)` 定位、带 `<>` 守卫 ⇒ 幂等） |
| 数据 | 220 实改 **58 行**（机械名）+ 早前 118 行累计；终态 **403 行 / 403 唯一标题 / 机械名 0** |

**取值原则：churn 最小** —— 优先沿用**库现值**（那是人工可读名），只在库值恰好是按钮名/机械名时
才另拟。实测 248 条里只有 5 条是新拟的。

### 9.4 ⛔⛔ 本轮最重要的机制：生成器「只插不改」+ down 不能反推

1. **改生成器/改目录修不了已有行。** 两个生成器的 `sys_api` INSERT 都带
   `WHERE NOT EXISTS(...)` 守卫、**没有任何 UPDATE 分支** ⇒ 必须配**前向迁移**
   （与第 3 轮 `api_group` 是同一个坑）。
2. **down 不能从「当前库」反推。** 迁移落库后库里已是终态，再读库算差异会得到**空 down**。
   正确做法：**从迁移执行前的快照表取旧值** —— 本轮用 `sys_api_title_backup_20260921`
   （`CREATE TABLE ... AS SELECT id,title,path,method FROM sys_api`）。
   实测执行 down 后与快照表**逐行比对 0 处不同**。
3. **标题集合必须覆盖「无按钮归属」的行。** 那 16 条 `/api/gb28181/zlm/*` 纯查询接口
   **不被任何按钮的 `apis[]` 包含**（加进去会改变 button 的授权语义），只能靠目录新增的
   `orphanApiTitles` 键声明，否则新装库永远留着这 16 行机械名。
   （同类教训：第 3 轮漏了「只在迁移文件里插入」的行。）

### 9.5 验证证据

- **220 终态**：403 行 / `COUNT(DISTINCT title)` **403** / 机械名 **0** / 重复 **0** / 旧组名 **0** / 软删 **0**。
- **干净库完整链路**（导基线 → 按序跑全部 146 个 MySQL 迁移）：**399 行 / 399 唯一 / 机械名 0**。
  与 220 的差异仅 6 行，全部是**既有差异**（5 行 `/api/plugins/example/*` 由示例插件运行时自播种
  + 1 行 220 曾硬删）—— 见 9.7。
- **down → up 往返**：down 后与 `sys_api_title_backup_20260921` 逐行比对 **0 处不同**；
  再 up 回到终态（机械名 0）。
- **生成器**：两道 `--check` 绿；产出集合 **0 重复、0 机械名、0 未声明**。
- **Go**：`go build ./...` OK；`app/models`、`app/routes`、`app/controllers -run TestSysApi` 全 `ok`。
- **前端**：`vitest` 7 passed。
- **备份**：`sys_api_title_backup_20260921`（标题迁移**首次执行前**整表，`down` 的旧值取自它）、
  `sys_api_title_backup2_20260921`（**机械名清理前**整表）。

### 9.6 新增防回归（4 条）

| 测试 | 挡什么 |
|---|---|
| `test_multi_api_buttons_declare_every_api_title` | 「1 按钮挂多接口」的每个接口必须有显式 `apis[].title`（否则回落按钮名 ⇒ 重现重复） |
| `test_emitted_titles_are_unique` | 生成器会写出的标题**全局唯一**；并单独挡**机械占位名**（`MECHANICAL_TITLE` 正则） |
| `test_explicit_api_titles_do_not_conflict` | 同一 `(path, method)` 不得被赋两个不同名字 |
| `test_title_migration_covers_every_declared_api` | 标题迁移必须**双向**覆盖目录声明（缺一条 / 多一条 / 值不等都红） |

---

## 10. 第五轮：路径规则的漏网修正 —— 探针与流监控归「流媒体管理」（2026-09-21 追加）

### 10.1 触发

老板问「**查询探针检测结果**怎么归到多屏播放下了」。查证后发现**不是个例** ——
是第 3 轮那套路径规则的**漏网**，实际 3 条：

| 接口 | 曾判 | 现判 |
|---|---|---|
| `GET /api/gb28181/stream-probes/operations/:operationId` | 多屏播放 | **流媒体管理** |
| `POST /api/gb28181/stream-probes/:streamId` | 多屏播放 | **流媒体管理** |
| `GET /api/gb28181/play/:streamId/monitor` | 多屏播放 | **流媒体管理** |

### 10.2 根因：一处「以按钮页面为理由」的显式规则

`scripts/api_groups.py` 里曾写着：

```python
# 探针诊断的是播放流，按钮也挂在多屏播放页   ← 又拿「按钮在哪页」当判据
("/api/gb28181/stream-probes", "多屏播放"),
```

第 3 轮已经把判据换成「按接口功能归属」，但**这条是显式前缀规则，绕过了新判据原地留住了**。
为什么它这次特别站不住（本轮新取证）：

1. `140368 多屏播放` 是 **type=2 页面菜单**，其下 16 个 **type=3 按钮**全是播放控制台的
   控制项（设备复合控制、云台 ×6、语音对讲、抓拍、探针、流监控、分享、截图）。
2. ⭐ **播放控制台不是这个页面的子组件** —— `PlayConsoleLinked` 挂在
   `web/src/layout/components/PlaybackConsoleHost.vue`，是 **layout 级全局宿主**，
   从任何页面都能唤起。「按钮挂在多屏播放下」只说明它是**主入口页**，不代表功能属于该模块。
3. 第 3 轮已按同一认识动过**同一批按钮**：把路径带 `device-mgmt` 的 **23 条**挪去了「设备控制」。
   `stream-probes` 路径里没有 `device-mgmt` 这个词，就被那条显式规则留在了原地。

### 10.3 语义取证

`streamprobe` / `streammonitor` 在后端是**独立子包**（`app/gb28181/streamprobe`、
`app/gb28181/streammonitor`），与 `zlm` 同级 —— 都属**媒体流层能力**，观测的是「流」
（帧到达时间线、卡顿、实时指标）；而「多屏播放」管的是**分屏布局方案**与**播放地址/点播动作**。
探针本身也走 ZLM 的 `addProbe`。

### 10.4 顺带体检：规则表里「以按钮页面为理由」的条目共 6 处

逐条核过，**只有这一处是真漏网**。另 5 处都是「页面 == 模块」的正常情形
（`users/profile` 的按钮就在个人中心页、SIP 诊断采集的按钮就在 SIP 日志页、
存储卡格式化与重启/升级同族）。

### 10.5 改动面

| 层 | 内容 |
|---|---|
| 规则表 | `stream-probes` → `流媒体管理`；新增更长前缀 `/api/gb28181/play/:streamId/monitor` → `流媒体管理`（必须排在 `/api/gb28181/play` **之前**，靠最长前缀优先命中） |
| 生成器 | 重跑两个生成器（3 方言迁移 + 3 基线），并把 `tmp/apigroup/reroute.py` 的内嵌规则表删掉 |
| 迁移 | `2026-09-21-api-group-reroute{,-postgresql,-sqlserver}{,-down}.sql` **就地重建**（**没有新增迁移**：该迁移尚未发到任何客户库，且 220 的 `gb_schema_migrations` 已记账 ⇒ 就地更新 + 手工重跑即可；再开一条新迁移反而会给客户库留两份功能重叠的脚本） |
| 数据 | 220 改判 **3 行** |

⚠️ **`down` 的旧值来源**：生成器原先是「从当前库反推」，而落库后库里已是终态 ⇒ 只能算出 7 行。
已改为从**迁移前快照表** `sys_api_apigroup_backup_reroute_20260921` 取旧值（恢复为 48 行）——
与第 4 轮标题迁移**同一个坑**（见 §9.4）。

### 10.6 顺带修掉「两个真源」

`tmp/apigroup/reroute.py` 原先**内嵌了一份 `API_GROUP_RULES` 拷贝**。本次差点出事故：
改了共享表里的探针归属，而那份拷贝仍写着「多屏播放」，跑一次就把规则表算回旧值
（表现为：规则表已改对，重算出来的映射却是旧的）。已改成 `import api_groups` 直接复用共享真源。

### 10.7 验证证据

- 220：**403 行 / 31 组**；逐行比 `match_rule()` → **403/403 收敛，0 不一致**；
  多屏播放 **17 → 14**、流媒体管理 **61 → 64**、设备控制 34 不变。
- **干净库完整链路**：导基线 → 按序跑全部 MySQL 迁移 → 与 220 **完全一致**（14 / 64），无旧组名。
- `down → up` 往返可逆：down 回 30 组且那 3 行回「多屏播放」，up 回 31 组。
- 端到端实测（`/api/sysApi/list?pageSize=500`）：那 3 条的 `apiGroup` 均为 `流媒体管理`。
- 生成器两道 `--check` 绿；`go build ./...` OK + `models`/`routes`/`controllers -run TestSysApi` 全 `ok`；
  前端 vitest 7 passed。

**多屏播放现在只剩 14 条，全部名副其实**：`playback-schemes`（分屏布局方案）×6、
`channel-favorite-groups`（通道收藏）×5、`play`（点播动作）×3。

### 10.8 新增防回归（2 条）

| 测试 | 挡什么 |
|---|---|
| `test_multi_screen_group_holds_only_its_own_paths` | 「多屏播放」组内的接口必须命中该组**专属**的路径前缀，不许是别处漂来的 |
| `test_workspace_pages_do_not_collapse_into_one_group` | **工作台页面不得决定分组** —— 播放控制台那 16 个按钮覆盖的 37 个接口横跨「设备控制/多屏播放/流媒体管理」**三个模块**，断言它们必须落在 **≥2** 个分组里，且每条都能命中路径规则（不许有「只能靠页面推」的行） |

---

## 11. 遗留（**未做**，需单独立项）

1. ~~**三方言增量迁移没有做**~~ → **已在第 3 轮补上**（`2026-09-21-api-group-reroute` 三方言 up/down，
   `(path, method)` 定位，幂等）。客户存量库现在跑这一条即可对齐。
2. ~~**8 个后续手写迁移仍写着 `按钮权限目录`**~~ → **第 3 轮已用「并集 + 前向迁移」解决**：
   这 8 个迁移确实还在写旧组名，但排序在它们之后的 `2026-09-21-api-group-reroute` 会把
   它们插入的行重新收敛 —— 干净库完整链路实测旧组名 **0 行**（见 9.4 / 9.6）。
   选择这条而非改写 8 个迁移，是因为改写会破坏「迁移是历史记录」的可追溯性，
   且对已跑过它们的客户库无效。
3. **`/api/sysParam/*` 3 个接口疑似退役功能残留**（本次已归入 `系统配置`，未删）：
   控制器 `server/app/controllers/sysparam.go` 在，但前端零调用，父菜单 `140436` 已不存在。
4. **22 行 API 至今没有任何菜单关联**（孤儿），本次只修了分组，没动关联关系。
5. **`scripts/button-catalog.test.py` 有 3 条存量失败**（与本次改动无关）：
   - `cascade/platforms/:id/reconnect` 路由已退役（见 `cascade_reconnect_retire_test.go`），目录仍引用；
   - `web/src/views/gb28181/device-mgmt/DeviceMaintenanceDialog.vue` 已被提交 `0553010e` 改名，目录里的 `source` 路径没跟着更新；
   - `openapi-client` 的 6 个前端权限码没登记进目录。
6. **⚠️ 列语义仍与前端标签不一致（第 4 轮遗留的最后一环）**：第 4 轮把**数据**统一到了
   「接口功能名」，但：
   - 列 `COMMENT` 仍是 **`权限名称`**（基线 DDL）；
   - 前端列头/表单仍写「**API标题**」。

   数据已经对得上前端标签了（都是「接口名」语义），所以这不影响使用；但**注释那句话现在是错的**。
   修法：把三份基线 DDL 的 `COMMENT` 改成「接口名称/接口标题」—— 属结构文本改动，需同时改
   三份基线 + 三方言（⛔ 基线历史段不可改，只能改 `CREATE TABLE`）。**本轮未做，避免为一句话
   掀起结构面改动。**
7. **⚠️ 220 库同跑 4 种 collation（第 4 轮副发现）**：65 `utf8mb4_general_ci` /
   29 `utf8mb4_0900_ai_ci`（含 `sys_menu`）/ 14 `utf8mb4_unicode_ci`（含 `sys_api`、`sys_menu_api`）/
   7 `utf8mb4_bin`。`sys_api.title` 与 `sys_menu.title` **跨表比较直接 1267**
   （见 `.workbuddy/memory/topics/migrations-and-db.md`）。治理要 `ALTER TABLE … CONVERT TO`，
   属独立立项。

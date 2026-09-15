# C08 · 平台支撑域与命名空间归属契约（2026-09-15 完成）

> 本项处理的是**除设备链路以外**的那一半日志：平台自身的功能（权限、调度、菜单、
> 代码生成、附件、审计、HTTP 入口）以及**命名空间归属**。
>
> 结论先说：**「其他」桶 71% 不是"命名混乱"，是"白名单没列"** —— 97 条里
> 没有一条是未归类的裸前缀，全部是合法命名空间（`scheduler` / `models` / `casbin` /
> `auth` / `codegen` / `sysaffix` …）只是不在 `BUSINESS_NS` 白名单里。
> 所以 C08 拆成**两个独立指标**：归属覆盖率（补白名单可解）与事件名质量（改事件名才可解），
> 防止前者把后者盖住。

---

## 一、这条链路要回答什么

排障时，平台支撑域的问题**不是"哪一路设备"**，而是下面这六类对象之一：

| # | 问题 | 定位对象 | 字段 |
|---|---|---|---|
| ① | 谁的操作出错了？ | 用户 / 操作人 | `user_id` `username` `operator_id` |
| ② | 哪个接口挂了？ | HTTP 请求 | `route` `method` `source_ip` |
| ③ | 哪个角色/菜单的授权写坏了？ | 权限主体与资产 | `role_id` `parent_role_id` `menu_id` |
| ④ | 哪次定时任务失败了？ | 任务与执行实例 | `job_id` `execution_id` |
| ⑤ | 哪份代码生成资产读不出来？ | 模板 / 文件 / 目标表 | `template_name` `template_path` `file_path` `table_name` |
| ⑥ | 哪次上传会话清理失败？ | 上传会话 | `upload_id` |

**这六类对象全部不是设备** —— 这正是 C08 与 C04–C07 的根本差别：前面几项都是
"哪一路"，C08 是"谁 / 哪个请求 / 哪份资产"。

---

## 二、命名空间三分组（`scan-logging.py`）

旧版只有一个 `BUSINESS_NS` 白名单（11 个），命中不了的**一律进「其他」桶**。
于是 97 条合法日志被判"未归类"，`其他` 桶 71% 无定位，看着像命名混乱。

C08 把它拆成三组 + 一个「未归类」兜底：

| 组 | 排障对象 | 成员 |
|---|---|---|
| **业务域** | 哪一路（设备/通道/流/平台/节点） | `gb28181` `cascade` `play` `ptz` `zlm` `recording` `recordingplan` `talk` `subscribe` `push` `security` |
| **平台支撑域** | 哪个请求 / 谁 / 哪个任务 / 哪份资产 | `scheduler` `models` `casbin` `auth` `audit` `db` `codegen` `http` `setup` `realtime_log` `streammonitor` `sip` `sysaffix` `sysmenu` `sysrole` `sysgenservice` `sysmenuservice` `websocket` `mcp` |
| **进程框架域** | 哪个进程 / 哪份配置 / 框架自身 | `lifecycle` `migration` `logging` `startup` `legacy` `config` `plugin` `gin` |
| **未归类** | —— | `其他`（应趋 0）+ `(无 event)` |

### ⚠️ 这次拆分的性质：**补观测，不是放宽判定**

`has_loc`（无定位判定）与命名空间分组**完全无关**。补分组前
`scheduler.demo.started`（带 `job_id`）就已经是"有定位"，只是被错算进「其他」。
所以「其他」桶 97 → 0 **不是治理成果**，而是**把问题从"看不见"变成"看得见"**：

```
（补分组前）其他  97 条 / 无定位 69 条 / 71%   ← 一个数字，什么都看不见
（补分组后）auth 40%  http 33%  db 100%  codegen 20%  casbin 50%  sysgenservice 80% …
```

### ⚠️ 补分组会盖住事件名问题 → 所以另设指标 ⑤

`sysaffix.upload.warn` 归到 `sysaffix` 之后不再显眼，但它的事件名依然在说等级而不是事件。
因此新增**独立指标 ⑤ 事件名末段复述等级**（`EVENT_LEVEL_ECHO_RE`）：

> 判定一个末段词是不是"等级复述"，看它**换掉之后事件还说不说得清发生了什么**。
> `.warn` / `.error` / `.info` / `.debug` / `.fatal` 算；
> **`.panic` 不算** —— `http.panic` / `play.reconcile.panic` 里的 `.panic` 是
> **事件语义**（进程崩了），算进来会误报 3 条。

指标 ⑤ 与归属覆盖率**必须一起看**，只看一个都会得出错误结论。

---

## 三、非设备类定位维度（`IDENT_BASE` 新增 10 项）

到此为止 `IDENT_BASE` 全是"设备/通道/流/平台/节点/请求"这一族。C08 新增：

| 字段 | 回答什么 | 实证调用点 |
|---|---|---|
| `username` | 登录失败时的"谁"（认证没过，拿不到 `user_id`） | `auth.login_audit.persist_failed` |
| `role_id` / `parent_role_id` | 角色授权的主体与它要继承的父角色 | `casbin.role_inheritance.{add,update,remove}_rejected` |
| `menu_id` | 菜单资产 | `sysmenu.menu_api_{delete,insert}_failed`、`sysrole.role_menu_*_failed` |
| `template_name` / `template_path` | 代码生成模板 | `codegen.template_{read,parse,execute}_failed` |
| `file_path` | 明确的文件路径 | `codegen.file_skipped`、`sysaffix.{thumbnail_generate,physical_file_delete,thumbnail_delete}_failed` |
| `operator_id` | 谁做的这次操作 | `setup.config_saved`、`setup.install_guide_skipped`、`gb28181.traffic.viewer_kicked` |
| `upload_id` | 分片上传会话 | `sysaffix.chunk_upload_cancel_failed` |
| `table_name` | 代码生成的目标表 | `sysgenservice.table_comment_read_failed` |

### 收录标准**没有放宽**

仍是那一句：**这个字段的值能不能唯一指向"出事时第一个要查的那个对象"**。

**明确不收**（答不出"谁"，属状态性字段）：
`phase`（哪一步）/ `status`（什么状态）/ `dialect`（什么方言）/ `role`（master/slave）/
`count` / `duration` / `interval` / `scope`（打包字符串）。

**明确不收 `path`** —— `file_path` 才有"文件"语义。`models.area.load_failed` 的
`path` 是内置区域数据文件路径，属**组件级豁免**（见下）。

---

## 四、逐事件契约

### 4.1 事件名修复（27 处）

劣质事件名一律改成"描述发生了什么"。**这些名字零外部引用**（全仓只出现在
自己的定义处 + 我写的文档注释里），改名是纯收益。

| 改前 | 改后 | 为什么 |
|---|---|---|
| `sysaffix.upload.warn` | `sysaffix.thumbnail_generate_failed` | 说的是**缩略图生成失败**，`.warn` 什么也没说 |
| `sysaffix.delete.error` | `sysaffix.physical_file_delete_failed` | 区分于下一条 |
| `sysaffix.delete.warn` | `sysaffix.thumbnail_delete_failed` | 两条 `.delete.*` 是**不同的事** |
| `sysmenu.setmenuapis.error` ×2 | `sysmenu.menu_api_delete_failed` / `menu_api_insert_failed` | **同名不同事**（删关联 vs 插关联），按 event 检索时区分不出 |
| `sysrole.addrolemenu.error` ×2 | `sysrole.role_menu_delete_failed` / `role_menu_insert_failed` | 同上 |
| `device_ptz_resources.writehomepositionfailure.warn` | `ptz.home_position.write_failed` | **同文件里两套命名**（315/547 行早就在用 `ptz.*`）；把函数名横拼成事件名 |
| `device_ptz_resources.executeptzextendedresourceas.warn` | `ptz.cruise_cache.delete_failed` | 名实不符 —— 实际做的是删巡航本地缓存 |
| `device_ptz_resources.createcruisetrack.warn` | `ptz.cruise_reconcile_record.write_failed` | 实际是写待对账记录失败 |
| `device_traffic.kickviewer.info` | `gb28181.traffic.viewer_kicked` | 归业务域 |
| `setup.saveconfig.info` | `setup.config_saved` | `.info` 复述等级 |
| `setup.saveconfig.error` | `setup.sip_reload_failed` | **名实不符** —— message 是"SIP 保存后热启动失败" |
| `setup.skip.info` | `setup.install_guide_skipped` | |
| `casbinservice.{add,edit,delete}roleinheritance.warn` | `casbin.role_inheritance.{add,update,remove}_rejected` | 归 `casbin` 域；且这三条是**参数非法被拒**（`return nil`），不是写失败 |
| `sysaffixservice.cancelchunkupload.warn` | `sysaffix.chunk_upload_cancel_failed` | 归 `sysaffix` 域 |
| `sysgenservice.batchinsert.error` ×2 | `sysgenservice.batch_insert_rollback` / `table_comment_read_failed` | **同名不同事** |
| `sysgenservice.{refreshfields,update,delete}.error` | `sysgenservice.{refresh_fields,update,delete}_rollback` | |
| `sysmenuservice.import.info` | `sysmenuservice.menu_imported` | 附带修掉 3 个**中文字段名** |

⚠️ **三个中文字段名**（`sysmenuservice.go:306-308`）：`zap.Int("新增菜单数量", …)` /
`zap.Int("新增API数量", …)` / `zap.String("新增菜单", …)`。
扫描器的 `FIELD_RE` 只认 `[A-Za-z_]` → **中文 key 被静默忽略**，
于是这三个字段既不进「唯一字段名」统计、也永远不会被判"无定位"。
已改为 `added_menus` / `added_apis` / `new_menu_names`。

> **这是一类独立盲区**：字段名不在 ASCII 词法范围内时，工具链**默认它不存在**。
> 当前全仓只剩这 3 处（已修），复跑：
> `rg -n 'zap\.\w+\("[^"]*\p{Han}[^"]*"'` 应为空。

### 4.2 定位字段补齐（20 处）

| 事件 | 补了什么 | 值本来在哪 |
|---|---|---|
| `auth.access_token.read_failed` / `.invalid` | `route` `method` `source_ip` | `c`（gin.Context） |
| `auth.session.touch_failed` | `user_id` `route` | **`claims.UserID` 已解析出来却没用** |
| `http.operation_failed` / `.rejected` ×2 | `route` `method` `source_ip` | `ctx` —— 这是**所有控制器的失败兜底**，不记 route 就不知道哪个接口挂了。C03.③ 起按状态码拆成两个事件名（5xx → `failed` / 其余 → `rejected`），两支字段相同 |
| `http.panic` | `route` `method` `source_ip` | `c` |
| `casbin.permission.check` / `.denied` | `uid` → **`user_id`**（统一命名） | 已在 |
| `casbin.permission.check_failed` | `user_id` `route` `method` | **`userSubject`/`route`/`method` 三个值全在手，一个没打** |
| `casbin.role_inheritance.*_rejected` ×3 | `role_id` `parent_role_id` | 函数参数 |
| `audit.operation_log.persist_failed` | `user_id` `route` | `log` 对象（落库失败时它在手上） |
| `recording.catalog.reconcile_failed` | **拆** `scope` → `node_id` + `phase` | 见 §五.1 |
| `ptz.scheduler.persist_failed` | `attempt` → `attempt_id`、`operation` → `operation_id` | 值本来就是 ID，字段名丢了 `_id` |
| `sysaffix.*` ×3 | `file_path` | `response.Path` / `affix.Path` / `affix.ThumbnailPath` |
| `setup.sip_reload_failed` | `server_id` | `view.ServerID`（同函数内） |
| `sysaffix.chunk_upload_cancel_failed` | `upload_id` | 函数参数 |
| `sysgenservice.table_comment_read_failed` | `table` → `table_name` | —— |

### 4.3 补事件名（40 条，(无 event) 40 → 1）

| 来源 | 条数 | 事件名 |
|---|---|---|
| `app/gb28181/bootstrap.go` | 31 | `gb28181.lifecycle.*`（里程碑）/ `gb28181.cleanup.*`（周期清理）/ `gb28181.{sip,playauth,cascade,talk,traffic,civilcode}.*`（启动期降级） |
| `app/scheduler/register.go` | 5 | `scheduler.jobs.{load_failed,loaded}`、`scheduler.job.{parameter_parse_failed,add_failed,loaded}` |
| `app/utils/ginhelper/ginhelper.go` | 4 | `plugin.routes.{none,initialized,skipped_nil,ready}` |

剩余 1 条 = `zlm/heartbeat/watcher.go:28`，**C07 已登记的动态事件名盲区**（见 `contracts/zlm.md`），不改。

> ⚠️ **`gb28181.lifecycle.*` 这一族要单独说**：它们是"XX 已启动 / 已装配"的**生命周期里程碑**，
> **判据②答不出"看到它要做什么动作"**。C07 曾把它们移交 C08，理由是"不给噪声发身份证"。
>
> C08 的结论：**补 event，但不假装它们合格**。理由是 C01 的方向是"事件唯一登记处"，
> 没有 event 的日志就是事件目录里的黑洞；而"要不要删这些日志"是**产品决策**
> （谁需要知道"清理了 3 条位置历史"？），不是治理决策。
> **待裁决清单见 §七。**

---

## 五、判定边界（复核时**不要**做的事）

### 5.1 不要把定位信息打包进字符串

```go
// 旧：node=7 在字符串里，人眼能看懂，按 node_id 聚合做不到
s.recordFailure("run-node:node="+strconv.FormatInt(nodeID, 10), err, ctx)
//           → zap.String("scope", "run-node:node=7")

// 新：拆成两个字段（`recordNodeFailure` / `recordSchedulerFailure`）
zap.Int64("node_id", nodeID), zap.String("phase", phase)
```

**同时**：没有具体节点时（`Enqueue` 自己失败）**不得塞 `node_id=0`** ——
那会把"调度器入队失败"谎报成"0 号节点失败"，属**信息错误**，比缺字段更糟。
已用契约测试锁住（`TestLoggingSchedulerFailureOmitsNodeID` 断言 `node_id` **缺席**）。

### 5.2 `scheduler.result.persist_failed` **不能补 `job_id`**（补了也无效）

它的 `job_id` 是 `logging.WithIdentity(root, zap.String("job_id", …))` 注入 core 的。
`runtimeCore.Write` 里：

```go
for _, f := range fields {
    if fixedKey(f.Key) || identityKey(f.Key) || c.bound[f.Key] { continue }  // ← bound 跳过
}
```

`c.bound` 已含 `job_id` → **调用点再写一次会被丢弃**。且 `identityKey` 明确列出
`request_id` / `client_request_id` / `execution_id` 三个 key 是**无条件剥离**的
（只能由 `WithIdentity` 注入）。

> **这是第三族"静态不可见"**：字段由 **context / core 携带**，
> 既不像 `[]zap.Field`（§4.3）也不像 `traceFields ...`（§4.11）——**前两族还能靠改写消掉，这一族连补都补不了**。
> **处置：登记为脚本已知盲区，不改代码**；运行时由观测器测试锁住。
> 影响面：`WithIdentity` 全仓只有 4 个调用点
> （`scheduler/result_handler.go` / `recordingplan/engine.go` / `schedulerhelper/zap_logger.go` / `ginhelper/logging.go`）。

### 5.3 别用全局字符串替换改日志字段名

`nodeId` / `channelId` / `serverId` / `callId` / `errorCode` 这一族**同时也是 HTTP API 的
JSON 字段名与 map key**（`web/` 前端、`controllers/*_test.go` 的响应断言都依赖它）。
C08 的替换**只匹配 `zap.<Method>("camelName"`（带 `zap.` 前缀与引号）**：

```python
ZAP_CAMEL = re.compile(r'(zap\.(?:String|Int|...)\()"([a-z][a-z0-9]*[A-Z][A-Za-z0-9]*)"')
```

57 处改动全部safe；`device_mgmt_test.go` / `play_stop_test.go` 里那 29 处
`body["deviceId"]` 是**响应体断言，一个都不能动**。

改完必须同步的**真正的日志断言**只有 5 处（`ptz` 3 处 / `streammonitor` 2 处）。

### 5.4 别给进程级事件补定位字段

`casbin.policy_reload.*`（5 条，AutoLoadPolicy goroutine）/ `db.diagnostic`（3 条，GORM 诊断）/
`lifecycle.*` / `migration.*` / `logging.*` / `config.reloaded` / `startup.failed` ——
它们发生时**根本没有请求上下文**，硬塞 `user_id` 只会得到 `user_id=""`。
判据：**看到它要做什么动作的对象** 才是定位对象。

---

## 六、豁免清单（进程框架域 + 组件级）

| 组 | 条数 | 无定位 | 处置 |
|---|---|---|---|
| 进程框架域 | 17 | 17 | **全豁免** —— 进程/配置/框架自身，无请求上下文 |
| `db` | 5 | 5 | 全豁免（GORM 诊断 / dialector 初始化） |
| `casbin` | 12 | 6 | 6 条 = `policy_reload.*` 进程级 |
| `sysgenservice` | 5 | 4 | 4 条 = `*_rollback`（panic recover，事务已回滚） |
| `models` | 8 | 2 | `models.area.load_failed` ×2 —— 内置区域数据加载，组件级；`path` 不收入基线 |
| `http` | 6 | 2 | `listen_failed` + `gin.diagnostic` 进程级 |
| `plugin` | 5 | 5 | 插件路由装配（`plugin.example.*` / `routes_registered`） |
| `sip` / `security` / `talk` / `recording` | 9 | 7 | shutdown 期 / 装配期 |
| `zlm` | 32 | 17 | C07 已登记（装配期节点表还读不到） |
| `play` | 20 | 8 | C04 已登记（组件级 + 轮次级） |

**门禁豁免清单**：`reviewed_legacy.json` **77 → 37**（C08 删 40 条：
bootstrap 31 + register 5 + ginhelper 4 —— 补 event 后不再需要豁免）。
**只减不增**，与 C09 的方向一致。

---

## 七、待裁决（留给用户 / C09）

**`gb28181.lifecycle.*` 等 12 条"XX 已启动 / 已装配"的 Info 级日志要不要保留？**

- 现状：已补 event，可检索，但**判据②答不出动作**（看到"心跳 Watcher 已启动"不需要做任何事）。
- 保留派观点：启动自检结果（`play_service_assembled` 附带的 `recovery_*` 5 个计数
  是"启动时恢复了几个流"的唯一记录）。
- 删除派观点：违反判据②，属"为了打印而打印"。
- **C08 不做这个决策**——删日志会影响现场排查习惯，需要产品判断。

具体条目（`bootstrap.go`）：`lifecycle.{heartbeat_watcher,thread_load_poller,offline_scanner,
play_service,play_reconciler,recording_reconciler,recording_signer,recording_catalog,
traffic_hook,zlm_scheduler,civilcode}_*` + `cleanup.{dashboard_facts,scheduler_log,
metrics_pairs,location_history}_*`。

---

## 八、逐项验收

```bash
cd server

# L1 门禁：0 findings（C08 新增 0 条）
go test -count=1 -run TestLoggingPolicyRepository ./internal/loggingcontract/

# L2 编译与包测试
go build ./...
go test -count=1 ./app/... ./internal/loggingcontract/

# 指标复跑
python3 ../docs/logging-governance/scan-logging.py --root .

# 逐条判定（三分组任一组，或某个命名空间）
python3 ../docs/logging-governance/scan-logging.py --root . --dump-ns 平台支撑域
python3 ../docs/logging-governance/scan-logging.py --root . --dump-ns casbin

# 负面判据：中文字段名 / 末段复述等级 / camelCase 都必须是 0
grep -cE 'zap\.\w+\("[^"]*[一-龥]' $(git ls-files '*.go') | grep -v ':0' || echo "中文字段名 0 ✅"
```

### C08 前后对照

| 指标 | C07 后 | **C08 后** | 性质 |
|---|---|---|---|
| 日志调用点 | 349 | **351** | +2（原本"零 zap 参数"的调用进入统计） |
| 无定位字段 | 152 (43%) | **125 (35%)** | -27 实质改善 |
| Warn 占比 | 150 (43%) | **150 (42%)** | 未动（C03 的事） |
| 唯一 event | 287 | **330** | +43（新登记事件名） |
| 唯一字段名 | 167 | **161** | -6（camelCase 归一 + 中文 key 转 ASCII） |
| camelCase 字段 | 58 | **0** | ✅ 达成 |
| 事件名末段复述等级 | 28 | **0** | ✅ 达成（新指标） |
| 「其他」桶 | 97 / 71% 无定位 | **0** | 归属补全（非治理成果） |
| `(无 event)` | 40 | **1** | 剩 1 条为已登记盲区 |
| 门禁豁免 | 77 | **37** | 只减不增 |
| 门禁 findings | 0 | **0** | 保持全绿 |

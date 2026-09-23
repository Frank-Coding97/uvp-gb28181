# 日志治理验收手册

> 适用对象：任何一次「日志改动」的验收（新增定位字段 / 改字段名 / **调等级** / 改输出形态）。
> 原则：**自动化能证明的，不要靠眼睛；眼睛能证明的，必须真的跑一遍。**
> 最后更新：2026-09-15（C03.④ 假阴性复核 —— 补入"零连带的第二次验证"与扫描器隐藏目录口径）

---

## 0. 环境前置

| 项 | 要求 |
|---|---|
| 后端 | `:8280` 起着，且**跑的是含本次改动的构建** |
| 确认方法 | `ps -o lstart -p $(lsof -nP -iTCP:8280 -sTCP:LISTEN -t)` 的启动时间 **晚于** 改动文件的 mtime |
| 日志文件 | `server/resource/logs/uvp-gb28181.log`（`logs.textformat: console` 时为人读格式） |
| 依赖 | `192.168.10.220` 的 MySQL 3306 / Redis 6379 / ZLM 18080 可达 |
| 前端 | `node_modules` 就绪（`web/`） |

⚠️ **最容易踩的坑**：进程起着但跑的是**改动前的二进制**。开工第一件事就是核对启动时间，
否则后面所有"日志没变化"的结论都是假的。

---

## L1 静态门禁（~30 秒）

```bash
cd server && export PATH=/opt/homebrew/Cellar/go/1.25.6/bin:$PATH
# ① 类型感知策略门禁（C05–C08 的成果）
go test -count=1 -run TestLoggingPolicyRepository ./internal/loggingcontract/
# ② 事件/字段登记册门禁（C09 的成果）
go test -count=1 ./internal/loggingcatalog/
```

**判据**：两个门禁都是 **0 findings**（2026-09-15 C09 后）。

```bash
# 只看类型分布，不看全文
go test -count=1 -run TestLoggingPolicyRepository ./internal/loggingcontract/ 2>&1 \
  | grep -oE "unresolved_logger|missing_event|unknown_field|dynamic_event" | sort | uniq -c
```

⚠️ 登记册门禁红了之后**不要先跑生成器**。先看 finding 是哪条规则
（`unregistered_event` / `locating_field_lost` / `field_not_in_dictionary` …），
判定它该改代码还是该改登记册；确认之后才跑：

```bash
UVP_LOGGING_CATALOG_UPDATE=1 go test ./internal/loggingcatalog/ \
  -run TestLoggingCatalogRegenerate -count=1     # 生成器，diff 就是审查
```

详见 [`contracts/registry.md`](./contracts/registry.md) §四。

### ⛔ 改等级（`Warn` ↔ `Info` ↔ `Debug`）会**连带**让 L1 红两处

这不是误报，是门禁设计的复查触发点。看到下面两条，**去核对，不要绕过**：

| 报什么 | 为什么 | 怎么办 |
|---|---|---|
| `legacy_exception_mismatch: legacy exception matched 0 call(s)` + 同一位置 `missing_event` | `reviewed_legacy.json` 按 `file + function + **method** + message` 匹配，**要求恰好命中 1 个调用点**。改了等级，那条豁免就匹配不上了 | 同步改豁免里的 `method`，并把 `reason` 改成与新事实相符 |
| `legacy_exception_mismatch: reviewed adapter source changed` | `reviewed_adapters.json` 按**文件 SHA-256** 封条；碰了被封印的文件就失效 | 重新读一遍该适配器的转发/归属契约确认没变，再更新 `sha256` 与 `reason` |

逐条判定正本见 [`contracts/levels.md`](./contracts/levels.md) §八。

---

## L2 自动化回归（~3 分钟）

```bash
cd server && export PATH=/opt/homebrew/Cellar/go/1.25.6/bin:$PATH
go build ./... && \
go test -count=1 ./app/gb28181/play/... ./app/gb28181/handler/... \
  ./app/gb28181/controllers/... ./app/gb28181/snapshot/... ./app/utils/logging/... \
  ./internal/loggingacceptance/ ./internal/loggingcatalog/

cd ../web && npx vitest run src/views/gb28181/realtime-log/
```

**判据**：
- `go build ./...` 干净（`go build -tags logging_acceptance ./...` 也要干净）
- 后端全绿。~~`controllers` 的既有红灯 `TestLoggingGBHTTPContextWiring`~~
  → **C08 已修**（`cascade/controller/management.go` 把 `*gin.Context` 当 `context.Context` 传，
  丢掉 `request_id`）
- `./internal/loggingacceptance/` 的**三条组件用例**必须真的跑起来（不是 skip）——
  C09.5 把它们移出了 `//go:build logging_acceptance`；剩下两条要 MySQL 的仍按 `UVP_LOGGING_MYSQLD` 跳过
- 前端 `realtime-log/` 下 13 例（`log-line.test.ts` 9 + `index.test.ts` 4）全过

⚠️ **别并发跑全量**：根包 `bootstrap.newCache()` 会真连开发 Redis，
`-p 2` 会误报 `context deadline exceeded`（看着像回归其实不是）。全量要 `-p 1`。

---

## L3 日志形态（~1 分钟，看文件）

```bash
tail -30 server/resource/logs/uvp-gb28181.log
```

**逐条核对**（这是「人读可读」的验收清单）：

- [ ] 时间：`2026-09-15 08:32:32.499` —— **本地时间、无 `T`、无时区后缀**
- [ ] 等级：`INFO` / `WARN` / `ERROR` 大写且**对齐**
- [ ] logger 名：`play` / `access` / `hook` 等**对齐到同宽**
- [ ] 字段：`k=v` 形式，非 JSON
- [ ] 顺序：`device_id` → `channel_id` → `node_id` → `stream_id` → `request_id` → …（两张排序表已对齐）
- [ ] **一条日志一行**，无折行、无 ANSI 转义
- [ ] 空值字段**不出现**（不是 `k=""`）

---

## L4 场景实机（~5 分钟，本轮核心）

### 准备：拿 token

```bash
TOKEN=$(curl -s --noproxy '*' -X POST http://127.0.0.1:8280/api/login \
  -H "Content-Type: application/json" \
  -d '{"username":"admin","password":"admin123"}' \
  | python3 -c "import sys,json;print(json.load(sys.stdin)['data']['accessToken'])")
echo "$TOKEN" > /tmp/uvp_token.txt
```

（仅当 `config.yml` 的 `captcha.open: false` 时可直接登录）

拿候选设备与通道：

```bash
curl -s "http://127.0.0.1:8280/api/gb28181/device/list?page=1&pageSize=5" \
  -H "Authorization: Bearer $TOKEN"
curl -s "http://127.0.0.1:8280/api/gb28181/device/<deviceId>/channels?page=1&pageSize=5" \
  -H "Authorization: Bearer $TOKEN"
```

### 场景 1 · HTTP 停播（**调用方手上有值**）

```bash
# ① 点播，记下返回的 streamId
curl -s -X POST "http://127.0.0.1:8280/api/gb28181/play/<deviceId>/<channelId>" \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" -d '{}'
# ② 停播
curl -s -X DELETE "http://127.0.0.1:8280/api/gb28181/play/<streamId>" \
  -H "Authorization: Bearer $TOKEN"
# ③ 看日志
grep -E "stop_requested|stop_completed" server/resource/logs/uvp-gb28181.log | tail -2
```

**判据**：停播返回 `released: true`；日志两行都带 `device_id=` **且** `channel_id=`。

### 场景 2 · 无人观看断流（**调用方传空 → 验兜底链**）

```bash
curl -s -X POST "http://127.0.0.1:8280/api/gb28181/play/<deviceId>/<channelId>" \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" -d '{}'
# 什么都不做，等 ZLM 判定无人观看（实测约 30 秒）
sleep 40
grep -E "none_reader|无人观看" server/resource/logs/uvp-gb28181.log | tail -2
grep "stop_requested" server/resource/logs/uvp-gb28181.log | tail -1
```

**判据**（这是 A′ 兜底设计的**唯一实机证明**）：
- hook 那行的 `request_id` 与随后 `stop_requested` 那行的 `request_id` **一致**
- `stop_requested` 的 `device_id` / `channel_id` **非空**
  —— hook 调用时传的是 `"", ""`，字段只可能来自 `Stop` 内部兜底链

### 场景 3 · 功能回归（**必须做**）

```bash
curl -s "http://127.0.0.1:8280/api/gb28181/play/<streamId>/monitor" \
  -H "Authorization: Bearer $TOKEN"
```

**判据**：`{"code":1,"data":null,"message":"流不存在"}` —— 流真的停了，不是"日志写对了但流没停"。

### 场景 4 · 对账路径（**本机默认不可测**）

点播对账 reconciler 由 `config.yml` 的 `reconcile_interval_sec` 控制，
**当前值为 0 = 未启用**（启动日志会明确打印「点播对账 reconciler 未启用」）。
要实机验证需临时调大于 0，或接受**单测覆盖**（`TestT6_6_Q5LocationMissAllOffline` 已断言透传）。

---

## 负面判据（防"假达标"）

```bash
# ① 空值是否被渲染成空串（看着像带了字段，其实没值）
grep -c 'device_id=""' server/resource/logs/uvp-gb28181.log     # 必须 0

# ② 目标事件是否仍有"无定位字段"的漏网之鱼
tail -n +<基线行号> server/resource/logs/uvp-gb28181.log \
  | grep -E "event=gb28181\.play\." \
  | grep -vE "device_id=|node_id=|platform_id="              # 应为空
```

⚠️ **这两个比正向判据更重要**。字段"缺席"容易被发现，字段"空串"会被统计脚本和门禁当成已达标 ——
那才是最难查的假阳性。

**③ 被脱敏规则误吞的字段** —— 见 L3.2。它是同一类假阳性的另一半：
字段"被替换成省略标记"同样会被当成"已带字段"。

---

## 附：本轮（C04）实测记录 · 2026-09-15

| 层 | 结果 |
|---|---|
| L1 门禁 | 13 条 findings，**与改动前逐字一致**（零新增） |
| L2 自动化 | `build` ✅；`play`/`reconciler`/`handler`/`snapshot`/`logging` 全绿；前端 13/13 ✅ |
| L3 形态 | 人读格式生效（本地时间、等级/logger 对齐、`k=v`、字段顺序正确） |
| L4 场景 | **场景 1 ✅**（HTTP 停播带 `device_id`+`channel_id`）<br>**场景 2 ✅**（hook 传空 → 兜底链补出 `37010301021180000007` / `34020000001320000020`）<br>**场景 3 ✅**（停播后 `流不存在`）<br>场景 4 ⏸ 对账未启用 |
| 负面判据 | `device_id=""` **0 条**；16 条 play 事件**全部**带定位字段 |

**结论**：C04 的停播定位改动在**两条真实路径上**都得到验证 —— 有值的走传参、传空的走兜底链，
且功能行为未变。静态盲区（`stopLogFields()` 让扫描脚本看不见字段）与运行时事实的差异，
由场景 2 的实机日志正式证伪。

> ⚠️ 上表是 **C04 当时的快照**（L1 那时是"13 条 findings、零新增"）。C05–C08 之后
> L1 的判据已改为 **0 findings**（见本文 §L1）。历史数字留着是因为它记录了
> "门禁从 13 条一路清到 0" 的过程，别把它当成当前基线。

---

## 附：C09 实测记录 · 2026-09-15

| 层 | 结果 |
|---|---|
| L1 门禁 | ① `TestLoggingPolicyRepository` ✅ 0 findings（**未动 policy.go**）<br>② `internal/loggingcatalog` 新增 ✅ 0 findings（**首次运行是 6 条**，见下） |
| L2 自动化 | `go build ./...` ✅；`go build -tags logging_acceptance ./...` ✅；`handler` ✅；`internal/loggingacceptance` **3 条组件用例不再 skip**（C09.5） |
| L3 形态 | 不适用（本轮不改输出形态） |
| L4 场景 | 不适用（本轮是门禁，只在 `server/app/gb28181/handler/hook.go` 补了 4 处 `source_ip`，用单测覆盖） |
| 负面判据 | **登记册门禁必须"能红"**：新事件名、新字段名、删掉某事件最后一个定位字段——三类各跑一次，都确实报 findings 后才算合格（`TestLoggingCatalogRules` 13 例钉住） |

**C09 的 L1 首次运行不是 0，是 6** —— 这才是它有价值的地方：

```
app/gb28181/handler/hook.go:1060  locating_field_missing  → 补 source_ip
app/gb28181/handler/hook.go:1098  locating_field_missing  → 补 source_ip
app/gb28181/handler/hook.go:1165  locating_field_missing  → 补 source_ip
app/gb28181/handler/hook.go:1260  locating_field_missing  → 补 source_ip
app/gb28181/recording/catalog_scheduler.go:81  locating_field_missing → 登记为例外
app/gb28181/zlm/heartbeat/watcher.go:28        unresolved_event       → 判定归属 loggingcontract，不在此处报
```

4 处 `source_ip` **不是新增认知**：同一份 `hook.go` 的 `keepalive.*` 早就这么写并附了理由。
门禁把它翻出来的方式是**把同一事件的每个调用点摆在一起**——
"同一份代码里两处不一致"这种问题，人工 review 几乎必然漏掉。

---

## 附：C03 实测记录 · 2026-09-15

| 层 | 结果 |
|---|---|
| L1 门禁 | ① `TestLoggingPolicyRepository` ✅——**先红后绿**：3 条 `legacy_exception_mismatch`（+3 条连带 `missing_event`）+ 1 条 adapter SHA 失效，全部是"改等级"的连带义务<br>② `internal/loggingcatalog` ✅ 0 findings（**含新规则 `event_level_divergence`**，基线 0） |
| L2 自动化 | `go build ./...` ✅；`go test -p 1` 受影响包全绿：`loggingcatalog` / `ptz` / `casbinhelper` / `service` / `gormhelper` / `middleware` / `handler` / `play`(含 reconciler) / `gb28181/controllers` / `controllers` |
| L3 形态 | 不适用（本轮不改输出形态，只改等级；控制台里同样一条消息的等级列变了，版式不变） |
| L4 场景 | 不适用（等级改动没有可实机复现的新路径，用单测 + 复扫覆盖） |
| 负面判据 | **「同一 event 跨等级」必须能红**：新增夹具 `one event logged at two levels` 断言报 `event_level_divergence`；另一条 `…at one level across call sites is clean` 断言同等级多处**不报**（否则规则会退化成"报重复"）。`TestLoggingCatalogRules` 15 例 |

**复扫结果（脚本口径 351 个调用点）**：

```
等级分布  Warn 150 → 133     Info 106 → 121     Error 71 → 70     Debug 24 → 27
Warn 占比  43% → 37%        业务域 Warn 52% → 47%        平台支撑域 20% → 12%
分桶变化  ptz 91%→58% · casbin 33%→0% · db 20%→0% · play 30%→25% · cascade 72%→66% · gb28181 49%→46%
未变      zlm 19 条一条没动（17 条装配链降级 + 2 条写入失败，都是 WARN 的标准情形）
```

⚠️ **口径外还有 3 条**（`bootstrap.go` 的无字段 `Warn`）也降了 `Info`，但它们
**不进扫描口径**（`calls_in()` 要求段内有 `zap.`），所以指标上看不见 —— 复核靠：

```bash
grep -nE "已进入停止流程|已运行,忽略重复启动|尚未配置,跳过 SIP" app/gb28181/bootstrap.go
# 三条都应是 app.ZapLog.Info(...)
```

**已知失败（非本轮引入）**：

- `app/gb28181` `TestZLMManagementT14_ControllerInstallAndTeardownAreRepeatable` panic
  —— C09 已登记为既有红灯（`git status` 确认该文件本轮未动）
- `app/utils/logging` 的 `runT15Load` 在本机 120s 超时 —— `load_test.go` **未被本轮改动**，
  是负载测试对机器资源的敏感（同一次会话里整包 `go test` 曾被直接 SIGKILL）

---

## 附：C03.③ `ERROR` 复核实测记录 · 2026-09-15

| 层 | 结果 |
|---|---|
| L1 门禁 | ① `internal/loggingcatalog` **先红后绿**：改完立刻红 1 条 `unregistered_event`（新事件名 `http.operation_rejected`）—— 这正是门禁该有的反应；判定完成后才跑生成器，diff **只**新增该事件（`route`/`source_ip`）<br>② `loggingcontract` ✅ **未受影响**（见下） |
| L2 自动化 | `go build ./...` ✅；`go test -p 1` 受影响包全绿：`controllers`（含改过的 `logging_base_test.go`）/ `middleware` / `models` / `scheduler` / `service` / `casbinhelper` / `ginhelper` / `gormhelper` |
| L3 形态 | 不适用（只改等级，控制台版式不变） |
| L4 场景 | 不适用（等级改动没有可实机复现的新路径） |
| 负面判据 | **「默认 400 不得报 ERROR」必须能红**：`TestLoggingBaseHTTPFailure` 加了成对断言 —— 不传状态码时 event 必须是 `http.operation_rejected` 且 `"level":"info"`；传 `http.StatusInternalServerError` 时必须是 `http.operation_failed` 且 `"level":"error"`。两条各锁一支 |

**复扫结果**：

```
等级分布  Error 70 → 20      Warn 133 → 178      Info 121 → 126      Debug 27 不变
调用点总  351（不变）        唯一 event 330 → 331
Warn 占比 37% → 50.7%   ← 45 条从 ERROR 桶移进来，调用点总数没变：转移，不是通胀
分桶      业务域 47% → 54%（135/248）· 平台支撑域 12% → 42%（36/85）
```

**⚠️ 这次没有碰 `loggingcontract`，原因是可复用的:**

C03 主体连带红了两处，本轮两处都没红，差别只有一句话 ——
**连带义务只在"改的是被豁免的调用点"时才触发**：

| | C03 主体 | C03.③ |
|---|---|---|
| 改的调用点有静态 `event` 吗 | 有 3 条**没有**（`bootstrap.go` 的无字段 `Warn`） | **全都有** |
| 命中 `reviewed_legacy.json`（按 `method` 匹配） | 是 → `legacy_exception_mismatch` | 否 |
| 碰了 `reviewed_adapters.json` 封条文件 | 是（`gormhelper/log.go`） | 否（只碰了同目录的 `client.go`） |

**⚠️ 根包 `go test .` 恒红，与日志无关**：`bootstrap/init.go` 的 `init()` → `startupFail` →
**`os.Exit(1)`（第 214 行）**。测试环境下没有 `config.yml` / DB，`init()` 在测试体之前
直接退出进程 —— 所以 `go test .` 只有一行 `startup.failed` JSON 然后 FAIL，
**没有任何 `--- FAIL` 行**。判定依据：`main.go` 只是 `_ "…/bootstrap"` 导入，
`init()` 不读 `main.go` 的内容，故与等级改动无关。

**已知失败（非本轮引入）**：

- `app/gb28181` `TestZLMManagementT14_BuildsTypedCore…` 与 `…_ControllerInstallAndTeardownAreRepeatable` panic
- `app/gb28181/migration` **[build failed]**（陈旧符号）
- `app/utils/logging` 的 `runT15Load` 本机超时

---

## 附：C03.④ 假阴性复核实测记录 · 2026-09-15

| 层 | 结果 |
|---|---|
| L1 门禁 | ① `internal/loggingcatalog` ✅ 0 findings（**没有新增事件名**；`event_level_divergence` 不受影响 —— 5 条改动的 event 各自只有一个调用点）<br>② `internal/loggingcontract` ✅ 0 findings —— **本轮零连带**（原因见下） |
| L2 自动化 | `go build ./...` ✅；`go test ./app/service/ ./app/gb28181/sip/` ✅（本轮改动的两个包）；改动的 3 个文件 `gofmt -l` 干净 |
| L3 形态 | 不适用（只改等级，控制台版式不变） |
| L4 场景 | 不适用（等级改动没有可实机复现的新路径） |
| 负面判据 | **"升级不得靠感觉"**：3 条升 `ERROR` 各有一句可引用的硬证据 —— `uac_init_failed` 是同一段注释里的"点播也不可用"；`login_audit.persist_failed` 是 `operationlog.go` 里"全仓唯一一类"的**反面**（同类第二条）；`login_audit.panic` 是另外 3 条 panic 全为 `ERROR`。**判不出来就不升** —— 另有 6 条候选被明确判为"故意不升"，理由同样写进代码注释 |

**复扫结果**：

```
等级分布  Warn 178 → 173     Error 20 → 23     Info 126 → 128     Debug 27 不变
调用点总  351（不变）        唯一 event 331（不变）
分桶      业务域 135/248 → 132/248 = 53%    auth 3 → 1    gb28181 81 → 78
```

**"零连带"是 §八 规律的第二次验证**：C03 主体因为改了 3 条**无静态 `event`** 的调用点
（`bootstrap.go`）而红两处；C03.③ 与 C03.④ 改的调用点**全都带静态 `event`**，
两次都没红。→ **改等级前先看一眼"这条在豁免清单里吗"**，就能预判要不要同步基线。

**⚠️ 顺带修掉一个会让指标虚高的口径漏洞**：`scan-logging.py` 的 `SKIP_DIRS`
原先不含隐藏目录，而仓库约定"跨分支另建 worktree"会落在 `.claude/worktrees/<name>/`
—— 那是**整份 `server/` 的拷贝**，被一并计入扫描：**486 vs 真实 351**
（唯一 event 331 → 358、唯一字段 161 → 200，看起来像"前几轮成果全部回流"）。
已改为一律跳过 `.` 开头的目录。**复现方法**：在有 worktree 的工作区跑一次扫描，
数字会比 `--root` 指到干净目录时明显偏大。

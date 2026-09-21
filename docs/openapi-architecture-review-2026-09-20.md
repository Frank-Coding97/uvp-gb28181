# OpenAPI（开放平台 / AK-SK 机器客户端）架构评审

- 评审对象：`server/app/openapi/**`（9,013 行生产 Go）+ `api/openapi-v1.yaml` + `web/src/views/gb28181/openapi-client/**`
- 评审基线：`develop` @ `310b9f24`
- 评审日期：2026-09-20
- 视角：架构师（安全边界 / 契约 / 授权模型 / 可观测 / 交付闭环 / 代码卫生）

---

## 0. 总评

**安全内核是这套产品里最扎实的一块（高于同行平均，接近金融级）；但"能不能用、能不能运营、能不能交付"这三面都还是半成品。**

一句话概括问题类型：**这不是设计得不好，而是"设计跑在实现前面"** —— 契约、UI、库表、授权边界都按"数据范围"这个维度铺好了，读路径却没接；验收测试本来能拦住，但它自己编译不过。

| 维度 | 评级 | 判据 |
|---|---|---|
| 认证协议（HMAC v1 / 防重放 / 防跨环境重放） | **A** | 10 行 canonical 全字段校验、nonce 持久化 660s、±300s 偏移、body 原字节哈希 |
| 密钥与凭据管理 | **A** | 只在创建/轮换一次性返回、AES-256-GCM + AAD 绑 `(clientID, ak, version)`、主密钥只走 env |
| 失效链 / 并发正确性 | **A-** | `SecretVersion/AuthEpoch/ScopeEpoch/DeviceEpoch` + `row_version` CAS，管理变更与准入同事务线性化 |
| 失败关闭（fail-closed）一致性 | **A** | schema 探测、时钟回拨冻结、审计写失败即 503、禁用即 503 |
| 契约工程 | **A** | OpenAPI 3.0.3 + 契约测试 AST 解析真实路由注册表逐条对账（实测通过） |
| 后端代码规模纪律 | **B+** | 生产 9,013 行 / 测试 17,281 行（1.9:1） |
| 授权模型（scope + owner dept） | **B** | 模型清晰，但 `data_scope` 只到管理边界，未落到查询 |
| **数据范围（data scope）** | **D** | UI 可选、创建时授权、落库、接口返回；**读路径完全不读它**；验收测试编译不过 |
| **可观测 / 可运维** | **D+** | 全包零日志零指标；被拒请求（未过 HMAC）**不进审计**；唯一的诊断收集器无人调用 |
| **交付 / 上线闭环** | **D** | `deploy/` 全目录无一处 openapi；主密钥 env 只在未纳管的 `.vscode/launch.json`；缺 env 则整个后端 panic |
| 能力面 vs 限流面匹配度 | **C+** | 6 个只读接口 + 无推送；默认 10 rps 的轮询模型撑不起大客户 |

---

## 1. 设计上做对了什么（这部分建议保持）

### 1.1 签名协议：把"能被利用的模糊地带"全部堵死

`api/openapi-v1.yaml:17-95` 把 canonical 规则写成了机器可读的契约，实现严格对齐：

- 10 行 canonical、LF 分隔、末行无 LF；`hmacKey` 明确要求 **base64url 解码成 32 字节**再 HMAC（`auth/canonical.go:113-125`、`auth/signature.go:53-59`）
- **audience 参与签名且部署固定、客户端不可提供** —— 这是防"同一套 SK 跨环境重放"的关键，绝大多数自研 API 都没做
- body 哈希"按收到的原始字节"算，禁止反序列化再序列化（`canonical.go:23-25, 113`）
- 重复 header / 逗号合并 header / 重复 query key / 空 key / 缺 `=` / `;` / 原始 `+` / 非法 percent / 非法 UTF-8 一律拒绝（`canonical.go:130-194, 292-363`）
- path 只允许 ASCII 字面量，拒绝 `%`、`\\`、`.`、`..`、双斜杠、尾斜杠（`canonical.go:263-285`）

### 1.2 密钥不出网关

- SK 只在 create / rotate 响应里出现一次（`api/openapi-v1.yaml:12-16`、`controllers/client.go:112,180`）
- 落库是 AES-256-GCM，且 **AAD 绑定 `clientID + AK + secretVersion`**（`client/secret.go:73-77`）—— 换行、换版本会解密失败，防"密文调包"
- 主密钥只从 `UVP_OPENAPI_MASTER_KEY` 读、要求严格 32 字节 base64url，用完立刻 `clear(key)`（`routes/runtime.go:55-60`）
- `models.Client` 的密文字段全部 `json:"-"`（`models/models.go:47-51`）

### 1.3 管理变更与准入在同一事务里线性化（这套设计里最有技术含量的部分）

- 每次请求：`Admit` 在一个事务内 **锁 client 行 → 校验 `SecretVersion`/`AuthEpoch` → 校验 scope 的 `ScopeEpoch` → 复检资源归属 → 插 nonce（唯一键冲突 = 重放）→ 插审计"started"**（`auth/admission.go:94-131`）
- 管理侧所有变更（改 scope / 停用 / 撤销 / 轮换）都 **先 CAS client 行（`WHERE id=? AND row_version=?`）再写 scope**，与准入共用同一把锁序（`client/service.go:389-403, 546-560`；`scope_batch.go:40-83`）
- 结论：**"权限刚被收回、请求正在飞行"这个竞态在数据库层被消掉了**，不靠内存缓存或者 sleep。`scope_batch.go` 用嵌套 savepoint 保证整批 scope 原子替换，任一条失败全回滚

### 1.4 fail-closed 不是口号，是处处实现

- 启动即探测真实列而非表存在（`routes/runtime.go:126-143`）
- `request_timeout_seconds` 与 `httpserver.write_timeout` 的关系被强制校验（`runtime.go:52-54`）
- 审计写失败 ⇒ 已成功的读取也被改判 503（`auth/gateway.go:285-288`、`audit/store.go:45-47`）
- 墙钟回拨 ⇒ 准入永久冻结直到重启（`auth/admission.go:61-73`），且这是有意的
- 网关禁用时公开路由返回 503 而非 404，且契约测试断言"每条契约路径都必须是已安装的 fail-closed 路由"（`integration/contract_test.go:600-620`）

### 1.5 契约即代码

`TestOpenAPIContract`（`integration/contract_test.go:192-289`）做了四件事，实测通过：

1. yaml 里的 `paths` 数量与 `publicContractRoutes` 逐一相等，`x-uvp-scope` 必须等于 `client.SupportedScopes()`
2. **AST 解析 `routes/public.go`**，确认真实注册的 path/method 与契约完全一致（`:529-560`）
3. 真实装配 `InstallPublicBoundary` 并断言每条路径在网关为 nil 时返回 503 + `no-store`（`:600-620`）
4. 校验返回 DTO 白名单（`checkPublicDTOs`）

`/openapi` 命名空间由 root 上的拦截中间件独占，**注册时机早于 CORS / 业务日志 / 全局超时**（`routes/routes.go:83` vs `:88,118,124`）⇒ 对外 API 流量天然不吃 CORS、不吃业务操作日志、不吃 30s 业务超时，并且不会把 AK/SK 漏进访问日志（`app/utils/ginhelper/ginhelper.go:58-90` 把路径脱敏成 `/openapi`）。

### 1.6 数据层

- 6 组迁移 × 3 方言（MySQL/PG/SQLServer）× up/down 齐全
- 新增列同步进了三个全量快照（`uvp-gb28181.sql` / `postgresql_converted.sql` / `sqlserver_converted.sql`）—— 这一点符合本仓"快照必须同步"的硬约束
- `data_scope` 的迁移写了幂等守卫 + 非法值先收敛到最小权限，注释明确"避免升级后扩大既有凭证可见范围"

---

## 2. 必须修（P0）

### P0-1　数据范围（`data_scope`）只落库不生效：UI 承诺了，查询不认

**现象**：客户端详情里明确显示「本部门及以下」，创建对话框的描述是"访问归属部门及其所有下级部门的设备"，但**读取路径永远只按 `owner_dept_id` 精确匹配**。

**证据链**：

| 环节 | 位置 | 状态 |
|---|---|---|
| 前端选项与文案 | `web/src/api/gb28181-openapi.ts:32-35` | ✅ 有 `3=本部门 / 4=本部门及以下` |
| 创建对话框 | `OpenAPIClientCreateDialog.vue:117-124` | ✅ 可选、有说明 |
| 详情展示 | `OpenAPIClientDrawer.vue:214` | ✅ 显示 `dataScopeLabel()` |
| 管理接口 | `controllers/client.go:96,215` | ✅ 收、存、返 |
| 库表 | `2026-09-18-openapi-client-data-scope*.sql` + 3 快照 | ✅ 有列 + CHECK(3,4) |
| 创建授权 | `client/management_boundary.go:66-100` | ✅ 按 range 校验操作者权限 |
| **资源查询** | **`resource/resource.go:286-317`** | ❌ **只有 `d.owner_dept_id = ?`，从不读 `client.DataScope`** |
| **请求落地** | **`auth/metadata.go:99-117`** | ❌ `readMetadata` 只传 `m.owner`，`metadataInput` 里根本没有 scope 字段 |

`grep -rn "DataScope" server/app/openapi` 的结果：非测试代码里 `client.DataScope` **只在写入侧**（models 定义、Create、ClientView、管理边界授权）出现，**读取侧零引用**。

`resource.DepartmentScope` / `ErrInvalidDepartmentScope` / `DataScopeDepartment{,AndChildren}` 三个符号在 `resource.go:13-45` 定义了，但**只有 `resource_test.go` 引用**（生产代码无任何使用）。

**影响**：选「本部门及以下」的接入方**永远拿不到下级部门设备**。方向上是**收紧**（不会越权泄露），所以不是安全事件，但属于"界面承诺与系统行为不一致"，现场一定会被投诉；而且修复要动读路径 + 补齐 `*InScope` 方法，不是改个判断。

**同源问题（同一个提交引入）**：`26e3ce7f feat(openapi): 客户端数据范围可配置并落到管理边界`（2026-09-20 06:47），commit message 写着"资源过滤按范围收敛"，但 `resource.go` 该提交的 diff **只有 +28 行（常量 + struct + validate）**，`ResolveDepartmentIDs` 与 7 个 `*InScope` 方法从未实现。

**建议**：二选一，但必须选一个并同步两端 ——
- (a) 补齐读路径：`metadataInput` 增加 `dataScope`，`resource` 实现 `ResolveDepartmentIDs` + `*InScope` 家族，查询改 `owner_dept_id IN (...)`；
- (b) 暂时下线 UI 的 `4` 选项，并在 `x-uvp` 契约里注明"当前版本仅支持本部门"，等 (a) 排期。
**绝不能保持现状**（可选、可授权、可展示、但不生效）。

### P0-2　`app/openapi/resource` 测试包编译不过 ⇒ 隔离边界的验收测试全部失效

```
$ go vet ./app/openapi/...
vet: app/openapi/resource/resource_test.go:96:14: undefined: ResolveDepartmentIDs

$ go test ./app/openapi/resource/...
app/openapi/resource/resource_test.go:96:14:  undefined: ResolveDepartmentIDs
app/openapi/resource/resource_test.go:100:22: svc.ListDevicesInScope undefined
app/openapi/resource/resource_test.go:106:20: svc.GetDeviceInScope undefined
app/openapi/resource/resource_test.go:109:26: svc.GetDeviceStatusInScope undefined
app/openapi/resource/resource_test.go:113:23: svc.ListChannelsInScope undefined
app/openapi/resource/resource_test.go:117:22: svc.GetChannelInScope undefined
app/openapi/resource/resource_test.go:120:28: svc.GetChannelStatusInScope undefined
FAIL uvplatform.cn/uvp-gb28181/app/openapi/resource [build failed]
```

`resource_test.go` 共 8 个用例（`:90,130,168,179,196,217,250,268`），其中 **3 个是这套系统最关键的断言**：

- `TestOpenAPIResourceRejectsInvalidAndSharedDevices`（`:168`）—— 越权/共享设备必须取不到
- `TestOpenAPIResourceRejectsAmbiguousChannelRootAndOwnerMismatch`（`:179`）—— 根因不唯一、owner 不匹配必须拒绝
- `TestOpenAPIResourceSQLRechecksCurrentOwner`（`:250`）—— SQL 层必须复检当前归属

**这 8 个用例一个都没跑过。** 也就是说：整条"外部客户端 → 只能看到自己部门设备"的隔离断言，目前在 CI/本地**没有任何可执行证据**。而仓库的 GitHub Actions 处于 `disabled_manually`，所以没有任何环节会拦。

**建议**：
1. 先删掉 `resource_test.go:90-127`（引用未实现符号的那一段）让包能编译，把 3 个隔离用例恢复成红灯信号 —— 这是最快让"隔离无测试"暴露在流水线上的办法；
2. 然后按 P0-1 的方案 (a) 实现 `*InScope` 家族，把用例改回绿灯；
3. 给 `go vet ./...` 加进提交前/流水线（`go build` 是绿的，只有 vet/test 能发现测试包编译失败 —— 这次就是这么漏掉的）。

---

## 3. 应该修（P1）

### P1-1　第三方接入失败时，平台侧零线索

- `grep -rn "zap\.|log\." server/app/openapi --include=*.go | grep -v _test` ⇒ **空**。整个子系统没有一行日志。
- `Gateway.RejectedSummary()`（`auth/gateway.go:294-302`）是唯一的诊断出口，**全仓没有任何调用点**（`grep "RejectedSummary"` 只命中定义与其自身的 collector）。注释写着"不是公开路由"，但它也没有接到任何内部工具/启动日志/指标上。
- 审计表只在**过了 HMAC 之后**才写（`auth/admission.go:130`）。因此下面这些最常发生的接入失败**一条记录都不留**：
  - 签名算错 / AK 写错 / AK 被停用
  - 不在 TLS 白名单（P1-2）
  - 时间戳超 ±300s、nonce 重复
  - 被限流 429
- 唯一存在的"信号"是 `Cache-Control: no-store` 加一个 401 —— 而 401 对上述四种原因**用的是同一个 code 和 message**。

**后果**：接入方说"我调不通"，平台侧无法自证是对方签名错还是自己配置错；也无法发现"某个 AK 正在被暴力试签"。审计表里只有成功链路，等于**只有 happy path 的可观测性**。

**建议**（按性价比排序）：
1. 网关拒绝路径打结构化日志（只打 `requestId / scope / reason / source / clientID / AK 指纹前 8 位`，**绝不打签名与 SK**）。`audit.Audit.ReasonClass` 已有现成分级词表可复用。
2. 把"未过 HMAC 的拒绝"落一张轻量 durable 表或复用 `sys_openapi_audit`（`ClientID` 已是 nullable，`AKFingerprint` 字段已存在 —— **表结构本来就为这个场景留好了口子**）。
3. 管理端加"最近拒绝原因分布"面板，数据源就用 `RejectedSummary()`（它本来就是为此存在，只是没接线）。

### P1-2　TLS 边界与配置默认值互相打架，故障现象还和"签名错"一样

- `auth/transport.go:34-60`：`IsHTTPS` 只认两种情况 —— ① `r.TLS != nil`；② 有且仅有一个 `X-Forwarded-Proto: https` **且** 直连对端 IP 命中 `openapi.tls_terminator_proxies`。
- 但配置模板与本地配置都是 `tls_terminator_proxies: []`（`config.example.yml:69`、开发 `config.yml:178`），而开发配置又是 `enabled: true`。
- 结果：**默认配置下公开 API 恒 401**（`auth/gateway.go:115-118`），且这个 401 与"签名验证失败"（`:213-215`）**返回完全相同的 body**。

**这是当前最容易把现场拖上半天的问题**：装好了、`enabled: true`、AK/SK 都对、签名脚本也对，就是 401，而平台不告诉你为什么。

**建议**：区分 `AUTHENTICATION_FAILED` 与 "非受信 TLS 通道"（可以仍返回 401 保证不泄信息，但**必须落日志/指标**，并在管理端自检项里给出"当前部署是否具备 TLS 终止"的明确结论）。另外 `tls_terminator_proxies` 为空时建议启动期就 warn 一行 —— 现在完全静默。

### P1-3　限流 / 配额没有任何管理面，且是硬编码

- `RateLimit: 10 / Burst: 20 / ViewerQuota: 10` 三处硬编码在 `client/service.go:218-220`（创建时写入）。
- `grep "rate_limit|viewer_quota|\"burst\""` ⇒ 除创建外**没有任何 UPDATE 路径**；管理接口只有 `list/create/detail/scopes/rotate/enable/disable/revoke/audits/revocation-status`（`routes/admin.go:12-23`），没有 `PUT /limits`。
- 前端也没有入口（`index.vue` 表格只读展示，抽屉不可编辑）。
- 但 `ClientView` **对外暴露了这三个字段**（`client/service.go:57-59`）—— 表现为"平台能看到但改不了"。

**影响**：所有接入方共用 10 rps 上限。大客户接几千台设备轮询会直接撞 429，而运维**无手段**给单个客户端提额，只能改库。

**建议**：加 `PUT /gb28181/openapi-clients/:id/limits`（复用现有 `row_version` CAS + 管理边界授权 + 审计），并在前端抽屉里放开编辑。这是投入最小、现场收益最大的一项。

---

## 4. 可以缓（P2 / P3）

### P2-1　`audience` 这个必填签名项，没有任何交付通道

它参与签名（canonical 第 10 行），且明确"部署固定、客户端不可提供"（`api/openapi-v1.yaml:49-51`）。但：

- `ClientView` 没有 `audience` 字段（`client/service.go:47-65`）
- `controllers/` 里 grep `audience` 为空
- 前端 grep `audience` 为空；一次性密钥对话框（`OpenAPISecretDialog.vue`）只有 AK + SK

⇒ 给接入方开通时，必须由人**从 `config.yml` 手抄** audience、连同 base URL 一起线下交付。而它并不是秘密（本就是部署标识）。

**建议**：客户端详情里加一个只读「接入信息」块（base URL、`Sign-Version: 1`、audience + 一份签名示例），把 onboarding 从"人工抄配置"变成"界面自取"。顺带解决"没有面向接入方的可读文档"这个缺口（目前对外交付物只有 `api/openapi-v1.yaml` 和 `server/examples/openapi-sign/` 一份 Go 示例）。

### P2-2　上一代设计的死代码残留在生产目录里

- `app/openapi/controllers/device.go`（201 行）+ `channel.go`（92 行）：**没有任何生产接线**，只有 `controllers/metadata_test.go` 引用。
- 它们走的是 `DeviceController + OwnerDeptResolver` 这条已被 `auth.Gateway + resource.Service` 取代的路线 —— **同一个资源边界在同一个包里有两套实现**。
- 更麻烦的是它抛的错误码 `AUTH_REQUIRED`（`device.go:35,54,71` 等）**不在契约的 `code` 枚举里**（`api/openapi-v1.yaml:513`）。一旦有人误接上去，就立刻破坏契约。

**建议**：连同 `metadata_test.go` 里有关于它们的用例一起删掉（保留 `ClientAdminController` 的测试）。这是 293 行净减 + 消掉一个契约漂移源。

### P2-3　审计查询能力不足

`controllers/client.go:302-312`：`GET /:id/audits` 固定 `LIMIT 100`，**无分页、无时间范围、无导出**；清理策略是保留 30 天、每分钟最多清 500 行（`audit/store.go:64-86`，容量上没问题）。

**影响**：排障时"上周三这个客户端报了什么错"基本查不到。而一个对外 API 的审计，第一诉求恰恰是**可追溯**。

**建议**：加 `page/pageSize` + `from/to`，先不导出。

### P3-1　契约细节（对外观感 / 工具链）

| 项 | 位置 | 问题 |
|---|---|---|
| `securitySchemes` | `api/openapi-v1.yaml:407-412` | 把 `X-UVP-Signature` 声明成 `apiKey` 凭据，但真正的凭据是 `X-UVP-Access-Key`。用 codegen 生成客户端会得到**错的认证骨架**。建议 `name: X-UVP-Access-Key`，签名头另行文档化 |
| `/live-authorizations` | `api/openapi-v1.yaml:356-405` | 用 `x-uvp-enabled: false` + 完整 200 响应 schema 发布，同时注明"当前恒 503"。对外部读者是"既是接口又不是接口"。建议在 200 的 description 里显式写"保留契约，当前部署不启用"，或先不发布 200 schema |
| 405 响应 | `api/openapi-v1.yaml:154-156` | 未声明响应体，但 `routes/public.go:32` 实际会回 JSON。契约宽松（不算错，但 codegen 出来会有 warning） |
| `device:status` / `channel:status` | `api/openapi-v1.yaml:210,320` | 返回的 `status` 是 detail 响应的字段子集 ⇒ **6 个接口里 2 个是冗余的**。要么删，要么改成聚合接口（如 `GET /devices/status?ids=`），否则调用方要对 N 台设备发 N 次请求 |
| `MaxInFlight` | `routes/runtime.go:75` | 硬编码 64，**无配置项、也没有按客户端并发上限**。一个客户端慢请求可以占满全平台对外的并发槽 |
| 限流是进程内的 | `limit/rate.go:1-2` | 包文档已声明"只做 per-process 预算"，但**配额是持久化的**，两者同包容易出现理解偏差；多实例/重启后限流计数归零 |
| 时钟回拨冻结 | `auth/admission.go:61-73` | 设计如此（fail-closed，注释完整），但**没有任何运维恢复/告警路径**，NTP 跳变 = 对外 API 静默停摆直到重启 |

### P3-2　交付闭环是空的（最容易被现场踩到）

- `grep -rn "openapi" deploy/` ⇒ **空**。Linux 包、Windows 单机版、安装脚本、verify 脚本里都没有 openapi 段。
- `UVP_OPENAPI_MASTER_KEY` 全仓只出现在：测试文件、`server/app/openapi/routes/runtime.go:55`、以及 **`.vscode/launch.json`——而 `.vscode/` 并未纳入 git**（`git ls-files .vscode` 为空）。
- 缺口放大效应：`routes/routes.go:59-85` 里，一旦 `openapi.enabled: true` 而主密钥缺失/长度不对，`InitializeRuntime` 返回错误 ⇒ **`panic("OpenAPI initialization failed; ingress remains closed")`，整个后端起不来**（不是只关掉 OpenAPI）。

**建议**：把"启用 OpenAPI"做成交付清单里的显式步骤（env 注入位置 + `tls_terminator_proxies` 必须非空 + 生成主密钥的命令），并在 Linux 包的 `verify-package-on-centos7.sh` 里加一条 openapi 冒烟项。

### P3-3　能力面与限流面不匹配（路线图层面）

当前对外能力 = **6 个只读接口 + 1 个未启用的媒体申请**，全部是拉模型：

- 没有设备上下线/告警的推送（无 webhook / 无 SUBSCRIBE），集成方只能轮询
- 按默认 10 rps、`pageSize ≤ 100`：**5,000 台设备全量巡检一轮需要 ~8 分钟**，且这段时间完全占用该客户端的配额
- 没有 PTZ、没有录像回放、没有级联相关

这不是"设计错"，而是**契约已经把"服务端到服务端集成"这个定位说清楚了（`api/openapi-v1.yaml:5-9`），但能力面和配额面没有一起设计**。如果目标是"现场集成服务"，建议在路线图里明确：**要么给 status 加批量入参，要么补一个变更推送（webhook）**，否则限流会长期是接入方的第一诉求。

---

## 5. 建议的处理顺序

| 顺序 | 事项 | 理由 |
|---|---|---|
| 1 | **P0-2**：让 `resource` 包能编译 + 把 3 个隔离用例变成信号 | 最小改动，立刻恢复"隔离边界有测试"这个事实；否则后面所有改动都无验收 |
| 2 | **P0-1**：定方案（补读路径 or 先撤 UI 选项），两端同步 | 界面承诺与行为不一致是现场投诉源；且它是"授权模型"这个核心设计的最后一块拼图 |
| 3 | **P1-1**：拒绝路径落日志 + 接 `RejectedSummary` | 没有它，后面任何接入联调都在盲飞 |
| 4 | **P1-2**：TLS 边界的可诊断性 + 空白名单启动 warn | 现场最容易卡住的一步 |
| 5 | **P1-3**：`PUT /:id/limits` + 前端放开 | 让"提额"不需要改库 |
| 6 | P2-2 清死代码 → P2-1 接入信息块 → P2-3 审计分页 → P3-2 交付清单 | |
| 7 | P3-1 / P3-3 作为契约与路线图议题排期 | |

---

## 附录 A：本次核查用到的可复现命令

```bash
# 测试包编译状态（比 go test 更快更全：会编译所有 _test.go）
cd server && go vet ./app/openapi/...

# 隔离边界测试是否真的能跑
cd server && go test ./app/openapi/resource/...

# 契约一致性（实测通过）
cd server && go test ./app/openapi/integration/ -run TestOpenAPIContract -count=1

# 生产代码里 data_scope 到底有没有被读
cd server && grep -rn "DataScope\|data_scope" app/openapi --include=*.go

# 观测面：整个子系统有没有日志
cd server && grep -rn "zap\.\|log\." app/openapi --include=*.go | grep -v _test

# 唯一的诊断出口有没有被接线
cd server && grep -rn "RejectedSummary" --include=*.go .

# 死控制器
cd server && grep -rn "NewDeviceController\|NewChannelController" --include=*.go . | grep -v _test

# 交付链路有没有 openapi
grep -rn "OPENAPI\|openapi" deploy/

# 生产/测试代码规模
cd server && find app/openapi -name "*.go" ! -name "*_test.go" | xargs cat | wc -l
cd server && find app/openapi -name "*_test.go" | xargs cat | wc -l
```

## 附录 B：本次评审未覆盖的范围

- `app/openapi/media/**`（2,202 行）、`app/openapi/config/**`（702 行）、`app/openapi/processauthority/**`（807 行）只做了接线与可达性核查，**未做逐行设计评审**。三者中 media 全链依赖 `play_enabled: false`（默认关）与运维提供的 `revocation_bindings_file`，`processauthority` 是全平台共用的启动期基础设施（`main.go:109-124`），不属于 OpenAPI 独有。
- 未做性能压测 / 未验证多实例部署下的限流语义。
- 未评审前端 `index.vue`（919 行）的代码质量，只核对了它展示的承诺与后端行为是否一致。

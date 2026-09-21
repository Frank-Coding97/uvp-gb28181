# 图像抓拍端到端流程设计（2026-09-20）

> 目标：把「抓拍 → 落库 → 展示」跑通。本文只回答三件事——**入口放哪 / 图片怎么存 / 图片怎么展示**，
> 每一条都先给现状证据，再给结论。落地顺序见 §5。

## 0. 前提：这个仓里现在有**三套"抓拍"**，不先分清必然设计重复

| # | 名称 | 触发方 | 图片来源 | 落盘 | 现状 |
| --- | --- | --- | --- | --- | --- |
| ① | **播放器本地截图** | 操作员点播放器截图按钮 | 浏览器画布（EasyPlayer 自带 `btns.screenshot`） | **不落平台**，只下载到本机 | 已实现，`gb28181:play:snapshot` |
| ② | **通道最新快照** | 平台在播放开始时自动抓帧 | 平台侧拉 ZLM 流抓帧（`GetSnap`） | `<serverroot>/gb-channel-snapshot/<yyyy-MM>/<did>_<cid>.jpg`（**覆盖式**），URL 落 `gb_channel.snapshot_url` | 已实现 |
| ③ | **设备图像抓拍会话** | 操作员下发 `SnapShotConfig` | **设备自己拍、自己 POST 回来** | `<serverroot>/gb-device-snapshots/<sessionId>/<file>`（**内存会话**） | ⛔ 断的（见 §2.5） |

证据：①`PlayWindow.vue:52`（`gb28181:play:snapshot` 门禁 EasyPlayer 截图按钮）；②`app/gb28181/snapshot/{file,service}.go`
＋`gb_channel.snapshot_url`；③`controllers/device_snapshot.go`＋`devicecapture/registry.go`。

⭐ **业务上是两件事**，UI 不能混：
- **"我要一张图"** —— 一个动作，秒级出结果（可由 ①②③ 任一路提供）；
- **"我要设备按我的节奏拍一批"** —— 一个**任务**，要跑几十秒、要轮询、可能部分失败（只有 ③）。

---

## 1. 抓拍入口放哪里

**结论：分三个入口，各按"用户此刻的意图"放，不合并。**

### 入口 A：正在看画面 → 要一张图 ⇒ 播放控制台工具栏「截图」（维持现状）

- 现状：`PlayWindow.vue:167` 把 EasyPlayer 自带截图按钮挂在控制栏（`screenshot: canScreenshot`），
  门禁权限 `gb28181:play:snapshot`（`PlayWindow.vue:50-53`）。截图动作**完全在浏览器内**：
  库内实现是 `createElement("a")` + `a.download = ${Date.now()}.${ext}` + `click()`
  （`public/easyplayer/EasyPlayer-pro.js`，该函数附近无任何 `fetch/xhr/upload`），
  ⇒ 图**直接进操作员本机「下载」目录，平台零感知**（后端全 `app/` grep `screenshot` = 0 命中）。
- 建议：**保持，且不做"上传到平台"**。截图是"我此刻要一张图"的**个人动作**，落到操作员本机
  就是它的正确语义 —— 平台自己的菜单库也把它登记为「**播放器本地图像截图**」（`sys_menu` 140487），
  「本地」二字是平台既有的口径。
- ⛔ **已否决的旧建议**：曾写"可选增强：截图后提供『保存到平台』"。**撤回**。理由是它会
  ① 往平台图像资产里混入无设备来源、带画布叠加的个人剪贴图；② 需要新增上传接口；
  ③ 把权限码语义从"允许本机下载"改成"允许把画面存进平台"，管理员预期要重训 —— 收益不抵成本。
  若将来确有"把某一帧**存为证据**"的需求，应做成**独立的显式动作**（单独按钮 + 单独权限 +
  `source=evidence`），**而不是**把普通截图按钮改成上传：两者语义与后果完全不同。
- ⛔⛔ **不要把"设备抓拍会话"放回播放控制台工具栏**。它不是一个动作，是一个会跑几十秒、要轮询、
  可能部分失败的**任务**；放工具栏会让操作员以为"点完就有图"。这正是 2026-09-20 把它搬走的原因
  （见 `PlayConsoleLinked.vue:328` 的注释）。

### 入口 B：要配置设备 / 立刻让设备拍一批 ⇒ 设备管理 → 设备详情 → 「设备控制」页（维持现状）

- 现状：`SnapshotConfigPanel.vue` 与 `DeviceControlPanel` 同挂在抽屉「设备控制」tab 下
  （`device-mgmt/index.vue:2963-2972`），权限 `gb28181:device:snapshot`。
- ⛔ **必须一起做的两件收尾**（不改会持续误导）：
  1. 按钮文案「下发抓拍配置」→ **「立即抓拍」**。因为协议里 `SnapShotConfig` 必须同时带
     `SessionID` + `UploadURL`，它**本质是一次性任务**，不是"存在设备上的长期计划"。
  2. **权限码归属要捋**（两个码情况不同，别一起改）：
     - `gb28181:device:snapshot`（`sys_menu` **140450**「设备图像抓拍会话」）—— **确实错位**：
       它挂在父菜单 `/gb28181/multi-screen-playback`（多屏播放，id 140368）下，
       但面板实现已经搬进**设备管理 → 设备详情 → 设备控制**（`device-mgmt/index.vue:2963-2972`）。
       ⇒ 管理员在"多屏播放"里配这个按钮权限，配不到任何东西。
     - `gb28181:play:snapshot`（`sys_menu` **140487**「播放器本地图像截图」）—— **归属本身模糊**：
       按钮在播放控制台（`PlayWindow.vue`），而控制台是 `layout/index.vue:5` 里的**全局弹窗**
       （`PlaybackConsoleHost`），从设备管理页也能唤起，并不专属"多屏播放"。
       ⇒ 挂在多屏播放下不算明显错，但"控制台该归哪个菜单"应先定，再决定这个码挂哪。
- ⭕ 若将来要做"每天 8 点抓一张"（设备侧计划）——那是**另一个东西**，当前协议不支持，别硬塞进这个面板。

### 入口 C：找历史图 ⇒ 新增「图像库」（见 §3.3 与 §4 的待定项）

没有库时，图片只在会话面板里活几十秒——刷新页面就没了。

---

## 2. 设备上传的图片怎么存

**结论：私有目录 + 通道/日期分桶 + 独立入库 + 可配置保留期。四层缺一不可。**

### 2.1 ⛔ 先修收图接口（✅ 2026-09-20 已做，真机实测通过）

真机（海康 `37010301021320000002`）实测上传形态与平台接口**三处不兼容**（详见
`docs/gb28181-2022-master-backlog.md` 的「📌 E-3 真机收图形态」）：

| # | 设备实际 | 平台原来 | 实测 |
| --- | --- | --- | --- |
| 1 | `POST` | 只注册 `PUT` | gin 路由级 404 |
| 2 | 路径到 `/uploads/<token>/` 结束，**无文件名段** | `/uploads/:token/:filename`（必填） | 路由不匹配 |
| 3 | `multipart/form-data`（部件 `name="file"`） | 要裸 JPEG（首尾 `ffd8`/`ffd9`） | `ErrInvalidImage` 422 |

✅ **实际改法**：
- `routes.RegisterContentRoutes` 为 POST 注册 **catch-all** `…/uploads/*token`
  （⛔ **不能**靠 `:token` + gin 的 307 尾斜杠重定向 —— 307 要设备跟着重发，设备不跟随）；
- `resolveDeviceSnapshotUploadTarget` 统一解析 catch-all 的前导/尾斜杠与路径文件名，
  PUT 的具名参数形态同时兼容；
- 按 `Content-Type` 分流：`multipart/form-data` 走**流式** `MultipartReader`（不落临时文件），
  **文件名与 Content-Type 都从部件头取**（⛔ 不能从 URL 取 —— 41 位文件名就是"图像标识"，
  是与完成通知 `SnapShotFileID` 对账的唯一依据）；判定"是不是文件部件"看 `FileName()` 是否为空，
  **不死认字段名**（`name="file"` 只是海康自己的选择，不属协议）；
- 裸 JPEG 的老路保留（兼容 PUT）。

⭐ **真机端到端已实测通过**（不重启后端、用生产代码另起端点，配方见技能 §19.8）：
下发 `SnapShotConfig` 把 `UploadURL` 指向新端点后，设备回传
**136273 字节真 JPEG（2560×1440）**，落盘文件名 = 部件头里的 41 位标识
`37010301021320000002022026092014333016801.jpg`；token 读接口 `200 / 136273B / image/jpeg`，
错 token 与错文件名均 `404`。

### 2.2 落盘位置：搬出**静态根目录**（✅ 2026-09-20 已做）

⛔ 现状 `<serverroot>/gb-device-snapshots/...` 而 `serverroot = ./resource/public`，
被 `engine.Static("/public", ...)` **免鉴权**挂载。实测：
`/public/gb-channel-snapshot/2026-08/…jpg` → **HTTP 200，47255 字节真图**。
即"设备抓拍图"落在这里 = **token 鉴权被静态挂载整条旁路**：图片路径 = 静态根 + 固定两段
（`gb-device-snapshots/<sessionId>/<41位文件名>`），知道路径即可取图，不需要 token。

⚠️ **暴露面澄清（先前写重了）**：gin 的 `Static` **关闭了目录列举** —— 实测 `/public/` 与
`/public/gb-device-snapshots/` **均 404**。所以旧行为是"知道确切路径即可读"，**不是**"可遍历"。
迁移仍是必要的收敛，但不必按"已泄漏"的紧迫度处理。

✅ **实际做法（不新增配置项）**：`bootstrap.deviceCaptureBaseDir()` 取 `serverroot` 的**父目录**，
图片实际落 `<父目录>/gb-device-snapshots/<sessionId>/<file>`：
默认 `./resource/public` ⇒ **`./resource/gb-device-snapshots/…`**（不是 `./public` 的子目录）。
理由：天然不在 `Static` 挂载树里，又能随 `serverroot` 一起被运维改卷；
读取一律走 `GET /api/gb28181/device-snapshots/uploads/:token/:filename`（token 即凭证）。

⛔ 两个已踩的坑，改这里时必须避开：
1. `devicecapture.Registry.Upload` 里**已经**拼了 `gb-device-snapshots` 这一段
   ⇒ 基目录只能返回**它的父目录**，否则变成 `…/gb-device-snapshots/gb-device-snapshots/…` 重复一层。
   用例因此断言在**拼完之后的完整路径**上（`captureImageDirFor`），不是在基目录返回值上。
2. ⚠️ 通道最新快照（②）**仍在**同一公开根下（`snapshot.Service` 用 `serverroot` + `serverrootpath`，
   且 URL 直接落库到 `gb_channel.snapshot_url` 给前端当 `<img src>`）。收紧它要同时改读路径与存量库值，
   **本批未动**，留作 §4 的独立决策项。

### 2.3 目录布局：按 日期 + 通道 分桶（⏳ **本批未做，理由见下**）

现状 `<sessionId>/<设备文件名>`。原计划改成 `<yyyy-MM-dd>/<channelCode>/<41位设备文件名>.jpg`，
好处是"文件名自带毫秒级时间 + 按天分目录 ⇒ 保留清理就是删目录（O(1)）"。

⏳ **本批（2026-09-20）没改**，理由是"问题已经被入库解决了"：
- 原痛点写的是「`sessionId` 是内存 UUID，**重启后无法反查**」——
  ⇒ 本轮 `gb_channel_snapshot` 落了 `session_id` / `channel_code` / `rel_path` / `captured_at`，
  **任意维度都能反查到文件**，不再依赖目录名；
- 改目录布局的收益只剩"清理 O(1)"，而那个收益只有在做**保留期清理**（§2.5）时才兑现
  ⇒ 两者同批做才划算，单独改只是让磁盘上出现两套布局。

⭐ 真要做时，注意它**牵动三处**：`Registry.Upload` 的落盘路径、`Registry.FilePath` 的解析规则
（已集中在这一个函数里，就是为这次改动准备的）、以及**存量图的迁移**
（库里已落 `rel_path`，所以存量迁移是"按库行移文件"，不需要猜）。

### 2.4 入库：新增 `gb_channel_snapshot`，**不复用 `sys_affix`**（✅ 2026-09-20 已做）

已落地的表（`models/gb_channel_snapshot.go` + `migrations/2026-09-20-channel-snapshot-library*.sql`）：

| 字段 | 说明 |
| --- | --- |
| `id` | 主键（也是**稳定读图地址**的凭据：`/api/gb28181/device-mgmt/snapshots/:id/content`） |
| `device_id` / `channel_id` / `channel_code` | 归属（前两个是平台主键，查不到写 0 不阻塞收图；`channel_code` 是国标编码） |
| `session_id` | 抓拍会话标识（zlm/browser 来源为空，可空） |
| `file_name` / `rel_path` / `size` / `md5` | 文件事实（`rel_path` 是**相对**路径，正斜杠分隔） |
| `captured_at` | 拍摄时刻（**从文件名反解**，不合规时回落接收时刻） |
| `source` | `device` / `zlm` / `browser` —— ⭐ 三套抓拍汇到一张表（`browser` 当前不落库，见 §1 入口 A 的否决决议） |
| `created_by` / `created_at` / `updated_at` / `deleted_at` | 审计与软删 |

唯一键 `(channel_code, file_name)`：⭐ 设备重传同一张图（网络抖动很常见）走**冲突更新**，
天然幂等且会刷新 `size`/`md5`（磁盘文件被覆盖了，库行必须跟着变）。
⛔ 冲突更新时另按唯一键**回查一次拿 id**，不用 `Create` 回填的 `row.ID` ——
MySQL 的 `ON DUPLICATE KEY UPDATE` 回填值不保证是既有行主键，而前端要拿它拼读图地址。

⛔ 写入失败（表缺失/字段超长）时上传接口**返回 5xx**，不静默 200：
否则会出现"图片在盘上、缩略图也能看，但图像库里永远找不到这张图"这种没人会发现的状态。
设备重传是安全的（唯一键幂等），所以 5xx 是可恢复的。

⛔⛔ **建表收尾必须显式写 `COLLATE=utf8mb4_general_ci`**（2026-09-20 实际踩到并修复）：
`DEFAULT CHARSET=utf8mb4` 单独出现时 MySQL 取的是**该字符集的默认排序规则**（8.0 里是
`utf8mb4_0900_ai_ci`），**不是库的默认**（本库是 `utf8mb4_general_ci`，`gb_channel`/`gb_device`
也都是它）。两侧不一致时，`JOIN … ON ch.channel_id = s.channel_code` 直接报
**MySQL 1267 `Illegal mix of collations`** —— 建表时不报错，错误延迟到列表接口第一次查询，
日志里只有一行 `db.query_failed code=1267`。
⚠️ 只在**列对列**比较时炸：`列 = 参数` 按列的排序规则走，所以"表能建、收图能写、按 id 读图正常，
唯独列表接口 400"。三步修法（改 DDL / ALTER 已有表 / 补文本锚点）见技能
`uvp-device-config-family` §25；防回归锚点 =
`models/gb_channel_snapshot_ddl_test.go::TestChannelSnapshotDeclaresGeneralCollationOnMySQL`
（行为测试只跑纯 DML，方言 DDL 在 sqlite 上被跳过，覆盖不到建表收尾那一行）。

### 2.5 保留：复用现成的 `retentionDays` 模式（⏳ 同 §2.3，本批未做）

服务配置里已有 `sipTraceRetentionDays`、`positionHistoryRetentionDays` 的先例
（`controllers/service_config.go` + 单一维护循环）。⇒ 加 `snapshotRetentionDays`，
挂进同一个维护循环：**先删文件、再删库行**（行删失败下轮还能补；反过来会留下孤儿文件）。

⏳ 本批没做，因为它与"按天分目录"是同一件事的两半（见 §2.3），一起做才 O(1)。

---

## 3. 图片怎么展示

**结论：按"用户要找什么"分三层，而不是把所有图堆到一个列表里。**

### 3.1 通道当前画面（最高频）⇒ 通道列表 / 详情的缩略图

- 现状：`gb_channel.snapshot_url` 已在设备管理列表与详情三处渲染（`index.vue:463 / 2187 / 2570`）。
- 保留，但把语义钉死：这一列是「**最近一次快照**」，**谁更新就写谁**。
- ⭐ 建议增强：**设备抓拍成功时回写** `gb_channel.snapshot_url` ——
  优先级应高于 ZLM 自动抓帧（设备图是"设备视角的原图"，且带会话上下文）。

### 3.2 刚下发的那一批 ⇒ 会话面板内联缩略图 + 状态（修 3 件事）

- 现状：`SnapshotConfigPanel.vue` 已有 `session.files` 缩略图墙 + `statusText()`。
- 必修三件：
  1. ⏳ **会话是内存态**：`devicecapture.Registry` 只在内存里，刷新页面/重启后端即消失
     ⇒ 面板要能从库里按 `session_id` **重载**。**后端数据已就绪**（`gb_channel_snapshot.session_id`
     有索引，图还在就能重聚），缺的是接口与前端 —— 属第 3 步。
  2. ⏳ **部分失败语义**（对应 backlog E-5）：`receivedCount/snapNum` 要能表达"收了 2/3"。
  3. ✅ **图片 URL 要稳定**（2026-09-20 已做）：新增
     `GET /api/gb28181/device-mgmt/snapshots/:id/content`（按库行 id 取图，JWT + `gb28181:device:snapshot`）。
     ⛔ 前端应**立即改用它**：老路径 `/device-snapshots/uploads/<token>/<file>` 的凭证是**会话 token**
     （内存态，刷新页面/重启后端就失效），只够"刚下发那几秒"用。
     上传成功的响应体现在会带 `data.id`（`{"id":42,"name":"…jpg","size":136273}`），拿它拼即可。

### 3.3 找历史图 ⇒ 新增「图像库」（一级菜单）

- 形态建议：**时间倒序图墙 + 左侧通道树筛选**（复用设备管理已有的通道树），支持
  按通道 / 时间段 / **来源**（device·zlm·browser）筛选，单张查看（大图 + 元信息），多选导出。
- 入口 = **一级菜单**（§4 待定项 1 已批准，2026-09-20）。

**后端契约已就绪（2026-09-20）**，前端照下面这三点接即可：

| 项 | 值 | 说明 |
| --- | --- | --- |
| 菜单 | `path=/gb28181/snapshot-library`，`component=gb28181/snapshot-library/index`，`sort=55` | ⛔ component 是**前端组件路径契约**（`route-output.ts` 用 `import.meta.glob("@/views/**/*.vue")` 逐字比对）⇒ 页面必须落在 `web/src/views/gb28181/snapshot-library/index.vue`。⛔ `sort` **不能与同层任何菜单相同**（见下） |
| 列表 | `GET /api/gb28181/device-mgmt/snapshots` | 参数 `channelId`(平台主键) / `channelCode` / `deviceCode`(20 位编码) / `sessionId` / `source` / `from`,`to`(RFC3339) / `page`,`pageSize`(`≤200`)；返回 `{list,total,page,pageSize}`，按 `capturedAt` 倒序 |
| 取图 | 列表每行的 `url` 字段 | 即 `GET …/snapshots/<id>/content`（JWT + `gb28181:device:snapshot`）。**别自己拼**，用返回的 `url` |
| 权限 | `gb28181:device:snapshot` | 与抓拍会话面板**同码**：能看到"这次抓拍的图"的人就能看图像库 |

⛔ 三个容易接错的点：① 列表的 `deviceCode` 是 **20 位国标编码**，而本表内部的 `device_id` 是**平台主键**
（与设备列表接口里同名的 `deviceId`＝编码 不是一回事）；② 时间筛选打在 **`capturedAt`（拍摄时刻，
从文件名反解）**，不是落库时刻 —— 设备补传时两者可能差几小时；
③ **`sort` 不能撞车**：一级菜单排序走 `app/models/sysmenu.go` 的 `TreeSort()`，它用
`sort.Slice`（**非稳定排序**）且 `aSort == bSort` 时直接 `return aSort < bSort` = false ⇒
同 sort 的两项**顺序不确定**，表现是"菜单栏位置偶尔会变"。初版取了 `80`，撞上已有的
「SIP 接入信息」(80)，已改为 `55`（插在「云端录像」50 与「录像计划」60 之间 —— 图像库与云端录像
同属"历史媒体资源浏览"，语义同组）。

**响应契约（前端按此接，别猜）**：成功 `HTTP 200` + `{"code":0,"data":{list,total,page,pageSize}}`；
参数非法 **`HTTP 400` + `{"code":1,"message":"<原因>"}`**（`DefaultResponseHandler.Fail` 的默认值；
`source` 白名单、`from/to` 格式、`channelId` 非数字、`from>to` 都走这条），
**不是"200 + 业务码"**。⚠️ 之前一度按 200 记录 —— 那是测试里全局 `app.Response` 被同包 mock
泄漏出来的假象（见技能 §21.10）。

✅ **当前状态：菜单行与页面已在同一批次收口，并且菜单已落到开发库**（2026-09-20）。
页面在 `web/src/views/gb28181/snapshot-library/index.vue`，与菜单 `component` 逐字对应，
两者的一致性由 `index.layout.test.ts` 的跨端断言钉住（它直接读迁移文件比对）。
开发库（220 `uvp_gb28181`）已手工执行 `2026-09-20-channel-snapshot-library.sql`：

- 菜单行 id `140509`、两个接口 `sys_api` 589(列表)/587(取图)、`sys_menu_api` 绑定到抓拍按钮 140450、
  授权 `sys_role_menu` role 1、casbin 两条；
- ⛔ **不需要重启后端**：① 菜单由 `GET /sysMenu/getRouters` **每次查库**（无缓存），
  前端 route store 也不持久化 ⇒ **刷新页面即见**；② casbin 有
  `casbin.autoloadpolicyseconds: 120` 的自动重载协程 ⇒ 新规则最多 2 分钟自行生效。
  判"运行中的后端有没有这条路由"的零成本手法：**不带 token 打一下，401=有、404=没有**。

---

## 4. 要你定的事（都给了推荐）

| # | 待定项 | 选项 | 结论 |
| --- | --- | --- | --- |
| 1 | **图像库入口** | A 设备管理页加 tab / B 一级菜单「图像库」 / C 播放控制台侧栏 | ✅ **B**（2026-09-20 批准，菜单行已入库）——"跨设备找图"是一级任务；挂设备管理下会变成"先选设备再找图" |
| 2 | ~~本机截图要不要"保存到平台"~~ **已作废** | 要 / 不做 | **不做**（截图天然是"下载到操作员本机"的个人动作；见 §2 入口 A 的否决理由） |
| 3 | **图像库图片鉴权** | 带 JWT（私有目录）/ 免鉴权（公开目录，同现状） | ✅ **带 JWT**（已实现）——否则任何能访问 IP 的人都能遍历设备画面 |

---

## 5. 落地顺序（建议）

1. ✅ **收图接口兼容真机**（§2.1）—— **2026-09-20 已做并真机实测**：设备回传 136273B 真 JPEG，落盘文件名取部件头；
2. ✅ **私有目录 + 入库 + 稳定读接口**（§2.2、§2.4）—— **2026-09-20 已做**：
   私有目录 + `gb_channel_snapshot`（三方言迁移 + 三快照）+ 按库行 id 的读图接口，整链有端到端用例；
   ⏳ 同批**刻意没做**的：目录改按天分桶（§2.3）与保留期清理（§2.5）—— 两者是一件事的两半，一起做才 O(1)；
3. ◐ **面板从库重载 + 部分失败语义**（§3.2）—— **2026-09-20 做了"轻量版"**：
   会话面板加了「在图像库中查看本次抓拍」入口，`push` 到图像库并带上 `sessionId`
   （会话只活在内存 Registry 里、刷新即丢，图已落库 ⇒ `sessionId` 是事后重聚一次抓拍的唯一线索）；
   ⛔ 该按钮**先探测路由是否注册**（`router.resolve(...).matched.length`）再启用 ——
   菜单行没生效时 `push` 会落到 404 白屏，比置灰更难懂。
   ⏳ 仍**未做**：面板内直接按 `sessionId` 拉列表做原地重载、以及 E-5 的部分失败语义。
4. ✅ **图像库**（§3.3）—— **2026-09-20 全链完成，且菜单已落到开发库**：
   ① `GET /api/gb28181/device-mgmt/snapshots` 列表接口（筛选/分页/数据范围）；
   ② 一级菜单行（`sort=55`）+ 列表接口登记 + 角色授权（三方言迁移 + 三份全量快照）；
   ③ 前端页 `web/src/views/gb28181/snapshot-library/index.vue`（筛选/缩略图网格/大图预览/分页），
   并给抓拍会话面板加了「在图像库中查看本次抓拍」入口（带 `sessionId` 深链，见下）。
   ⛔ **图片鉴权走 `?token=`**：取图接口在鉴权组内，`<img>` 带不了 Authorization 头 ——
   前端统一经 `snapshotContentImageUrl()` 拼装，与 SIP 看板 SSE 同约定；
   ⛔ **前端页与菜单行必须同批次收口**（`component` 是前端组件路径契约），已由跨端断言钉住。
   ⛔ **菜单 `sort` 不能与同层撞车**（`TreeSort` 用非稳定排序、且相等时无 tiebreak ⇒ 顺序会漂）；
   初版 80 撞上「SIP 接入信息」，已改 55。
   ⏳ 仍未做：目录按天分桶（§2.3）与保留期清理（§2.5）。

⚠️ **本批改动的生效条件**：后端进程需重启（迁移在建表时自动应用）。
**图像库这一批是例外 —— 已手工打进开发库，不用重启**：菜单由 `getRouters` 每次查库
（前端 route store 不持久化）⇒ **刷新页面即见**；casbin 每 120s 自动重载 ⇒ 规则自行生效。
判"运行中的后端有没有某条路由"的零成本手法：**不带 token 打一下，401=有、404=没有**。
设备侧记住的仍是上一轮手工实验（`tmp/send_snapconfig.py`）下发的那次配置，其 `UploadURL`
指向早已关闭的实验端口 —— **修复后第一次经平台下发就会把它覆盖成正式地址**。

---

## 6. 「下发抓拍后设备不上传」的定因与修法（2026-09-20 第九轮）

**现象**：操作员在设备详情点「立即抓拍」，会话起了、operation 记 `accepted` + `device_result=OK`，
但**设备一张图都不上传**。

### 6.1 真凶：抓拍报文**一个字节都没发出去**

`gb_sip_trace_message` 取证（设备 `37010301021320000002`，三条 `snapshot_config` 操作
SN 10179 / 10181 / 10183，正文解密后**全部**是）：

```xml
<Control><CmdType>DeviceConfig</CmdType><SN>10183</SN><DeviceID>…</DeviceID>
<VideoParamAttribute Num="0"></VideoParamAttribute></Control>
```

平台发出去的是「把视频参数清空」，`SnapShotConfig` **从未发出** —— 设备回的那句
`Result=OK` 是对空视频参数配置的应答。**两侧日志全绿**，唯一症状就是"设备没上传"。

⛔ 根因：`snapshot_config` 这个 action 只是 `controllers/device_snapshot.go` 里的一个
**字符串字面量**，`ptz` 包完全不认识它 ⇒ 三处「按 action 分流」全部落到 A-5 视频参数分支：

| # | 位置 | 后果 |
| --- | --- | --- |
| 1 | `ptz/scheduler.go::buildScheduledPTZBody` | **实际发出的报文错**（直接原因） |
| 2 | `ptz/handler.go::OnPTZMessage` 的 `CmdDeviceConfig` 支 | ack 按 A-5 处理，派生的对账去回读 `VideoParamAttribute` |
| 3 | 由 2 派生的对账 | 与父 payload 的块一个都对不上 ⇒ 差异 0 条 ⇒ **判 read_ok = 没对账** |

### 6.2 修法：判据从「action 白名单」换成「payload 里装了什么」

`ptz/device_config.go` 新增 `ActionSnapshotConfig` + `deviceConfigOperationForm(action, payloadJSON)`：

- payload 里 **`blocks` 非空** ⇒ 按配置族处理（重建发 blocks、ack 走配置族分支）；
- action 声明自己属于配置族（`deviceConfigBlockActions` 名单）却**没有 `blocks`** ⇒ **报错**，
  不许回落成 A-5 —— 否则故障又会伪装成"设备没照做"；
- 其余 ⇒ A-5。

⛔ **为什么不再用 action 白名单**：白名单每新增一个 action 都会漏，而漏的后果极不对称
（报文错、两侧无错、症状模糊）。判 payload 这个**数据事实**之后，将来新增配置族 action
只要用 `blocks` 装 payload 就自动走对。
`scheduler.go` 与 `handler.go` **共用同一个函数**（同一件事不能有两份判定），
`controllers` 改用 `ptz.ActionSnapshotConfig` 常量。

### 6.3 第二个断点：`UploadURL` 由**浏览器 Host** 派生

`snapshotUploadURL` 取 `X-Forwarded-Host` / `Host`，而前端 dev server 的 `xfwd: true`
（`web/vite.config.ts`）会把原始 Host 透传过来 ⇒ **用 `localhost:5177` 打开平台**就会下发出
`http://localhost:5177/…`，设备把它解析成自己，**永远传不上来且平台不报错**。

已加门禁 `snapshotUploadHostUnreachableForDevice`：localhost / 回环 / 通配地址**当场拒发**
（503 + 文案让操作员改用局域网 IP），**域名一律放行**（平台判定不了它解析到哪，判错会挡住正常部署）。

⇒ ⭐ 现场操作口径：**用平台所在机器的局域网 IP 开平台**（当前 `http://192.168.10.120:5177`）。

### 6.4 防回归锚点（三条，均通过变异自检：精确红 + sha256 还原）

| 锚点 | 钉住什么 |
| --- | --- |
| `ptz.TestBuildScheduledPTZBodyBlocksPayloadNeverRebuildsAsVideoParamAttribute` | **性质测试**：带 `blocks` 的 payload 不许重建成 `VideoParamAttribute`；含一个不存在的 action 名代表"将来的新 action" |
| `ptz.TestOnPTZMessageRoutesSnapshotConfigAckToBlockFamily` | ack 必须派生 `configTypes=[SnapShotConfig]` 的对账（走错时得到的是 `refresh_video_params`） |
| `controllers/device_snapshot_upload_url_test.go` | 回环/通配拒发、局域网 IP 与域名放行（**内部包**，因 `snapshotUploadURL` 未导出） |

⛔ 变异注入有个坑：把 handler 的**参数**换成常量是**等价变异**（payload 有 blocks 时判据本就
与 action 无关），跑出来假绿 —— 必须**同时短路 payload 判据**才复刻出原缺陷。

---

## 附：本文依据的实测事实（可复现）

| 事实 | 证据 |
| --- | --- |
| 真机上传形态（POST + multipart + 无文件名） | `tmp/snap_upload_capture/requests.log`；配方见技能 `uvp-device-config-family` §19.7 |
| 平台三种请求的响应（两种 404） | `POST …/<token>/` → `404 page not found`；`PUT …/<token>/x.jpg` → `{"code":404,…}` |
| ✅ **改造后真机端到端通过** | 用生产代码另起端点（技能 §19.8 的 `scripts/snap_e2e/`）：设备回传 **136273B / 2560×1440 真 JPEG**，落盘名 `37010301021320000002022026092014333016801.jpg`（41 位）；读接口 `200/136273B/image/jpeg`，错 token 与错文件名均 `404` |
| `/public` 免鉴权 | `curl /public/gb-channel-snapshot/2026-08/…jpg` → 200 / 47255B |
| `/public` **不可目录列举** | `curl /public/` 与 `/public/gb-device-snapshots/` → **均 404**（gin `Static` 关闭列举）⇒ 暴露面是"知道确切路径可读"，不是"可遍历" |
| 两套权限码挂在多屏播放菜单下 | `sys_menu` id 140450「设备图像抓拍会话」(`gb28181:device:snapshot`)、140487「播放器本地图像截图」(`gb28181:play:snapshot`)，parent 140368 = `/gb28181/multi-screen-playback` |
| 播放器截图**不下发任何请求** | `EasyPlayer-pro.js` 内：`i.href=t; i.download=Date.now()+"."+ext; i.click(); revokeObjectURL(t)`；该函数附近 grep `fetch/xhr/upload` = 0 命中；后端 `app/` grep `screenshot` = 0 命中 |
| 播放控制台是**全局弹窗**、不专属某菜单 | `layout/index.vue:5` 常驻 `<PlaybackConsoleHost />`；`device-mgmt` 页也可唤起 |
| 抓拍面板当前挂载点 | `device-mgmt/index.vue:2963-2972`（设备详情抽屉「设备控制」tab） |
| 无快照图片表 | `SHOW TABLES LIKE '%snapshot%'` → 空（改造前）；现为 `gb_channel_snapshot` |
| 保留期可用现成模式 | `sipTraceRetentionDays` / `positionHistoryRetentionDays` |
| ✅ **入库与稳定读接口（2026-09-20）** | 迁移 `2026-09-20-channel-snapshot-library{,-postgresql,-sqlserver}.sql`（6 文件）+ 三全量快照追加 `channel-snapshot-library{,-permissions}:start/end` 块；用例 `models.TestChannelSnapshotPresentInEverySnapshot`（三快照）、`migration.TestChannelSnapshotLibraryPermissionMigrationIsScopedAndReversible`（sqlite 两遍幂等 + 不越权 + down 可逆）、`controllers.TestSnapshotContentServesImageByLibraryID`（上传→落盘→入库→按 id 取回原图） |
| 真机 40 位文件名（序列码不补零） | `manscdp.TestParseSnapshotFileIDReadsRealDeviceAnchors`：三张连拍为 41/40/40 位，且解出的时刻严格递增、间隔 ≈3s（与 `SnapShotConfig.Interval` 一致） |
| 越权防护 | `migration` 行为测试的种子含「只有 `ptz:view` 的角色」与「同 permission 但 `type=1` 的目录菜单」，两者都必须拿到 0 条 casbin 规则（注入 `ptz:view` 变异后该用例精确红） |

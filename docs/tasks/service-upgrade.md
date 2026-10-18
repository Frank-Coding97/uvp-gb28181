---
title: 平台服务升级任务拆解与测试清单
status: implementation-in-progress
created: 2026-10-15
spec: ../specs/service-upgrade.md
plan: ../plans/service-upgrade.md
---

# 执行基线与完成标准

老板确认继续，现从已评审技术方案进入 tasks 阶段。本文件同时承载任务、测试用例及后续执行结果，不另建测试或验收文档。
首版按 Linux amd64/arm64、绿色包、systemd 和单机 Compose 执行，包含在线、离线和预先授权的无人值守维护窗口；Windows 保持待定，不纳入当前实施清单。
本阶段已进入实现；下列尚未开始的任务仍是计划用例，已完成项必须记录实际测试证据，不能把计划用例当作通过。

当前实现进展（2026-10-16）：U02 的自建 HTTPS 清单检查已接入管理 API，支持 Ed25519 信任公钥、制品矩阵匹配、当前版本比较和持久 sequence 防重放；U03 已支持签名离线 tar 导入和受限暂存；U04 已有原子任务索引、互斥、幂等、阶段元数据和助手重启恢复；U05 已新增固定 v1 JSON Unix socket 助手、独立 `uvp-updater` 进程、注册绑定和无适配器时的安全拒绝；U06 已提供只读迁移计划，绿色包/systemd/Compose 迁移阶段均调用应用固定 `-migrate-up`；U10/U11 已有停服、备份、激活、候选版本身份校验、Compose `--pull never` 和回退适配器；U13/U14/U17 已有基础状态接口、在线/离线管理页面、应用与回退短请求入口及无人值守策略。真实 Docker daemon、Linux amd64/arm64、systemd、宿主重启、外置数据库恢复、candidate 业务禁写和完整权限 HTTP 回归仍未完成，不能据此宣称已完成全矩阵验收。

已执行证据：`cd server && go test ./internal/upgrade ./cmd/uvp-updater -race`、`go build ./...`、`bash -n deploy/standalone/uvp-gb28181-ctl.sh deploy/standalone/uvp-gb28181-service-install.sh` 通过；`cd web && pnpm exec vitest run src/api/service-upgrade.test.ts --pool=forks --maxWorkers=1` 通过。`pnpm exec vue-tsc --noEmit` 仍受工作区既有 firmware-repo/device firmware 测试类型错误阻断。

- 每项按 RED → GREEN → REFACTOR 执行，先运行能揭示缺失行为的测试，再做必要实现；任务完成须其测试通过并记录证据。
- 若前置能力尚不存在，测试失败应指向该任务的行为缺失，不能只把文件不存在、编译失败当作完整 RED 证据。
- 保留现有脏改动；涉及已被修改的文件时只提交本任务差异，不顺手格式化或提交其他工作。
- 正常路径、失败路径及恢复路径均需验证；模拟器通过不能替代真实部署、数据库恢复、浏览器或宿主重启证据。
- 任务内记录测试命令、环境、版本/构建摘要、结果和限制。缺少必要环境标为“未验证”，不能标记完成或降低验收标准。
- 发布域名、Registry、签名生产公钥及首批可升级来源构建在正式发布前落定；本地测试使用隔离 HTTPS 源、测试密钥与两份可识别的测试构建。

# 执行顺序

| 批次 | 任务 | 依赖与阶段出口 |
| --- | --- | --- |
| 1 | U01 协议与发布身份、U02 可信校验、U03 制品输入、U04 持久任务 | 顺序建立核心数据与状态；U03、U04 均以 U01/U02 合同为前提。 |
| 2 | U05 助手边界、U06 迁移规划、U07 备份恢复 | 助手依赖 U03/U04；迁移规划依赖 U01；备份依赖 U06。 |
| 3 | U08 维护与就绪、U09 安装接入、U10 本地激活、U11 Compose 激活 | 先 U08/U09；两类激活均依赖 U05–U09，不共享执行路径假设。 |
| 4 | U12 崩溃恢复、U13 管理 API、U14 管理页面 | U12 在两类激活后验证；API 依赖助手，页面依赖 API；最终均通过 U12 场景回归。 |
| 5 | U15 发布流水线、U17 无人值守策略、U16 全矩阵验证与交付 | U15 消费已稳定协议和打包产物；U17 依赖 U04/U08/U10–U14；U16 依赖 U01–U15、U17，在全部功能具备后执行，真实发布仍按发布授权单独进行。 |

顺序表示依赖，不要求同时启动多个 agent；同目录修改和数据库迁移操作保持串行。
每项预计代码位置是责任范围，实际新增文件名可随现有风格调整，不改变规格和测试目标。

# U01 — 升级合同、发布身份与测试夹具

范围：server/internal/upgrade 的类型与协议、构建时版本身份、前端构建标识和测试夹具；关联 scripts/version.mjs 的现有版本合同。
交付：固定 v1 清单、签名信封、组件身份、注册、任务和错误结构；配置/迁移兼容声明；区分正常升级与回退。依赖：无。

| 用例 | 描述与输入 | 期望输出 |
| --- | --- | --- |
| U01-1 | 同源构建 A/B，后端嵌入身份、前端元数据、版本清单 | 版本与 build_id 可交叉核对；B 不被识别为 A，改写 version.json 不能伪造嵌入 build_id。 |
| U01-2 | 稳定版、rc、未知协议版本、助手协议不足、错误 OS/架构 | 仅适配目标进入准备；返回明确不兼容原因，不执行命令。 |
| U01-3 | 来源迁移指纹或依赖数据格式不在允许集合，目标版本号较大 | 仍拒绝应用；不以版本号大小代替兼容判定。 |
| U01-4 | 同版本不同构建、低版本正常升级、兼容的显式回退 | 前两者拒绝；回退只进入独立预检流程，不直接执行。 |

# U02 — 清单签名、更新检查与下载来源

范围：server/internal/upgrade 的签名、清单解析、更新检查和 HTTPS 客户端。依赖：U01。
交付：Ed25519 原始 payload 校验、稳定通道 sequence 与有效期处理、来源和重定向限制，网络失败不伪装成无更新。

| 用例 | 描述与输入 | 期望输出 |
| --- | --- | --- |
| U02-1 | 可信签名、签名缺失、错误 key_id、单字节 payload 篡改、重复 JSON 字段、信封超过 1 MiB | 仅首项接受；其他返回校验错误，且从未进入执行阶段。 |
| U02-2 | 过期或未来异常时间、低于持久最高 sequence、相同 sequence 指向不同内容 | 拒绝异常清单并说明原因；最高序号只在完整可信检查后更新，合法重复检查幂等。 |
| U02-3 | stable 指向错误摘要的版本清单；旧公钥/新公钥过渡双签 | 摘要不符拒绝；至少一个内置信任公钥签名有效才可接受，不自动信任未知钥匙。 |
| U02-4 | 正常自建 HTTPS、连接失败、重定向至未授权主机或 HTTP、终端无 GitHub 网络 | 正常源可检查；失败与无新版本分别返回；违规跳转拒绝，无 GitHub 请求。 |

# U03 — 在线制品、离线导入与安全暂存

范围：server/internal/upgrade 的下载、归档解析、inbox/staging 和空间预检。依赖：U01/U02。
交付：可取消的后台获取、原始 tar.gz 与 .upgrade.tar 导入共用可信验证；不在业务可写目录执行内容。

| 用例 | 描述与输入 | 期望输出 |
| --- | --- | --- |
| U03-1 | 正确在线包、离线完整包，下载中断后重试、上传中途断开 | 完整包进入准备；半包标记未完成且不可应用，重试不误复用坏文件。 |
| U03-2 | 摘要错误、Content-Length 缺失/谎报、超限文件/总大小/数量、压缩膨胀 | 按实际读取限制拒绝，旧服务持续运行，临时数据受限并可清理。 |
| U03-3 | 绝对路径、../、重复条目、软硬链接、设备文件、缺件及额外执行文件 | 全部拒绝，不写出暂存根，不覆盖共享配置。 |
| U03-4 | 校验后替换 inbox 文件、符号链接竞态、磁盘不足/写入失败 | 助手自有 staging 再校验，不执行被替换内容；失败不停止业务。 |
| U03-5 | 准备后修改当前构建、配置或迁移状态再确认 | 原预检摘要失效；重新预检并确认，不能执行旧审批结果。 |

# U04 — 持久状态、互斥和幂等

范围：server/internal/upgrade 的状态机、文件日志与锁。依赖：U01/U02。
交付：完整阶段转换、原子持久记录、幂等索引、活动任务发现与安全清理。

| 用例 | 描述与输入 | 期望输出 |
| --- | --- | --- |
| U04-1 | 两进程同时提交、同幂等键重试、同键换目标或操作者 | 同请求返回原任务；其他竞争冲突，最多一个活动任务；不混用不同操作者授权。 |
| U04-2 | 每个持久化边界注入短写、fsync/rename 失败，重开日志 | 只恢复完整已提交记录；失败不返回已持久受理，不推测成功。 |
| U04-3 | 非法状态跳跃、停止前取消、stopping 后取消 | 禁止非法转换；停止前安全结束并释放锁，停止后拒绝强制取消。 |
| U04-4 | 助手 SIGKILL、残留 PID/临时文件、超过保留数量 | 内核锁释放后可恢复；不依赖 PID 数字判定；保留当前/回退/活动及待恢复备份。 |

# U05 — 宿主助手、注册与权限边界

范围：server/cmd/uvp-updater、deploy/updater、本地客户端及固定执行协议。依赖：U03/U04。
交付：注册绑定实例和受管动作；绿色包独立会话、systemd 独立 unit、Compose 宿主助手；受控 CLI。

| 用例 | 描述与输入 | 期望输出 |
| --- | --- | --- |
| U05-1 | 正确 UID 对注册实例操作，错误 UID、未注册实例、任意 unit/path/命令 | 仅授权固定动作可执行，所有越权输入拒绝；不经 shell 拼接。 |
| U05-2 | 非 root 业务访问特权助手，篡改注册/公钥/staging，容器查看挂载 | 无法改写可信执行资料；业务容器无 docker.sock，仅暴露专用 socket。 |
| U05-3 | 停止业务 unit 或容器；助手继续运行；CLI 查询/恢复 | 助手不随业务退出，CLI 返回持久任务及实际恢复结果。 |
| U05-4 | 应用包 min_updater_protocol 高于助手版本 | 停服前拒绝，提供可信助手引导升级说明；不在任务中替换自身。 |

# U06 — 只读迁移规划与兼容判定

范围：server/app/gb28181/migration、运维入口及兼容探测。依赖：U01。
交付：migration-plan、数据库身份与目标迁移集合校验、完整/部分迁移状态回退判定；复用当前迁移执行内核。

| 用例 | 描述与输入 | 期望输出 |
| --- | --- | --- |
| U06-1 | 四种方言的空/既有/未知记录，旧基线结构不完整 | 只读列出计划或阻断原因，执行前后库无修改，不通过自动补记掩盖问题。 |
| U06-2 | 两数据库同时启用、数据库身份与注册预期不同 | 拒绝应用，不选择一个“看起来可用”的数据库迁移。 |
| U06-3 | 非 SQLite 文件执行一半失败，旧版兼容探测分别通过/失败 | 仅可证明兼容时允许程序回切，否则 recovery_required；不假设整文件事务回滚。 |
| U06-4 | 目标含未认证破坏性迁移、配置/依赖格式不支持旧版 | 一键应用预检拒绝，展示受控维护路径；不自动执行 down SQL。 |

# U07 — 数据备份与恢复前提

范围：uvp-setup 的 SQLite 工具、助手外置数据库备份适配及快照记录。依赖：U06。
交付：停写后备份、受保护凭据处理、快照完整性验证、仅在允许条件下恢复；不接管外置数据库版本升级。

| 用例 | 描述与输入 | 期望输出 |
| --- | --- | --- |
| U07-1 | SQLite WAL 中含未 checkpoint 数据，全部连接关闭后生成快照并恢复 | 快照含全部已提交数据，integrity_check 通过；恢复处理 WAL/SHM，不因裸拷贝丢数据。 |
| U07-2 | SQLite 快照损坏/空间不足，外置备份工具缺失/版本不合/权限不足 | 阻止迁移并尝试恢复原服务，不以存在文件或零退出码认定可恢复。 |
| U07-3 | MySQL/PostgreSQL/SQL Server 测试库备份，恢复到独立可丢弃目标库 | 表结构、关键数据和迁移记录与备份点一致；不覆盖原库。 |
| U07-4 | 已开放业务后请求旧快照恢复；备份包含凭据，检查 argv/日志/权限 | 禁止自动恢复旧快照；凭据不出现在 argv/普通日志，备份及配置权限正确。 |

# U08 — 维护门禁、候选启动与就绪验证

范围：server/bootstrap/init.go、main.go、SIP/后台任务入口、受限探测及前端构建身份。依赖：U05–U07。
交付：自动迁移前的启动门禁、候选只验证模式、持久 commit 决策、解除门禁后受控开放；正常非升级启动保持原有行为。

| 用例 | 描述与输入 | 期望输出 |
| --- | --- | --- |
| U08-1 | 门禁处于恢复中/损坏/不可读，启动后端或 Docker 自动重启 | 在自动迁移和业务初始化前阻止抢跑；不产生库变更或对外副作用。 |
| U08-2 | 候选模式调用写 API、SIP MESSAGE/INVITE、hook、录像与调度，监测后台写入 | 业务入口与外部效果被阻断；只执行明确允许的验证，不能因 HTTP 门禁漏掉后台任务。 |
| U08-3 | 返回 200 但 build_id/task_id 错误、端口可连但数据库未就绪、前端为旧构建 | 不计入通过次数，不报告升级成功；必需本地组件失败阻止提交。 |
| U08-4 | 连续三次就绪、180 秒内反复失败、非必需外置节点/摄像机离线 | 仅连续通过可提交；超时进入失败恢复；非必需节点不误阻断。 |
| U08-5 | commit 记录前/后中断，开放业务后新增数据再重启助手 | commit 前按兼容条件恢复；commit 后继续目标验证/开放，不恢复旧快照覆盖新数据。 |

候选运行需要初始化的授权域和数据库元数据应列成允许变更集合；任何变更均发生于备份后并纳入兼容判断，不把“未开放业务写入”解释为数据库字节绝对不变。

# U09 — 新安装布局与历史安装一次性接入

范围：deploy/standalone 的 ctl、构建、服务安装及 deploy/updater 接入工具。依赖：U05/U08。
交付：固定根入口、共享可写目录、版本目录、受管注册和可恢复接入；对不支持的自定义安装明确拒绝。

| 用例 | 描述与输入 | 期望输出 |
| --- | --- | --- |
| U09-1 | 新包安装、自定义根路径/端口/非 root 用户，启停与状态查询 | 正确解析三个根路径，组件以原用户运行，共享数据不落入可清理的版本目录。 |
| U09-2 | 平铺历史包，已有数据库/配置/证书/录像/授权域，接入两次 | 首次保留原件并注册，重复执行幂等；不重建数据或生成新凭据，业务内容可核对一致。 |
| U09-3 | 接入移动目录或更新入口中途失败/断电，再启动 | 能识别阶段并恢复原入口或完成接入，不出现半迁移且误报完成。 |
| U09-4 | 已有自定义 systemd 服务名和 User，注册助手后启停和开机启动 | 保留原名称、用户和开机状态；业务 cgroup 停止不影响助手。 |
| U09-5 | 历史包尚不支持门禁，或发现无法识别的可写目录/运行方式 | 仅在业务停止且布局/引导适配验证后接入；否则退出并说明，不宣称旧程序已遵守新协议。 |

# U10 — 绿色包与 systemd 激活、回退

范围：server/internal/upgrade 的 local/systemd 适配器和稳定 ctl。依赖：U05–U09。
交付：确认后停服、备份、迁移、切换、候选验证和提交；失败按兼容策略恢复。

| 用例 | 描述与输入 | 期望输出 |
| --- | --- | --- |
| U10-1 | A 正在运行，准备 B 后等待确认，确认后升级完整包 | 确认前 A 不停；完成后前后端及随包依赖对应 B，用户配置/数据/端口保留。 |
| U10-2 | stop 超时/进程残留/端口未释放，备份失败或迁移失败 | 不继续切换；根据实际阶段恢复 A 或报告需恢复，不伪报成功。 |
| U10-3 | current.next 创建、rename 或启动 B 失败；A 与数据兼容 | 恢复 A 指针和必要配置，验证 A 实际可用后才标记 rolled_back。 |
| U10-4 | A 恢复也启动失败、数据兼容性不明、缺少可信旧构建 | 保留现场和门禁，recovery_required；不无限重启或删掉唯一备份。 |
| U10-5 | B 已提交，重新 start/restart 或重启 VM，再手动请求兼容回退 | 日常启动保持 B；回退作为新任务重新验证当前数据，结果不复用升级前预检。 |

# U11 — Compose 正式模板与镜像激活

范围：deploy/compose、镜像元数据、助手 compose 适配器与容器启动门禁。依赖：U05–U09。
交付：在线 digest 和离线镜像 ID 分别验证，持久 Compose 引用、安全迁移容器、配置保留及回退。

| 用例 | 描述与输入 | 期望输出 |
| --- | --- | --- |
| U11-1 | 在线拉取正确 digest、错误 config ID/平台，准备期间 A 正常提供服务 | 仅正确镜像就绪；拉取不停止 A，身份不符拒绝，目标配置不使用 latest。 |
| U11-2 | 完整离线包，docker load 后无 RepoDigest，禁止全部外网 | 依据归档摘要和签名绑定的 config ID 验证，可离线启动；不错误依赖 RepoDigest。 |
| U11-3 | 自定义 project name、端口、环境、卷、额外服务；升级并再次标准重建 | 目标引用持久保存在项目配置，重建仍为 B；持久数据与用户配置不丢，额外服务不被操作。 |
| U11-4 | 现有项目含不支持的覆盖/拓扑变化、预检后配置变动、离线 tag 被换指向 | 应用前阻止并说明原因，不能覆盖用户配置；按实际镜像 ID 检测替换。 |
| U11-5 | 迁移容器失败或 B 就绪失败，恢复旧 Compose 后再次重建 | 仅在数据兼容时恢复 A，引用持久有效；无 down -v、卷删除、无关服务重启或 image prune。 |
| U11-6 | 宿主重启，Docker restart 先于助手拉起业务容器 | 容器启动门禁阻止迁移/写入抢跑，待助手恢复后启动正确版本。 |

# U12 — 全阶段崩溃恢复与运维诊断

范围：任务恢复协调、部署适配器、启动顺序与 CLI。依赖：U04/U08/U10/U11。
交付：日志与实际运行事实协调、幂等恢复、恢复失败明确终态；测试使用可控故障点而非修改生产数据。

| 用例 | 描述与输入 | 期望输出 |
| --- | --- | --- |
| U12-1 | 每个阶段进入前后 SIGKILL 助手，重新启动并查询活动任务 | 无重复备份/迁移/应用；检查实际状态后恢复，无法证明安全时进入 recovery_required。 |
| U12-2 | 停服后、部分迁移后、current/Compose 切换后、commit 前后重启 VM | 按各阶段恢复协议执行，门禁持续有效，实际运行版本与最终任务状态一致。 |
| U12-3 | 伪造/残留 PID，已删旧镜像，损坏任务记录或不可读共享存储 | 不凭 PID 或过时记录误报恢复；停止不安全操作，CLI 给出可定位原因。 |
| U12-4 | 相同恢复命令重复执行，后端和浏览器均不可用 | 恢复幂等，CLI 可读取任务和诊断；没有成功条件时不显示成功。 |

SIGKILL 不等同断电耐久性证明；持久化顺序做故障注入，宿主重启用 VM 验证，两类证据分别记录。

# U13 — 管理 API、授权与任务审计

范围：server/app/service、controllers、routes、权限迁移及审计脱敏。依赖：U05/U09，完整应用回归依赖 U10–U12。
交付：/api/system/upgrade 短请求、受限流式导入、只读/管理权限、幂等受理与任务查询。

| 用例 | 描述与输入 | 期望输出 |
| --- | --- | --- |
| U13-1 | 未登录、初始口令未改、演示账号、普通用户、只读用户、授权超管访问各接口 | 按既有保护链拒绝或放行；只读不能获取/导入/应用/回退，未明确授权不扩大管理权限。 |
| U13-2 | 慢下载、后端请求断开、重复 apply、跨任务预检摘要 | 短请求返回持久 task_id；助手继续既定任务，同请求不重复执行，错误摘要拒绝。 |
| U13-3 | 超限/断开的 multipart 上传，检查全局超时和现有接口 | 半包不就绪；仅专用上传有明确上限/超时策略，不放宽全局接口限制。 |
| U13-4 | 操作人、版本、失败及恢复结果；正文包含模拟密钥/凭据 | 审计可追溯且脱敏；业务库恢复后能关联助手事件，日志不含原始包正文或秘密。 |
| U13-5 | 对外访问详细就绪、任意 URL/path/unit 参数，CLI/后端提交同任务 | 内部状态和任意目标受限；两入口共享注册、互斥、幂等和最终校验。 |

权限菜单/API/角色变更需同时覆盖新库基线与四方言增量，复用现有精确授权测试，不仅检查按钮隐藏。

# U14 — 服务升级页面与真实浏览器验收

范围：web/src/views/system/upgrade、API 客户端、路由和权限入口。依赖：U13；完整交互需 U10–U12。
交付：在线检查、离线导入、预检确认、任务进度、重连与结果展示，沿用项目组件和明暗主题。

| 用例 | 描述与输入 | 期望输出 |
| --- | --- | --- |
| U14-1 | 有更新/无更新/检查失败/部署不支持/助手协议不足 | 明确区分状态；显示用户可决策的版本与限制，不把网络失败标为最新。 |
| U14-2 | 下载完毕、点击应用前后、重复点击和取消 | 完成校验后仍等待确认；确认显示中断和恢复限制，重复点击不重复任务，停止后不可强行取消。 |
| U14-3 | 升级中刷新、关页重开、停服重连、登录过期后重新登录 | 查询并恢复相同 task_id，以服务端事实显示阶段，不按倒计时推测完成。 |
| U14-4 | 自动回退成功、恢复失败、进度未知/仅字节数可知 | 准确显示 rolled_back/recovery_required；不虚构总进度或把回退当升级成功。 |
| U14-5 | 只读账号、明暗桌面和 390px 小屏，旧前端缓存 | 权限与接口一致，控件可达无整页横向溢出；完成后加载与 B 对应的前端入口。 |

先跑 Vitest 行为测试，再在隔离安装的真实登录浏览器完成升级流程；不以文本选择器测试代替视觉验收。

# U15 — 构建、签名和自建站点发布流水线

范围：发布脚本、构建元数据、离线包与 CI；不将普通测试构建变成自动生产发布。依赖：U01–U03/U09/U11。
交付：同 commit 完整目标矩阵、测试签名夹具、受保护生产签名入口、上传验证和 stable 最后切换。

| 用例 | 描述与输入 | 期望输出 |
| --- | --- | --- |
| U15-1 | 两架构完整包、镜像和离线包，同版本但混入不同 commit 或缺少一个制品 | 完整同源矩阵可签发；混版/缺件拒绝发布 stable。 |
| U15-2 | dry-run、无生产私钥、模拟上传失败、上传完成但实际下载不符 | dry-run 无远端写入；失败保留旧 stable；全部下载验证后才切换。 |
| U15-3 | 同 release_id 已存在再上传不同内容，切换时并发读取 stable | 禁止覆盖版本；读者只见完整旧/新签名信封，不出现半个清单。 |
| U15-4 | 检查安装包、镜像层、日志、测试夹具与构建输出 | 仅含公钥；生产私钥不进入制品或日志，测试密钥不能签生产通道。 |
| U15-5 | GitHub 全部封禁，自建源及镜像源可访问；在线和离线各升级一次 | 必要运行资源均来自允许来源，离线包完全无网络；不要求用户额外配置 GitHub token。 |

# U16 — 全矩阵故障验收与交付说明

范围：隔离环境验收脚本、部署说明，以及本任务文件的实际结果记录。依赖：U01–U15、U17。
交付：对规格 AC-01–AC-16 的可追溯证据、首批允许升级来源清单及明确限制；发布和生产部署分别记录。

| 用例 | 描述与输入 | 期望输出 |
| --- | --- | --- |
| U16-1 | Linux 两架构 × 三部署 × 在线/离线，共 12 条成功路径 | 每条实际运行验证，B 身份/前端/随包组件一致，数据保留；构建或模拟运行不冒充原生验证。 |
| U16-2 | 四数据库兼容迁移、迁移失败、备份恢复与拒绝破坏性变更 | 按方言分别留证；未通过某方言时不向其发布自动升级路径，不用其他数据库结果替代。 |
| U16-3 | 每部署的空间不足、半包、启动失败/回退失败、SIGKILL 和 VM 重启 | 旧版可用或明确需恢复，任务无虚假成功，满足 AC 故障边界。 |
| U16-4 | 旧安装一次性接入后升级，复制记录中的标准重建/启停命令 | 接入无需重配业务；后续启停及 Compose 重建保持目标版本，自定义项保留。 |
| U16-5 | 对照 AC、任务结果、交付说明、发布站点实际地址 | 覆盖无缺口；未验证事项明确标注，域名/Registry/生产公钥未落定时不得宣布可正式发布。 |

12 条是部署与分发基础路径，不代替四方言、故障、权限、无人值守及浏览器矩阵；amd64/arm64 各至少有原生环境证据。
不在生产环境制造故障或重启服务器。后续确需远程环境时先通过 SSH MCP 查目标并连接；数据库测试优先使用该环境提供的数据库 MCP，仅对明确的可丢弃测试库操作。

# 任务出口与结果记录模板

每个任务完成后在相应小节追加，不覆盖计划用例：

```text
状态：未开始 / RED / GREEN / REFACTOR / 完成 / 阻塞
实现提交：<commit 或工作树说明>
测试命令与环境：<精确命令、OS/架构、部署方式、数据库/镜像>
结果：<通过/失败/未验证及关键输出>
证据：<日志、任务 ID、运行版本/build_id、摘要或截图路径>
限制：<未覆盖的范围、真实环境差异、后续动作>
```

完成发布前必须额外形成以下可审计结果：

- 规格 AC-01–AC-16 到 U01–U17 的映射及每项实际证据。
- 允许自动升级的版本来源、数据库方言和随包依赖兼容矩阵；被阻止的路径也要列出原因。
- 自建发布站点、Registry、测试/生产签名公钥指纹和 stable 切换记录；不在代码或文档中写入私钥。
- 提交、构建、制品摘要、镜像 digest/config ID、部署、运行、浏览器和生产发布分别记录，不能用源码测试代替。
- 版本/迁移/恢复限制写入面向管理员的升级说明；明确哪些情况必须人工维护恢复。

## 当前执行结果

状态：进行中（U01–U04 部分 GREEN）。已新增 `server/internal/upgrade` 的签名清单校验、版本比较、目标制品匹配、自建 HTTPS 清单获取、制品流式摘要校验、内存 sequence 防重放、任务状态原子落盘、跨进程文件锁和幂等任务索引；未接入管理 API、升级助手或三种部署适配器。
实现提交：工作树未提交；仅新增升级相关文件，未触碰其他既有脏改动。
测试命令与环境：`cd server && go test ./internal/upgrade`、`go test -race ./internal/upgrade`；本机 macOS，Go 1.25。
结果：通过。覆盖 Ed25519 可信签名、严格 JSON/重复字段、版本与 rc 比较、清单身份/序号/制品约束、部署/OS/架构/助手协议匹配、HTTPS 来源、跨主机重定向、响应大小限制、制品长度/SHA-256 校验、sequence 旧内容/冲突内容防重放、状态跳转、损坏落盘拒绝、跨实例文件锁、同键幂等和活动任务冲突。
证据：`server/internal/upgrade/manifest_test.go`、`server/internal/upgrade/source_test.go`；测试输出为 `ok uvplatform.com/uvp-gb28181/internal/upgrade`。
限制：sequence 目前只在进程内，任务索引尚未接入保留策略、恢复扫描和故障注入；尚未实现离线导入、助手和无人值守调度；尚未构建、连接测试数据库、发布或部署。后续继续按 U04/U05 的 RED 测试推进。

状态：进行中（U05/U06/U10/U11/U12 局部 GREEN）。
实现提交：工作树未提交；本次仅补充 Compose 离线镜像身份判定、外置数据库注册构造和助手注册命令，保留其他脏改动。
测试命令与环境：`cd server && go test ./internal/upgrade ./cmd/uvp-updater`；`bash -n deploy/standalone/uvp-gb28181-ctl.sh deploy/standalone/uvp-gb28181-service-install.sh`；使用现有 `server/config/config.yml` 执行 `go run ./cmd/uvp-updater register ...`。
结果：通过。Compose 镜像在 `docker load` 后缺少 `RepoDigest` 时，只要签名清单绑定的 `config_id` 匹配即可离线继续；没有 `config_id` 且没有 `RepoDigest` 会拒绝，config ID 不匹配会拒绝。注册命令生成 0600 的 JSON，能从 `config.yml` 选择唯一启用的外置数据库，且不写入数据库口令；SQLite 不生成外置备份注册。
证据：`server/internal/upgrade/adapters_test.go`、`server/internal/upgrade/external_backup_test.go`；本机 macOS arm64，Go 1.25；注册输出已检查不含 `Tudou`、`pass` 或口令字段。
限制：尚未连接真实 Docker daemon、Registry、Linux amd64/arm64、systemd 或宿主重启；外置数据库备份仍要求管理员通过 `UVP_UPGRADE_DB_PASSWORD`（或 `UVP_UPGRADE_DB_PASSWORD_ENV` 指定的变量）提供口令，未验证真实 mysqldump/pg_dump/sqlcmd 和恢复链路；candidate 业务禁写及完整权限 HTTP 回归仍未完成。

状态：进行中（本轮恢复与部署接线 GREEN）。
实现提交：工作树未提交；新增恢复协调、固定迁移动作、Compose 注册迁移服务、活动版本 version/build 校验、systemd 助手注册参数和服务升级 API 单测。
测试命令与环境：`cd server && go test ./internal/upgrade ./cmd/uvp-updater -race`；`go build ./...`；`bash -n deploy/standalone/uvp-gb28181-ctl.sh deploy/standalone/uvp-gb28181-service-install.sh`；`cd web && pnpm exec vitest run src/api/service-upgrade.test.ts --pool=forks --maxWorkers=1`；本机 macOS arm64，Go 1.25，Node 25。
结果：通过。助手启动时扫描 stopping 到 committing 的活动任务并从已持久边界继续，失败保留任务状态供 CLI 查询；绿色包/systemd 使用注册控制脚本执行应用 `-migrate-up`，Compose 使用注册服务执行 `docker compose run --rm --no-deps <service> -migrate-up`；候选启动后校验活动 `version.json` 的 version/build_id；服务升级 API 单测覆盖任务幂等字段、应用/回退路由、策略、迁移计划和离线 multipart。
限制：仍未完成真实三种部署环境、候选业务副作用隔离、外置数据库恢复、任务并发锁的宿主级故障注入、权限 HTTP 回归和浏览器视觉验收；本机没有 Docker daemon，因此 Compose 仅有命令构造和离线身份单测证据。


# U17 — 无人值守策略、维护窗口与通知

范围：server/internal/upgrade 的策略模型与调度器、策略 API、页面设置和本地事件记录；不绑定具体邮件/短信/企业 IM。依赖：U04、U08、U10–U14。
交付：默认关闭、管理员授权、窗口与时区处理、自动条件门禁、暂停/恢复、审计和阻止通知；自动流程复用手动应用的最终校验和恢复路径。

| 用例 | 描述与输入 | 期望输出 |
| --- | --- | --- |
| U17-1 | 新安装或未配置策略，存在可用稳定版本 | 策略为 disabled；只下载/检查，不停服、不重启、不创建自动应用任务。 |
| U17-2 | 超管开启策略，指定部署方式、架构、补丁范围、IANA 时区和维护窗口；普通用户尝试修改 | 超管策略保存并递增 policy_version、写审计；普通用户拒绝；任务保存策略快照。 |
| U17-3 | 窗口外、窗口开始/结束边界、夏令时重复/缺失时刻、策略暂停 | 窗口外保持 awaiting_confirmation；不在窗口结束后开始 stopping；暂停立即阻止尚未停服的自动任务，边界按明确时区规则记录。 |
| U17-4 | 签名/摘要/兼容性/备份/空间/配置摘要任一失败，或版本为 rc/破坏性迁移 | 自动转 blocked 或 manual_required，不停服；记录单一可定位原因并生成事件。 |
| U17-5 | 所有自动条件满足，助手重启或后端/浏览器均离线 | 由助手写 auto_approved_by_policy 后执行；不依赖浏览器或 HTTP 长请求；重启不重复应用。 |
| U17-6 | 自动应用候选启动验证失败，旧版本兼容/不兼容，策略允许/不允许自动回退 | 兼容且允许时按已验证回退并验证；否则 recovery_required；不执行 down SQL/RESTORE 覆盖库，不伪报成功。 |
| U17-7 | 策略版本变化、当前配置/迁移状态变化、stable sequence 重放 | 已准备任务使用旧策略快照但重新检查当前摘要；变化则阻止并要求新任务，不能沿用旧授权。 |
| U17-8 | downloaded、blocked、auto_started、succeeded、rolled_back、recovery_required 事件，日志包含凭据 | 页面/持久本地日志可查事件、策略版本、目标 build_id 和原因；敏感值脱敏，外部通知未配置不影响升级。 |
| U17-9 | Docker、绿色包、systemd 各运行一条自动任务，窗口中宿主重启 | 三种适配器都由助手恢复并遵守相同策略；Docker 不因 restart 策略抢跑，systemd 保持原 User，绿色包不依赖浏览器。 |

无人值守任务不能使用“跳过签名”“忽略备份”“强制继续”等隐藏参数；测试应检查 API、CLI、配置文件和环境变量均无法注入这些旁路。

# 规格验收映射补充

- AC-15 对应 U17-1、U17-2、U17-3、U17-5、U17-9。
- AC-16 对应 U17-4、U17-6、U17-7、U17-8，以及 U06/U07/U08/U12 的恢复证据。
- 原 AC-05 的“确认应用”在无人值守路径解释为“策略自动确认”；页面必须显示策略授权和自动触发原因，不能伪装成人工点击。


# 执行补充 — 2026-10-16 独立助手与发布可信链

状态：进行中；工作树未提交，未推送、未发布、未部署。

本轮已实现并验证的边界：

- 独立助手在执行前用注册公钥重新验证原始签名清单、部署目标、最低来源版本、制品长度及 SHA-256，复制到验证目录；保留 tar/tar.gz 原格式。普通升级拒绝同版本和降级，回退走独立流程。
- 停服前重新核对策略版本、维护窗口和来源预检；手动与自动任务均绑定准备时的构建、配置、数据库方言及迁移摘要。缺少独立证据或摘要变化时不停止旧服务。
- 新增带发布时间、过期时间和独立 sequence 的签名通道指针，先验证指针，再验证同 HTTPS 来源下的不可变版本清单摘要和独立签名。刷新指针有效期不要求覆盖已有版本清单。
- 恢复门禁和候选门禁阻断业务入口；候选就绪必须来自运行中的后端，并连续核对版本、构建、任务和数据库状态。已提交任务不允许恢复升级前快照。
- 自动任务的维护窗口接管已由独立 helper scheduler 实现，业务服务停止后通过注册的当前后端 `-upgrade-preflight` 只读命令重新收集证据，仍须完成部署接线和隔离环境验收。

已执行的本机证据：

- `cd server && go test ./internal/upgrade ./cmd/uvp-updater -race -count=1`：在本轮后续接线改动前通过；接线完成后须重跑最终回归。
- `cd server && go test ./internal/upgrade -run 'TestRegisteredTaskVerifier|TestWorkflow' -race -count=1`：通过，包含同版本/降级/最低来源版本拒绝、tar 原格式、停止前最终预检和提交后恢复边界。
- `cd server && go test ./internal/upgrade -run 'TestFetch|TestDownload|TestChannel' -race -count=1`：通过，覆盖通道过期/未来时间、签名、跨来源、摘要和版本身份不一致，以及合法刷新指针。
- 管理 API、middleware、routes 和 runtime identity 定向竞态测试通过；`go build ./...` 通过。
- `cd web && pnpm exec vitest run src/api/service-upgrade.test.ts src/views/system/service-upgrade/index.test.ts --pool=forks --maxWorkers=1`：2 个文件、9 项通过。
- `node --test scripts/sign-upgrade-manifest.test.mjs`：10 项通过；后续发布目录和构建身份接线须另记最终结果。

尚未完成的出口：真实 Linux 两架构三种部署、外置数据库恢复、宿主崩溃恢复、业务权限 HTTP 回归、登录后的浏览器视觉与完整升级验收。自建发布域名、镜像仓库和生产签名公钥尚未提供，当前不执行正式发布。助手特权状态隔离、回退后的数据与运行身份验证仍在审查修正，不将模拟命令测试作为生产可用证明。

# 执行补充 — 停服前容量与 Linux amd64 核心验证

状态：进行中；仍未提交、推送、发布或部署业务服务。

- 新增完整包 tar/gzip 预扫描：校验路径、类型、重复项、父目录冲突、展开上限、内容截断和 gzip 校验和。以实际文件系统设备合并 state、releases 和本机 Docker 存储预算，现有版本不计入可回收空间；预检失败不停止服务、不改变门禁。
- 长耗时扫描与数据库预检完成后，再读取自动策略和维护窗口；避免扫描跨出窗口后仍开始停服。外置数据库导出体积的独立估算尚未覆盖，不能把当前空间预算称为所有数据库的容量保证。
- 离线导入完整读取 gzip 尾部并检查流上限，暂存包重命名后同步目录；metadata 使用唯一临时文件后原子替换。
- 本机 `go test ./internal/upgrade ./cmd/uvp-updater -race -count=1` 在 RPC 权限隔离改动前通过；新增预检、窗口和导入回归的定向竞态测试通过。并行实现完成后需再执行最终全包回归。
- 通过 SSH MCP 在 `UVP 测试服务器`（Linux amd64）唯一临时目录 `/tmp/uvp-upgrade-check-4u0XuC` 执行交叉编译的 `internal/upgrade` 与 `cmd/uvp-updater` 完整单测二进制，两者均 `PASS`、退出码 0。该结果是 Linux 本地文件/socket/SQLite/适配器测试证据，不代表实际 systemd、Docker daemon 升级或 arm64 验收；未修改现有业务服务、数据库或镜像。

补充修正：归档预算计入隐式父目录，限制路径深度、组件长度及总目录数；按 64 KiB 保守单位计费并拒绝更大的文件系统分配单位。门禁落盘后再次核对自动策略与时间，过窗时恢复正常门禁且不执行 Stop。新增保留清理仅处理已知完成任务的制品，保护当前、回退、失败/恢复任务及未知目录，使用受限目录句柄删除，不清理 Docker 镜像；提交后的清理错误单独记为 `cleanup_failed`，不能触发数据回滚。

新增容量、窗口、归档和保留清理测试已在排除并行中 `control_test.go` RED 的选定文件竞态套件中通过；这不是最终全包测试结果。助手 RPC、生产控制器和部署权限接线完成后须统一重跑。

实际 Linux amd64 CLI 验证：以 `-X uvplatform.com/uvp-gb28181/app/global/app.BuildID=preflight-fixture-build` 构建真实后端，经 SSH MCP 放入 `/tmp/uvp-upgrade-check-4u0XuC/uvp-preflight`，运行 `scripts/verify-upgrade-preflight.sh --binary <该文件> --workdir /tmp/uvp-upgrade-check-4u0XuC/preflight-fixture`。结果退出码 0，stderr 为空；stdout 被校验为单一 JSON，版本、构建和方言正确，配置与 SQLite 文件前后 SHA-256 相同，无遗留子进程。该临时夹具不连接现有业务数据库，不等同于线上服务切换验收。

Linux amd64 第二次交叉编译回归：修复 `openat` 注册文件描述符关闭顺序后，将 `internal/upgrade` 与 `cmd/uvp-updater` 完整测试二进制放到测试服务器 `/home/ubuntu/uvp-upgrade-test-TIBow3`，分别执行，均 `PASS`、退出码 0。第一次 Linux 运行曾暴露 `bad file descriptor`，已修复后复跑通过；临时测试目录未用于现有业务服务。

恢复边界补充：新增持久 `failure_phase`，保存进入 failed/recovery_required 前的执行阶段；人工重试恢复不覆盖原阶段。停止或备份失败尚未进入迁移时，不要求恢复不存在的 SQLite 快照，也不把外置数据库误认作目标迁移状态。先运行新增行为测试观察到 SQLite、MySQL 及持久阶段三个失败，再实现并执行 `go test ./internal/upgrade -run 'TestRollback|TestSQLiteRollback|TestExternalRollback|TestWorkflowPersistsOriginalFailurePhase' -race -count=1`，通过。

本轮扩大回归在并行接线期间发现待修问题：helper 的可写 gate 父目录拒绝测试失败，以及两个升级控制器 HTTPS 夹具证书测试失败，已交由对应实现处理；不能将该次全包运行记为通过。middleware、routes、app/global/app 完整 race 测试通过。controllers 完整 race 另外暴露 cachehelper 清理 timer 竞争和 `TestLoggingBaseHTTPFailure` 错误泄露断言失败，均在本轮升级修改范围外，未改动这些文件。

# 执行补充 — 2026-10-16 smoke 发布包与最终定向回归

状态：进行中；本次仅完成隔离构建与本机/交叉编译验证，未发布到正式站点。

- 在临时目录 `/var/folders/fq/t7kcbyf96kl18vpdslng8_cw0000gn/T/uvp-upgrade-smoke.ZUVIv0` 生成两份真实 Vite production dist，未写入工作区 `web/dist`：A 为 `1.0.9 / upgrade-smoke-a-20261016`，B 为 `1.1.0 / upgrade-smoke-b-20261016`。
- 两份包均执行 `checkReleaseMetadata`、`checkFrontendBuildIdentity`，并检查 `dist/version.json` 与编译后 JavaScript：版本和 build_id 均匹配，bundle 中实际包含对应 build_id；每份 dist 约 26 MiB。该目录供后续 Linux standalone smoke 使用，暂不清理。
- `cd server && go test ./internal/upgrade ./cmd/uvp-updater -race -count=1`：通过；`go build ./...`：通过。
- `cd web && pnpm exec vitest run src/api/service-upgrade.test.ts src/views/system/service-upgrade/index.test.ts src/config/version.test.ts --pool=forks --maxWorkers=1`：3 个文件、13 项通过。
- `node --test scripts/sign-upgrade-manifest.test.mjs scripts/publish-upgrade-release.test.mjs scripts/release-build-id-contract.test.mjs scripts/release-build-id-runtime.test.mjs scripts/release-frontend-build-id-runtime.test.mjs`：24 项通过；真实 Vite build identity 测试也通过。
- `GOOS=linux GOARCH=amd64/arm64 go build ./cmd/uvp-updater`：分别得到 ELF amd64/aarch64；本机未安装 Docker，未执行真实 Docker daemon、systemd 切换、宿主重启、外置数据库恢复或登录后浏览器验收。

# 执行补充 — 权限边界、错误脱敏与 Linux 候选复验

状态：进行中；未提交、推送、正式发布或部署现有业务。

- 升级助手的 Unix socket、任务状态、已验证制品和事件日志保持助手私有；上传只进入管理员准备的 opaque inbox，助手 RPC 才执行签名导入。RPC 返回、任务错误、gate 原因和审计事件经过统一凭据脱敏后才落盘或返回页面。
- 注册适配器已接入管理员登记的业务 UID/GID：绿色包的控制脚本和 systemd 的应用迁移按业务身份运行，systemctl 仍由 root 助手执行；新增身份执行器测试覆盖 UID/GID、工作目录和失败传播。
- `cd server && go test ./internal/upgrade ./cmd/uvp-updater -race -count=1`：通过；升级控制器定向 race 通过；`go build ./...` 通过；部署 shell `bash -n` 通过；发布/build identity Node 24 项通过。
- SSH MCP 在 `UVP 测试服务器` 新建隔离候选目录，用最终 Linux amd64 后端 `upgrade-runtime-final-20261016` 实跑：候选 readiness 的 version/build/task/migration identity 正确，业务表和迁移账本未改变；清除 gate 后同一进程才初始化 9 个业务 jobs；退出码 0。

仍未完成的出口：真实六目标（Linux amd64/arm64 × 绿色包/systemd/Compose）切换与回退、Docker daemon/Registry 或离线镜像导入、外置数据库四方言备份恢复、宿主崩溃恢复、systemd 实机接管、登录后浏览器视觉验收。生产发布站点、Registry、生产公钥和正式维护窗口尚未提供，不能据此报告生产可用。

# 执行补充 — 2026-10-16 业务 GID 绑定修正

- 发现并修正 Compose 自定义 `UVP_BUSINESS_GID` 与共享升级 gate 属主不一致的问题：助手现在从管理员注册信息读取业务 GID，`updater-gate` 目录和 `gate.json` 均按该 GID 设置属主；无注册参数的兼容测试仍使用原默认 GID。Compose 安装脚本的属主检查也改为比较实际 `BUSINESS_GID`，不再固定断言 `10001`。
- 新增 root 条件下非默认 GID 的 gate 目录/文件属主回归测试。
- 已验证：`cd server && go test ./cmd/uvp-updater -race -count=1` 通过；`cd server && go test ./internal/upgrade -race -count=1` 通过；`bash -n deploy/compose/uvp-compose-upgrade-install.sh` 通过。
- 仍未覆盖真实 Docker daemon、systemd、Linux amd64/arm64 部署切换及宿主重启；本次只修正本地权限接线，不能据此报告 Compose 生产升级已验收。

# 执行补充 — 权限回滚排序规则与人工确认

状态：进行中；工作树未提交、未推送、未发布、未部署。

- 四方言正向权限映射补齐无人值守策略写接口 `/api/system/upgrade/policy`；四个 down 迁移改为按 ASCII API 路径删除，避免用中文 `api_group` 回滚时发生 MySQL 排序规则冲突。MySQL 正向迁移中的 API/菜单/Casbin 文本关联改为二进制比较，避免旧表与当前连接排序规则不同导致安装启动失败。
- 管理页面的应用和回退动作现在均先展示中断范围与数据库恢复限制，再由管理员确认后提交助手；组件测试检查确认前不发起应用/回退请求。
- 测试命令与环境：`cd server && go test ./... -run 'TestServiceUpgrade|TestUpgradeCandidate|TestConfiguredUpgrade|TestProductionUpgradeController|TestInitRoutesDoesNotStartAutomaticUpgradeScheduler' -count=1`；`go test ./app/gb28181/migration -run 'TestServiceUpgrade' -count=1 -v`；`cd web && pnpm exec vitest run src/api/service-upgrade.test.ts src/views/system/service-upgrade/index.test.ts --pool=forks --maxWorkers=1`；`pnpm exec vite build --mode development`；`node --test scripts/*.test.mjs`；相关部署脚本 `bash -n`。
- 结果：Go 全仓定向测试通过；迁移四方言合同测试通过；前端 2 个文件、11 项通过；Vite 开发构建通过；Node 83 项中 79 通过、4 项按条件跳过；部署 shell 语法通过。前端 `vue-tsc --noEmit` 仍被既有 firmware-repo/device firmware 类型错误阻断，升级页面无对应错误记录。
- 曾在同一 Go 定向测试中复现 MySQL 1267 混合排序规则错误；根据实际迁移 SQL 将 Casbin 的 `v1/v2` 比较也改为二进制比较后，同一命令通过。此修复有应用启动迁移路径证据，但四种数据库的真实迁移/回滚仍需各自隔离数据库验收。
- 限制：没有真实 Docker daemon/Registry、Linux amd64/arm64 部署切换、systemd 服务接管、宿主断电/重启和外置 MySQL/PostgreSQL/SQL Server 备份恢复证据；本机浏览器自动化环境不可用，尚未完成真实登录页面视觉与完整升级闭环。发布站域名、镜像仓库和生产密钥仍未确定。

# 执行补充 — 2026-10-16 本地最终回归

状态：本地实现与测试完成；真实部署矩阵仍未验收。工作树未提交、未推送、未发布，未修改现有业务服务。

本轮实际证据（macOS arm64，Go 1.25，Node 25）：

- `cd server && go test ./internal/upgrade ./cmd/uvp-updater -race -count=1`：通过。
- `cd server && go test ./internal/upgrade -run 'Test.*(Compose|Systemd|Standalone|Rollback|Recovery|Auto|Scheduler|Registered|Preflight|Capacity|Staging|Source|Channel|Manifest|Task|Workflow)' -count=1`：通过。
- `cd server && go test ./app/controllers -run 'ServiceUpgrade|Upgrade' -count=1`、`go test ./app/middleware -run 'UpgradeCandidate|ServiceUpgrade' -count=1`、`go test ./app/routes -run 'Upgrade' -count=1`、`go test ./app/gb28181/migration -run 'ServiceUpgrade' -count=1`：均通过。
- `cd server && go build ./...`：通过。
- `cd web && pnpm exec vitest run src/api/service-upgrade.test.ts src/views/system/service-upgrade/index.test.ts --pool=forks --maxWorkers=1`：2 个文件、11 项通过；`pnpm exec vite build --mode development`：通过。
- `node --test scripts/*.test.mjs`：83 项，79 项通过、4 项按环境条件跳过、0 项失败；`python3 -m unittest discover -s deploy/test -p '*test.py' -v`：14 项通过；升级相关部署脚本 `bash -n`：通过。
- 发布脚本已验证完整六目标矩阵、Ed25519 清单、SHA-256、离线包、不可覆盖 release、stable 指针原子切换和 build_id 一致性；客户端运行时只依赖登记的自建 HTTPS 发布源。

完整 `go test ./app/controllers ./app/gb28181/migration` 仍会被工作树中与本任务无关的既有测试/迁移文件缺失及日志断言阻断；升级专用定向测试均通过，不能将该全包结果解释为升级代码失败或全仓通过。

本机没有 `docker` 客户端/daemon，故真实 Docker 镜像导入、Compose 重建与回退、Linux amd64/arm64 三种部署切换、systemd 实机接管、外置 MySQL/PostgreSQL/SQL Server 备份恢复、宿主重启/SIGKILL 和登录后浏览器视觉闭环仍标记为“未验证”。正式发布域名、Registry、生产公钥和首批构建确定后，再执行隔离 Linux 验收和正式站点发布。

# 执行补充 — 2026-10-16 状态能力与 Compose 生命周期回归

状态：本地升级实现与定向回归通过；真实 Docker daemon 仍未验证。

- 助手 `ControlStatus` 新增在线检查和离线导入能力字段。在线检查只有在注册的 HTTPS 发布源和可信公钥同时存在时报告可用；管理 API 直接使用助手事实，不再因 RPC 可达就固定返回 `onlineCheck: true`。
- 新增 `TestControlStatusReportsRegisteredOnlineCapabilities`、`TestProductionUpgradeStatusUsesHelperOnlineCapability`，覆盖未配置发布源、仅可信公钥和完整 HTTPS+公钥三种状态。
- 新增 `TestComposeLifecycleFakeRunnerCoversPrepareStopBackupMigrateActivateValidate`，在无 Docker daemon 的环境用 fake runner 验证 Compose 准备、停止、备份、固定迁移命令、注册 compose 原子激活、候选身份验证的顺序和目标文件结果。
- 本轮命令：`cd server && go test ./internal/upgrade ./cmd/uvp-updater -race -count=1`、升级 controller/middleware/routes 定向测试、`cd web && pnpm exec vitest run src/api/service-upgrade.test.ts src/views/system/service-upgrade/index.test.ts --pool=forks --maxWorkers=1`、`python3 -m unittest discover -s deploy/test -p '*test.py' -v`；全部通过（前端 2 文件 13 项，部署测试 14 项）。
- 限制：fake runner 不替代真实镜像 load、Compose 重建、容器内迁移和回退；正式发布域名、Registry、生产签名公钥及 Linux 三部署矩阵仍待确定后验收。

# 执行补充 — 2026-10-16 最终复核更正

- `cd server && go test ./internal/upgrade ./cmd/uvp-updater -race -count=1`：通过。
- `cd web && pnpm exec vitest run src/api/service-upgrade.test.ts src/views/system/service-upgrade/index.test.ts --pool=forks --maxWorkers=1`：2 个文件、14 项通过。
- `app/middleware`、`app/routes` 和 `app/gb28181/migration` 的升级定向测试分别通过。
- `app/controllers` 升级定向测试在当前工作树编译 `app/gb28181/cascade_invite_handler.go` 时被既有未定义符号 `cascadeMediaSourceIdentity` 阻断；该符号不属于本次升级实现，未修改级联脏改动。不能把这次控制器命令记为通过，也不能据此否定已通过的升级核心、middleware、routes 和迁移测试。
- `git diff --check` 未发现本次升级文件新增的空白错误；工作树仍包含其他功能的既有未提交改动。真实 Docker daemon、systemd 接管、Linux 双架构部署切换、外置数据库恢复及正式发布站点仍未验证。
- 随后级联工作树补齐 `server/app/gb28181/cascade_subject.go` 后，重新执行 `go test ./app/controllers -run 'ServiceUpgrade|Upgrade' -count=1`：通过；`go build ./...` 也通过。因此当前升级相关定向测试和全仓构建均有通过证据，前一条仅记录中间时点的短暂阻断。
- 最后一批收尾回归：升级核心/助手完整 race 测试、controllers/middleware/routes 升级定向测试及 `go build ./...` 均通过；前端升级与版本身份测试 3 文件 18 项通过；发布签名与构建身份 Node 测试 24 项通过；部署 Python 测试 14 项通过；升级 shell 语法及限定文件 `git diff --check` 通过。未追加提交、推送、发布或实机部署。

# 执行补充 — 2026-10-16 前后端闭环与浏览器复核

- 管理页面补齐任务阶段中文说明、失败原因、失败阶段、无人值守策略版本、操作者与更新时间；`recovery_required` 明确显示“不可视为已回退”。任务表只展示管理员可处理的诊断信息，不展示助手的暂存目录、备份目录、旧版本切换路径、配置快照或兼容声明。
- 应用/回退确认现在按持久任务自身的版本和 build_id 生成，不依赖页面后来检查到的另一份更新；只读权限、助手能力、活动任务、状态错误和重复提交均在页面动作层再次阻止。状态、策略、迁移计划采用独立结果处理，迁移查询失败不会覆盖任务状态；轮询请求合并，避免慢响应倒写旧状态。
- 策略表单补齐时区、HH:MM 维护窗口和保留版本数校验；保留版本使用普通输入，不静默 clamp；所有输入支持清空。离线导入改用 Arco 的 `upload-button` 插槽，移动端描述表格和页头在 390px 下不产生页面横向溢出，任务表在自身区域横向滚动。
- 真实开发服务器浏览器复核覆盖桌面和 390px 小屏、任务失败/回退、正确目标确认弹窗、未配置发布源及 `uvp-system-dialog` 类；前端 3 个升级相关测试文件共 27 项通过，Vite development build 通过。后端控制器新增公共任务序列化边界测试，升级控制器定向测试通过。
- 真实 Docker daemon、Linux amd64/arm64 切换、外置数据库恢复、宿主机中断恢复、正式发布站点和 Registry 仍需隔离部署环境验收；本机没有 Docker/systemd，不能用浏览器夹具或 fake runner 代替这些证据。

# 执行补充 — 2026-10-17 220 静态包首轮验收

状态：Linux amd64 绿色包构建、隔离安装启动及真实后端预检/candidate 生命周期通过；完整签名升级应用与回退未验证。

- 环境：SSH MCP 已连接 `开发环境支撑220`（Ubuntu 24.04 LTS、x86_64）。制品 `/tmp/uvp-static-acceptance-20261017/output/uvp-gb28181-linux-amd64-1.1.0.tar.gz`，SHA-256 `07b23a845359e4ba7d09ba6b46c48c41169fd423ddf2fb47dfa0932760f4a7c4`，build_id `acceptance-20261017`。包内 ELF 可移植性门禁通过，随包 nginx/Redis/ZLM 的 glibc 要求最高为 2.17。
- 隔离安装路径 `/home/superadmin/uvp-static-acceptance-20261017/install-3/uvp-gb28181-linux-amd64-1.1.0`，使用 52000–52063 端口段和独立 `UVP_PROCESS_AUTHORITY_DIR`；未触碰旧服务 `/opt/uvp-220test/...`。控制脚本报告 Redis、ZLM、后端和 nginx 均运行；配置、updater-state、run 目录权限均为 0700。
- HTTP 实测：`https://127.0.0.1:52000/` 返回 200；生产 JS `/static/js/index-DPaolEKR.js` 返回 200、`application/javascript`、1,619,842 bytes；CSS `/static/css/index-CqJrBK_u.css` 返回 200、`text/css`、531,787 bytes。非 candidate 模式的 `http://127.0.0.1:52002/internal/updater/readiness` 返回预期 503。
- 在 220 上使用包内真实 `bin/uvp-server` 执行 `scripts/verify-upgrade-preflight.sh --binary <package-bin> --build-id acceptance-20261017 --version 1.1.0 --workdir /tmp/uvp-static-acceptance-20261017/acceptance-run/preflight`：通过，独立 SQLite fixture 未被预检改写。
- 使用同一真实二进制执行 `scripts/verify-upgrade-candidate.sh --binary <package-bin> --build-id acceptance-20261017 --version 1.1.0 --workdir /tmp/uvp-static-acceptance-20261017/acceptance-run/candidate`：通过；candidate readiness 身份匹配，candidate 阶段业务表和迁移账本未变化，清除 gate 后同进程才初始化 9 个业务 jobs。
- 220 的 Docker daemon（29.6.1）和 systemd（255）可用；本轮没有注册 systemd unit，也没有对现有容器做操作。真实 Docker Compose 切换、签名更新包下载/导入、版本 B 激活、失败回退、登录后管理页面操作和宿主重启均未验证。生产发布域名、Registry、生产公钥仍待确定。

# 执行补充 — 2026-10-17 220 签名升级与回退实测

状态：真实 standalone 升级成功用例失败；任务已回退至 1.1.0，业务服务和升级助手均恢复运行。回退终态已验证；候选版本成功提交仍未验收。

- 环境限定为 `/home/superadmin/uvp-static-acceptance-20261017/install-3/uvp-gb28181-linux-amd64-1.1.0`，使用测试 Ed25519 密钥和 build_id `acceptance-20261017`。未停止、修改或清理 `/home/superadmin/uvp-verify`、`/opt/uvp-220test` 或同机其他服务。
- 导入的签名离线包目标为 1.1.1，SHA-256 `7bca59219019c6fb5f54e913d4a267d018c6e883e938f016270c220e2c9afb18`。任务 `41d6fe0e827fa98716837b6799d4b4d5` 完成签名制品导入、预检、停服、SQLite 快照、迁移和候选激活，随后候选校验失败。任务最终 `phase=rolled_back`、`failure_phase=validating`、错误为 `exit status 1`；事件记录曾进入 `recovery_required`，后续受控恢复终态为 `rolled_back`。
- 回退后 `current` 指向 `releases/initial`，根版本仍为 1.1.0 / `acceptance-20261017`；`gate.json` 为 `normal`；控制脚本报告 Redis、ZLM、后端及 nginx 运行；SQLite 快照位于 `updater-state/backups/41d6fe0e827fa98716837b6799d4b4d5/database.sqlite`。后端 readiness 在 normal gate 下返回 503，符合候选专用端点行为，不表示普通业务健康失败。
- nginx 错误日志记录候选启动曾尝试从 `/opt/uvp-220test/uvp-gb28181-linux-amd64-1.1.0/vendor/nginx/conf/uvp.key` 读取证书私钥并因权限拒绝失败。该证据证明验收 B 包带有旧安装路径；它解释候选 nginx 启动失败，但不单独解释之后回退阶段的通用 `exit status 1`。需在重跑前修正并验证 B 包运行路径布局，同时让助手保留受控命令输出或增加阶段错误上下文，才能明确定位回退子步骤失败。
- 本轮在本地 `deploy/standalone/uvp-gb28181-ctl.sh` 暂加“每次 start 先重渲染 nginx 配置，再检查已有监听”的调整；`bash -n` 与该文件 `git diff --check` 通过。尚未补有行为回归测试、尚未进入 220 安装包，也未证明该改动可以修复此次签名升级；不得把此项记为已修复。
- 当前下一步：先为安装路径变化场景补控制脚本/适配器行为测试，确认候选与回退都使用注册安装根下的配置、证书和持久目录；再重建并重新签署 B 包、在同一隔离实例执行成功升级与失败回退。测试通过前保持当前已恢复的 1.1.0 服务，不重跑原任务 ID，也不手工清除 gate。
- 本轮附加回归：`bash -n deploy/standalone/uvp-gb28181-ctl.sh`、`git diff --check -- deploy/standalone/uvp-gb28181-ctl.sh docs/tasks/service-upgrade.md`、`python3 -m unittest discover -s deploy/test -p '*test.py' -v`（14 项）通过。首次执行 `cd server && go test ./internal/upgrade ./cmd/uvp-updater -race -count=1` 时，`TestUnixServerApplyPersistsBackgroundExecutionError` 的 recovery outcome marker 断言失败；随后 `go test ./internal/upgrade -run '^TestUnixServerApplyPersistsBackgroundExecutionError$' -count=1 -v` 单项通过，故记为未稳定复现，不据此宣称完整 race 套件通过。

# 执行补充 — 2026-10-18 升级故障修复及 220 复验

状态：Linux amd64 绿色包真实签名离线升级、失败自动回退及 SQLite 恢复通过。最终隔离实例运行 1.1.2 / acceptance-20261018-final。未提交、推送或发布生产。

根因及修复：

- nginx 的渲染配置被构建目录复制进包，包含其他安装目录的绝对路径；模板又把版本资源和持久状态都解析到安装根。出包时删除 nginx.conf、uvp.crt/key；运行时配置写入 run/nginx.conf，mime.types、前端资源指向活动 release，证书与 uploads 保留在稳定安装根。
- sync_ports_into_config 从 config.env 读取自定义端口后，仅凭当前进程的环境变量重写 USER_SET 标记，下一次启动便退回默认 51000 段；ZLM 完整端口段也没有落盘。现保留既有显式标记，读写完整 ZLM 端口/RTP 范围，并消除每次启动追加空行造成的配置漂移。
- 助手原先丢弃子命令输出，失败只显示 exit status 1。现保留最多 8 KiB 的 stderr 尾部并经现有脱敏器处理，保留原退出异常链；stdout 不进入任务错误，避免启动摘要中的一次性凭据外泄。
- 原偶发 race 用例确认为测试观察时序：只等 recovery phase 即断言稍后落盘的事件 marker。30 次重复复现 3 次失败；改为等待 phase 与 marker 后，30 次 race 重复通过。未改变生产事件提交顺序。

真实验收（均通过 SSH MCP，限定 install-3 的 52000–52063 实例）：

| 用例 | 实际输入与期望 | 结果 |
| --- | --- | --- |
| 签名成功升级 | 1.1.0 → 1.1.1，导入 Ed25519 清单与 SHA-256 制品；候选身份匹配后开放业务 | 任务 f679afd32f0966fee2f400e959f19ddd = succeeded；后端/Redis/nginx 实际 executable 位于新 release；HTTPS 首页及 JS/CSS 与包内内容一致；配置值、证书、uploads 和 data 哨兵保留。config.env 当时只有空行增加，随后补幂等修复并用最终包再验。 |
| 迁移后候选失败 | 签名 1.1.2 故障夹具：迁移新增测试表和一行，nginx 模板故意加入非法指令；应自动恢复旧 release 和 SQLite 快照 | 任务 d48876d714b26215e5149ee642a6da66 = rolled_back，failure_phase=validating；诊断包含 unknown directive；自动恢复 1.1.1，gate=normal，HTTPS=200。外部证据记录 probe_rows=1；回退后测试表消失，备份中也无该表，sys_users 内容摘要保持一致，PRAGMA integrity_check=ok；配置及证书保持一致。没有手工清 gate 或修改任务终态。 |
| 最终包与持久端口 | 升级助手重启时移除所有临时 UVP_ZLM_* 环境变量，再导入最终签名 1.1.2 包；端口、配置与数据应保持 | 任务 47bd93dd6a61511146a13ed08e4a237e = succeeded；version=1.1.2，build_id=acceptance-20261018-final，gate=normal。捕获真实 candidate readiness 的 version/build/task/database_ready/dialect/migration_digest；config.yml、config.env、crt/key、uploads/data 哨兵 SHA-256 全部不变，用户记录摘要不变，SQLite 完整性 ok。HTTPS 首页/JS/CSS 与当前包逐字节一致，JS 含最终 build ID；上传文件 HTTPS 读取正确。 |

复测中的一次测试准备失误：SFTP 来源脚本没有执行位，复制到稳定入口后 RPC 报 permission denied；恢复 0755 后用正式 rollback 动作恢复。该任务 1c12faf84f1668a047518abbced98912 最终 rolled_back；其后使用新任务完成上述成功与自动回退，不复用失败任务伪造成功。

最终制品（220）：

- 安装包：/tmp/uvp-static-acceptance-20261018/release-final/uvp-gb28181-linux-amd64-1.1.2.tar.gz
- 安装包 SHA-256：f539ed8c9a806b9eb43acb86f84fb9d226209e85cb6814be97e3dc5c96ba1528
- 签名离线升级包：/tmp/uvp-static-acceptance-20261018/uvp-1.1.2-final.upgrade.tar
- 离线升级包 SHA-256：4791794d0c1cf1a300a5767717979419af4effd1e30fc5d7ed04b0565c293e40
- 机器证据：同目录 final-evidence.json、rollback-evidence.json、candidate-final-identity.json；本地副本位于 release-output/acceptance-20261018/。测试签名密钥未复制到交付包。

回归：部署 Python 18 项通过（含实际执行 shell 渲染和重复配置写入的 RED→GREEN 用例）；升级核心与助手完整 go test -race 通过；上述偶发用例连续 30 次 race 通过；签名/构建身份/助手服务 Node 17 项通过；前端生产构建通过；相关 shell bash -n、限定文件 git diff --check 通过。220 最终出包可移植性门禁通过（13 个 ELF，glibc 上限 2.17）。SQLite 属于隔离包内 fixture，未配置可连接它的 Database MCP，本轮通过该实例上的 Python sqlite3 验证；未操作业务数据库服务器。

边界：本轮使用管理员确认的离线 apply，未实测自建 HTTPS 在线下载、无人值守维护窗口、Docker Compose、systemd、arm64、宿主崩溃、外置数据库或正式站点发布。早先失败的历史任务及证据保留；其他实例、正式服务未部署或停止。稳定控制入口和升级助手是在本隔离实例中预先更新后验收，不能据此声称任意历史旧包能不经接入改造直接升级。

### 2026-10-18 — 系统管理下补齐服务升级菜单

- 老板确认入口放在“系统管理 → 服务升级”。保留动态路由 `/system/service-upgrade/index` 及原菜单 ID，静态隐藏直达路由继续兼容。
- 新增四方言 `2026-10-18-service-upgrade-menu` 增量：挂载系统目录、显示菜单及下载图标、补齐启用的 role 1 对系统目录/页面/查看/管理按钮的授权；其他角色不自动授权。SQL Server 中文使用 Unicode 字面量。down 保留管理配置，完整数据回退使用升级前数据库快照。
- 嵌入新迁移，并确保空迁移账本的新装快照也执行这条幂等数据迁移，避免因 API 注册已存在而漏过菜单授权。已有迁移账本通过新文件名正常补齐，不篡改历史迁移。
- RED：新增菜单行为及迁移账本用例，因四方言迁移文件/嵌入缺失失败；GREEN：`cd server && go test ./app/gb28181/migration -run '^(TestServiceUpgrade|TestRun|TestNeedsBaseline)' -count=1` 通过。覆盖新/旧菜单、重复执行、保留 ID、仅管理员授权、停用管理员不授予权限、空/非空账本。
- 扩大执行整个 migration 包未通过：包含既有历史迁移文件缺失（如 `2026-09-11-cascade-reconnect-retire*.sql`、`2026-09-21-target-track*.sql`）及其他基线断言失败，本次未修改无关迁移。
- 220 通过 SSH MCP 仅更新 install-3 的 SQLite；该包内 SQLite 无匹配数据库 MCP，因此使用 Python sqlite3，在事务前保存权限 0600 的数据库备份。应用与本地相同的 SQLite SQL，记录新版本账本。现场确认菜单 140616 的 parent_id=10，父路径 `/system`，hide/disable 均为 0；role 1 获得菜单和两个按钮权限；`PRAGMA integrity_check=ok`。无需重启或重新打包前端，刷新登录会话重新加载动态菜单。
- 浏览器 HTTPS 验收站可达，已进入登录页；没有现成登录会话，未宣称登录后的菜单点击/视觉验收通过。此轮未重新签名打包，先前 1.1.2 制品不包含本轮源码修正；后续出包会嵌入新增迁移。

### 2026-10-18 — 页面整理与版本说明部署任务

老板授权：按预览开始部署 220，仅 install-3 / 52000。

| 任务 | 测试输入 | 期望 |
| --- | --- | --- |
| 签名版本说明透传 | 清单含多行说明和发布时间，导入、准备、重开任务仓库；篡改暂存说明 | 导入和历史保留原说明；重验用签名清单纠正篡改；公开 DTO 保留说明，不泄露执行路径 |
| 页面信息分层 | 含长错误的历史任务、自动升级配置、未知迁移、只读账号 | 默认列表无长日志，详情可看；标签切换配置，权限/未保存状态/阻断不丢失 |
| 版本说明入口 | 在线/离线有说明、历史说明、无说明、HTML 字符串 | 应用前可看，当前及历史可回看，缺失明确提示，文本安全显示 |
| 构建与部署 | 1.1.3 / 独立 build ID，签名导入后升级 | 前后端身份一致、菜单和迁移正常、HTTPS 新资源可读、保留配置数据，实际页面验证 |

本轮结果（2026-10-18，220 install-3 / 52000）：

- 前端 26 项测试通过；升级核心及助手完整 `go test -race` 通过；公开 DTO、升级接口与权限相关控制器定向测试通过。前端生产构建成功。签名说明覆盖导入、暂存重验、创建任务及重新打开任务仓库；未经签名的暂存说明不能覆盖清单内容。
- 后端与助手在 220 的隔离源码快照编译，`CGO_ENABLED=0`。以先前验收的 1.1.2 整包为基础替换本轮程序、前端和版本元数据，生成独立 1.1.3 安装包；13 个 ELF 的可移植性门禁通过（glibc 上限 2.17）。未覆盖此前制品，未修改其他运行实例。
- 升级助手稳定入口先备份后原子替换并重启，继承原进程环境及注册配置，保留 process-authority 目录。仅在远端用既有测试私钥通过 Python cryptography 生成 Ed25519 签名及离线 tar（远端没有 Node）；私钥未下载或进入包。本轮没有实际接入发布站点。
- 已用助手真实 RPC 完成签名导入 → 创建任务 → 等待确认 → apply → succeeded。任务 `87b09640de0d7c7a4f9c596c601254d2` 的 committed=true，current 指向同名 release，version=1.1.3，build_id=acceptance-20261018-service-ui，gate=normal。未手工更改任务终态、迁移账本或维护门禁。
- 23 项部署检查全部通过：后端实际 executable 与新构建 SHA-256 相符；助手运行文件一致；HTTPS 首页、入口 JS/CSS 和服务升级页 chunk 与包内内容逐字节相同；前端含新 build ID；本轮说明与发布时间同时存在于助手查询和磁盘任务记录。
- 8 个配置/注册/证书/上传与数据哨兵文件 SHA-256 保留；用户表内容摘要不变；SQLite integrity_check=ok；`/public/uploads/upgrade-acceptance.txt` 的 HTTPS 内容正确。服务升级菜单 140616 位于 parent_id=10，hide/disable 均为 0，role 1 的页面及查看/管理按钮授权齐全。
- 使用与页面接口相同的 AvailableMigrationFiles / BuildMigrationPlan 对真实 SQLite 账本只读计算，can_apply=true，unknown/pending 均为空；临时检查程序已删除。没有删除未知记录绕过检查。该检查验证实际账本与本轮嵌入集合，不代表已通过登录页面调用 migration-plan API。
- 浏览器自动安全策略拒绝读取 220 的现有错误页，并明确禁止替代浏览器或间接绕过；因此未完成登录后的视觉与点击验收。保留这个边界，服务器资源可读不等于浏览器交互验收通过。

220 制品目录：`/tmp/uvp-static-acceptance-20261018/service-ui/`。

- 安装包：`uvp-gb28181-linux-amd64-1.1.3.tar.gz`，SHA-256 `db425d1d64f3caa871acafd60d9e1339544963489d92fb40a890a4da73280a6f`。
- 离线升级包：`uvp-1.1.3-service-ui.upgrade.tar`，SHA-256 `2a96470d79ebc25490c22db647b9e5c15c43ca4c91a390a05acf7a89f2d7a548`。
- 证据：`deployment-evidence.json`、`task-final.json`、`migration-plan.json`、`payload.json`、`portability.log`；本地副本位于 `release-output/acceptance-20261018-service-ui/`。

本轮验证限定绿色包管理员确认离线升级；Docker/systemd、在线站点和无人值守维护窗口未在本轮验收。源码未提交或推送。

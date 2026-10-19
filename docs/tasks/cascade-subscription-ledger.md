---
title: 国标级联订阅展示任务与测试
status: locally-verified
spec: ../specs/cascade-subscription-ledger.md
plan: ../plans/cascade-subscription-ledger.md
---

| 任务 | 测试输入 | 期望输出 | 结果 |
|---|---|---|---|
| S01 运行时快照 | 本/其他上级、多个 dialog、旧配置、取消、过期、关闭 | 仅有效本平台目录会话；未接通与不可用明确区分；复制不改内部状态 | 通过：平台隔离、旧配置/停用过滤、取消/到期/关闭、多会话及复制隔离 |
| S02 状态采集 | SUBSCRIBE、续订、NOTIFY 成功/失败 | 接受/续订/通知时间及状态码来自真实处理分支 | 通过：首次 pending、200 成功、500 失败、续订时间及并发读取 |
| S03 管理 API | 列表/详情、可用/不可用运行时 | JSON 包含四类状态和明细；不改变原查看权限，不暴露凭据 | 通过：列表/详情、本上级数据隔离、缺接口/无 runtime、凭据不泄露 |
| S04 台账组件 | 已订阅多会话、无订阅、未接通、未知、props 更新 | 四类可见；明细正确；状态变更可刷新；不读取设备侧接口 | 通过：四类状态、多会话明细、props 更新、旧 API/刷新失败显示未知 |
| S05 回归 | 相关 Go/前端测试、race、类型与差异检查 | 既有级联测试通过，说明环境和未验收边界 | 相关回归及 race 通过；全项目类型检查受范围外错误阻断，见下方 |

本地隔离测试，不访问浏览器、开发数据库、已有级联台账或真实上级。不包含无关版本与升级模块改动。

## 本地执行结果（2026-10-19）

- RED：新增 Go 测试因快照接口和 DTO 缺失编译失败；Vue 测试因组件缺失失败。日志：/tmp/uvp-subscription-ledger-red-go.log、/tmp/uvp-subscription-ledger-red-web.log。
- GREEN：三个新增快照测试（含并发读取）、一个管理 HTTP 列表/详情测试、三个组件测试，以及台账接入断言通过。HTTP 测试使用独立 SQLite，SIP 通知使用模拟传输。
- 后端回归：go test ./app/gb28181 ./app/gb28181/cascade/... ./app/gb28181/sip -count=1，14 个包通过；日志 /tmp/uvp-subscription-ledger-regression-go.log。
- 并发检查：go test -race ./app/gb28181 ./app/gb28181/cascade/controller ./app/gb28181/cascade/service ./app/gb28181/sip -run 'Cascade.*Subscription|SubscriptionSnapshot|ManagementHTTPIncludes|ManagementBusinessGroup' -count=1，4 个包通过；日志 /tmp/uvp-subscription-ledger-race.log。
- 前端回归：vitest run src/views/gb28181/cascade src/api/gb28181-cascade.test.ts，8 个文件、168 项通过；日志 /tmp/uvp-subscription-ledger-final-web.log。
- vue-tsc --noEmit 未通过：119 条错误位于本次改动以外的固件管理、设备升级、ZLM 等文件；日志 /tmp/uvp-subscription-ledger-typecheck.log 无本次修改文件的诊断。不能据此声称全项目类型检查或构建通过；未顺手修改这些模块。
- git diff --check 通过；新增组件与测试已按项目 Prettier 格式化。

## 展示与边界

平台列表和详情增加 subscriptions，含 catalog/mobilePosition/ptzPosition/alarm。目录状态来自上级已接受的 SUBSCRIBE 会话；其余三类返回 unsupported，前端显示“尚未接通”。目录会话只有状态和公开发布目标，不输出 SIP 路由、认证、源设备编码。

台账新增“上级订阅”列，复用既有十秒刷新；展开查看建立、最近续订、到期、最近通知事务结果。读取失败或旧 API 缺字段显示未知。会话结束后移除，重启后由上级重新订阅，本次不提供历史审计。500 暂时失败期间会话仍有效，明细显示失败；永久失败清理后不再计数。

本轮未操作浏览器、开发数据库、已有台账、真实上级、真实设备；未提交、推送、部署。工作区另有版本、升级等并行修改，不包含在本任务交付内。

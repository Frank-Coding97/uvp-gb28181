---
title: 级联目录订阅任务与测试
status: locally-verified
spec: ../specs/cascade-catalog-subscription.md
plan: ../plans/cascade-catalog-subscription.md
date: 2026-10-19
---

| 任务 | 测试输入 | 期望输出 |
|---|---|---|
| C01 范围与差分 | 平台/通道/业务/行政；新增/改名/离线/删除 | 仅发布范围；ADD/UPDATE/OFF/DEL；无变化无增量 |
| C02 编码 | 2016/2022、中文、批量、UDP 上限 | Notify、原 SN、Event；SumNum=本批 Num；无源 ID |
| C03 入口 | 合法/伪造来源、错误 Event、目标冲突 | SUBSCRIBE 独立 hook；合法 200，非法拒绝 |
| C04 生命周期 | 初始、续订、取消、到期、错误 tag/旧 CSeq | dialog 关联；首次离线通知；终止停止；旧请求拒绝 |
| C05 可靠性 | 名称/在线/共享变化、200/500/481/超时 | 确认后推进快照；有限重试；永久失败清理 |
| C06 隔离与关闭 | 多上级、配置变化、共享撤回、shutdown | 授权重验；DEL 无旧详情；工作退出；race 通过 |

按 RED → GREEN 推进，结果回填此处，不另建验收文档。未提交、推送、部署或实机验收。


## 本地结果（2026-10-19）

| 任务 | 结果 | 验证依据 |
|---|---|---|
| C01 范围与差分 | 通过 | 平台、通道、业务组、嵌套组织、行政区划；ADD/UPDATE/DEL/ON/OFF；未知/隐藏编码拒绝；重复应用幂等 |
| C02 编码 | 通过 | 2016/2022 profile、中文转码/转义、原 SN、独立批次 Num/SumNum、RegisterWay、2022 无 Owner、UDP 上限及超限拒绝 |
| C03 入口 | 通过 | Server 真实本地 UDP SUBSCRIBE 收包进入 hook 并回 200；来源、To/URI/XML、Event id、有效期、From tag 非法拒绝 |
| C04 生命周期 | 通过 | 首次离线通知；同 dialog 续订、无消息体续订、默认 600/兼容 Expire、取消、1 秒到期、旧 CSeq 与 URI/tag 变化拒绝 |
| C05 可靠性 | 通过 | 500 最多三次、481 立即清理、网络事务超时终止；失败批次不前移；UPDATE 重发；共享发布编码变化 DEL 后 ADD；多批中途撤回仍补 DEL |
| C06 隔离与关闭 | 通过 | 同源通道在两个上级使用不同发布编码；停用/修改配置/软删停止；共享撤销仅 DEL；shutdown 取消并等待在途通知；相关 race 通过 |

新增 29 个顶层测试函数（含子用例），其中 1 个覆盖基础业务目录保存的运行时同步。数量按当前新增文件及 service 追加测试统计，不代表 29 项国标认证。

RED 证据：目录/NOTIFY/订阅入口最初缺失而编译失败；新增测试曾检出初始多批中途撤回漏 DEL、Event 缺 id 被接受、缺 RegisterWay、严格路由未改写、基础业务目录保存不 Reload、续订 URI 可改换、接受后变更被并入初始基线、事务超时仍重复尝试，均修复后通过。

GREEN（server 目录）：

~~~sh
go test ./app/gb28181 ./app/gb28181/cascade/... ./app/gb28181/sip -count=1
go test -race ./app/gb28181 ./app/gb28181/cascade/catalog ./app/gb28181/cascade/sipclient ./app/gb28181/cascade/service ./app/gb28181/sip -run 'Cascade|Subscription|Notify|ManagementBusinessGroup' -count=1
~~~

第一条覆盖 14 个包全部测试通过；第二条筛选相关用例覆盖 5 个包全部通过。不是整个仓库的全量测试/全量 race。git diff --check 通过。

## 证据与交付边界

- 2022 标准公开 PDF 镜像（2026-10-19 读取）：https://ossrs.net/lts/zh-cn/assets/files/GBT28181-2022-e617420a7a79d48164835321c4ecc429.pdf 。按 N.2.1.2、N.4.2/N.4.3、A.2.1.9 和附录 M 复核。镜像版 PDF 阅读页与本地技能记录不完全一致，以条款号定位。
- RFC 6665：取消订阅终止原因 timeout；NOTIFY dialog/route/CSeq；超时和 408/481 的终止处理。
- 只有入站 SUBSCRIBE 做过本机 UDP 收包验证；完整上下行订阅链路使用隔离 SQLite、模拟传输，未连接真实上级或设备。
- 变更检测采用 5 秒最终状态快照比较，短时反复上下线可能合并。暂未做大规模订阅/目录负载压测；不声称实时事件完整性。
- VLOST/DEFECT 可编码，但当前目录事实只有 ON/OFF，没有自动生成异常事件；待可靠源事实接线。初始通知固定报告已知离线项，初始在线/离线上报的管理配置尚未新增。
- 会话仅存内存，重启后由上级重新订阅。M02 当时未增加订阅管理 UI/持久化台账；后续只读当前订阅台账展示已由 docs/tasks/cascade-subscription-ledger.md 补充，仍无历史持久化。
- 多上级相邻级联为当前范围；端到端跨级路由与第三方 Contact/Record-Route 互操作待联调。
- 保留无关工作区修改；未提交、推送、部署，未操作浏览器、开发数据库、已有级联台账或真实设备。M03/M04 不因本次 Catalog SUBSCRIBE 入口增加而自动完成。

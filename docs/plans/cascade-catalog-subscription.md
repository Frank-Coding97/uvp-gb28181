---
title: 级联目录订阅方案
status: locally-verified
spec: ../specs/cascade-catalog-subscription.md
date: 2026-10-19
---

## 数据流

SIP Server 增加独立 SUBSCRIBE hook。目录订阅管理器复用严格上级身份匹配和当前 Catalog publication。每个订阅保存 dialog、有效期、SN 和最近已确认快照，串行发送 NOTIFY。

catalog 包负责范围选择、目录差分和通知编码；sipclient 增加 NOTIFY envelope 和事务发送。保留摘要挑战与 dialog CSeq。设备侧订阅表语义不同，不混用；级联 dialog 存内存，重启后上级重新订阅。

## 变化来源与取舍

当前没有覆盖链路断开、目录入库和共享变化的统一事件总线。每个订阅每 5 秒受控读取其平台 publication，比较目录内容而非仅比较 revision；管理 Reload 触发再校验。避免覆盖设备 observer 或扩大到事件总线改造。此策略保留最终状态，短时反复上下线可能合并，不提供完整事件历史。

每批发送前检查当前配置及 projection revision。共享删除仅发送发布 ID 的 DEL；配置改变终止旧订阅。没有可靠异常事实时只发布 ON/OFF。

## 生命周期

初始有效期缺省 600 秒；接收 Expires，兼容 Expire。工程策略：接收有效期 0～86400 秒，默认 600 秒；服务端暂时失败最多尝试 3 次，通过后续扫描限速；NOTIFY 超时、408/481、永久错误或耗尽次数移除会话。有效期上限是本实现资源边界，不是国标固定值。关闭取消网络上下文并等待退出。重点验证撤权时序、批次确认和 race。


## 实现复核

- 初始在线假设绑定在接受订阅时读取的发布快照，避免 200 返回后、工作协程启动前的目录变化被吞并。
- 每批 SIP 200 后应用该批事件；中途投影变化时从已确认状态重新计算，避免漏掉已通知资源的 DEL。
- 续订验证原 dialog 的双方 URI/tag、Event id 和远端 CSeq；更新 Contact，但网络目的地址仍绑定已配置可信上级，不接受 Contact 重定向任意地址。
- Record-Route 按原顺序保留，分别构造严格路由和宽松路由的 NOTIFY；摘要挑战后保留传输实际使用的最大 CSeq。
- 共享撤回只传已发布 ID 的 DEL，目标整体消失后发送 terminated;reason=noresource；取消/到期使用 terminated;reason=timeout。
- 生产装配进入 cascadeRuntimeManager 与管理 Reload/Shutdown；基础 ReplaceBusinessGroup 保存成功后也触发 Reload。

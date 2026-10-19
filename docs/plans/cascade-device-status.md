---
title: 级联 DeviceStatus 技术方案
status: locally-verified
spec: ../specs/cascade-device-status.md
date: 2026-10-19
---

## 入口与数据流

在现有级联 MESSAGE 分派中增加独立 DeviceStatus handler，复用严格 peer 匹配、平台投影、协议 profile 和应答规划器，不改变 DeviceInfo/PTZ 行为。

校验上级与请求目标，读取本平台投影并确认发布权限，确认 SIP 收件；平台目标使用当前服务事实，已发布通道目标映射回源后复用既有 PTZ 查询调度执行器。等待关联 operation 完成，只读取与本 operation 关联的设备状态事实，生成保留 SN、发布 DeviceID 的业务应答；发送前复核平台启用与配置/投影版本。

## 风险与取舍

- 设备自检和在线事实未知时不能用 bool 零值冒充 OFFLINE/ERROR，返回业务 ERROR。
- 同设备并发更新可能替换缓存；不回退使用其他 operation 的事实。
- 查询有超时边界，设备失败与传输收件成功分开表达。
- 报警列表仅允许可靠映射到已发布资源；本轮无法映射时省略可选列表。
- 复用现有执行器和表结构，避免引入第二套操作调度或数据库迁移。

## 授权与查询结果边界

生产执行器要求捕获源设备正数 access_epoch，且 cleanup_completed_epoch 与之相等；设备、通道身份和归属须一致。将原 epoch 交给已有 intent/barrier 调度器，不创建第二套授权机制；读取结果后再次确认原 epoch、源通道未删除及身份/归属一致。关键事实和 epoch 查询走主库。

查询最多等待 25 秒；拒绝、超时、必填事实未知或关联缓存已被其他 operation 替换时返回业务 ERROR。平台停用或配置/投影版本改变时直接停止发送。源设备转移或通道删除/归属变化时不转发旧事实，只返回不含设备状态的 ERROR。

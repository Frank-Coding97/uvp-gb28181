---
title: 客户设备点播可靠性修复任务与测试结果
status: verified-production
date: 2026-10-19
---

| 任务 | 测试输入与场景 | 预期结果 | 结果 |
| --- | --- | --- | --- |
| 心跳端点刷新 | 注册51174，同IP心跳12230/63404；UDP/TCP | 新事务使用最新端口 | 10案例RED后GREEN |
| 信任边界 | From/XML不同、缺From、未注册/超期、IP/传输变化、port0 | 不改变路由与在线事实 | 定向回归与race通过 |
| 恢复语义 | 有效离线心跳；关闭心跳上线策略 | 一条恢复事件；关闭策略仍更新路由但保持离线 | 回归通过 |
| 并发与过期消息 | 旧心跳、新REGISTER、20个并发心跳 | 最终最新端点、单次恢复 | race通过；SQLite尾零时间CAS曾RED并修复 |
| SQLite超时 | +08纳秒、UTC、-07、195秒边界、旧无offset、周期0/-1 | 统一真实时刻，默认周期60秒 | RED后GREEN |
| 扫描竞态 | 扫描后收到新心跳，再执行置离线 | 保持在线且无离线事件 | RED后GREEN；UPDATE复检超时 |
| 实时会话BYE | INVITE/ACK/BYE真实sipgo，端口更新，Record-Route | 保留对话身份与代理Route；直连BYE跟随有效端口 | uac与sipgo定向race通过 |
| 注册核实 | 1424个携认证请求；正常0038与失败0003对比 | 区分认证挑战与摘要不匹配 | 正常样本74条精确匹配；0003摘要不符平台密码，另有不回复401的设备；未放宽认证 |
| 综合 | uac/device整包、四包定向race、sipgo普通整包、Linux构建 | 受影响模块通过 | 通过；全量sipgo race的既有计时器竞争在未改baseline亦复现，未扩改 |

## 发布来源与隔离

- 客户原1.1.5后端SHA256：`3ef63cf036f555e3da3588f2bed2386bb5516f645e9648e35c2c4ad5ca6b6386`，与本地1.1.5发布包完全一致。
- 从`release-output/1.1.5/source-1.1.5.tar.gz`独立解包，只覆盖本次14个文件；`sip/server.go`另按最小补丁加入lookup，排除其他任务级联修改。未修改原发布包。
- 构建目录：`/tmp/uvp-customer-nat-hotfix-20261019/source`；产物：`/tmp/uvp-customer-nat-hotfix-20261019/uvp-server`。
- Linux amd64/CGO0，BuildID `customer-1.1.5-natfix-20261019`，保留版本1.1.5。
- 新SHA256：`9d0075973f7fc3b8a2ed47900de079ae40e393a2ee08ff954cf86bf934b12ae1`；上传后重新核对一致。
- 旧客户包没有updater；采用有备份、候选预检、自动失败回退的单后端热修复。隔离fixture验证预检不写数据；客户配置只读预检exit0。
- 工作区修复尚未commit/push；保留所有其他任务脏改动及并行提交。

## 生产应用（北京时间）

- 目标：`124.236.76.233:22050`，目录`/data/docker/uvp/uvp-gb28181-linux-amd64-1.1.5`。
- 2026-10-19 19:36:52开始，19:36:56就绪；旧后端PID1492578，新PID2244019。先SIGTERM等待优雅退出，再备份data并检查SQLite quick_check。
- 备份：`/data/docker/uvp/backups/natfix-20261019-193652`，包含原二进制、version.json、config、data及受限权限environment.json；无需手工修改客户配置。
- ZLM PID1492513、nginx PID1492542、Redis PID1492558保持原启动时间；仅后端重启。SQLite schema前后摘要一致，无结构迁移。
- 实际HTTP后端为51002；51000为nginx HTTPS，51001为HTTP入口。后端验证码接口HTTP200并校验code0；SIP15060 TCP/UDP恢复监听。
- 19:37:25自动扫描将6台陈旧在线设备转为离线：尾号0037、0049、0012、0057、0010、0029。列表显示26在线/6离线，非人工改状态。
- 0006未重新REGISTER时，端口随连续heartbeat变化：47106→49184→…→12212，实际数据库与入站来源一致。

## 生产播放验收

- 0052：19:40:19 INVITE发往3054，100/200应答，19:40:20媒体就绪，19:40:22.937客户端first_frame。浏览器1920×1080、readyState4、paused=false，连续播放45秒以上；19:41:48 BYE200及cleanup_completed。
- 0006第一次打开复用升级前遗留媒体，出首帧，但不能据此证明新INVITE修复；该样本单独记录。
- 0006新会话：19:47:29 INVITE发往57328，收到200；19:47:32.282 first_frame。19:48:28 BYE使用心跳后端口32708，收到200，cleanup_completed，Call-ID保持一致。
- 0006重复新会话：19:50:18 INVITE发往35846并200，19:50:19媒体就绪，19:50:21.053 first_frame；浏览器1920×1080、readyState4、paused=false，已保存截图。
- 此重复会话19:52:28 BYE跟随心跳改发28636，仍保持原Call-ID，收到200；19:52:28.583 cleanup_completed。最终浏览器无video元素，已关闭本次新建验收标签，未关闭老板原标签。
- 另观察到0051、0019均有媒体就绪、客户端首帧和BYE200/cleanup_completed，来自现场其他正常操作。
- 截图：`/tmp/uvp-customer-nat-hotfix-20261019/evidence/device-0006-fresh-playing.jpg`。
- 收尾复核：生产PID2244019仍运行；验证码健康接口HTTP200/code0，SIP15060 TCP/UDP监听；部署二进制SHA256与隔离构建产物一致。

## 检查依据与边界

- 自动化日志：`/tmp/uvp-keepalive-red.log`、`/tmp/uvp-reliability-persisted-test.log`、`/tmp/uvp-reliability-scanner-red.log`、`/tmp/uvp-reliability-suite.log`、`/tmp/uvp-reliability-race.log`、`/tmp/uvp-isolated-reliability-race.log`、`/tmp/uvp-sipgo-focused-race.log`、`/tmp/uvp-sipgo-suite.log`。
- sipgo全量race既有失败：`/tmp/uvp-sipgo-race.log`；未改baseline复现：`/tmp/uvp-sipgo-baseline-race.log`，全局sip.SetTimers与残留事务定时器竞争，不能宣称全量race通过。
- 修复覆盖有效注册同IP同transport的心跳端口刷新及新建实时点播对话BYE；跨IP需REGISTER。历史回放/INFO/重启后持久化恢复对话的路由未在本次扩改。
- 注册摘要不匹配和未响应401的设备仍需设备侧核对凭据/固件；未批量改密码、关闭认证或宣称所有通道恢复。6台长期失联设备的网络/注册问题独立于已修复的在线状态误判。

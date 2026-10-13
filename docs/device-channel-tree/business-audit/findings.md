---
title: 手机模拟器业务分组核对
---

## 依据与边界
- 现场手机：cn.uvp.gb28181sim，1.0.3，安装更新时间 2026-10-09，配置 GB/T 28181-2022、UDP。
- 规范：GB/T 28181-2022 附录 J，印刷页 140–142（PDF 页147–149），已渲染核对原图。
- 手机源码位于 uvp-gb28181-sim；此前修订的桌面 Rust 模拟器位于 uvp-gb28181-sim-desktop，两者不能互相代替验收。
- 本次检查源码、手机已保存配置、真实 SIP Catalog、平台入库与真实业务树构造结果；未做完整登录态页面验收。
- 数据库 MCP 拒绝认证，回退本地开发服务配置进行只读查询。没有手工修改数据库；手机注册及 Catalog 查询会正常更新平台在线状态和目录。

## 标准含义及推荐展示
业务分组是按某项业务定义的资源组织根，不是平台部门或物理设备。一个业务分组包含虚拟组织，虚拟组织可嵌套并挂接设备资源。标准里的视频设备资源，在本平台通常对应可播放通道。

推荐业务树：业务分组(215) → 虚拟组织(216，可嵌套) → 视频通道。点击容器列出后代通道，点击叶子定位对应通道；保留来源下级平台及物理设备信息，不把下级注册连接重复插在每个业务分支里。

字段关系：
- 215 的 ParentID 指向制定此分组的系统 ID（200）。
- 首层 216 使用 BusinessGroupID 关联215；当上级是业务分组时不写 ParentID。
- 嵌套216的 ParentID 指向父虚拟组织，同时携带所属 BusinessGroupID。
- 设备 ParentID 表达父设备（无父设备时为系统）及虚拟组织，以 / 分隔；不能把这个字符串当成一个普通父ID。
- 设备未上报业务分组时业务视图为空，不从行政区划或平台部门自动制造业务分组。

## 手机现场结果
实抓 SN=812348、SumNum=6 的 Catalog 已保存为同目录 XML（原包GB18030，归档转为UTF-8，字段不变）。
- 根设备 37010301021180000007 为118类型，ParentID自指。
- 前置、后置摄像头及报警通道挂设备根。
- TestGroup（34020000002150000001）挂设备根。
- TestCam（34020000001320000021）直接挂TestGroup。
- 没有216，没有BusinessGroupID。这不能验收标准的215→216→资源链路。

手机实现问题：
1. 业务模板是设备根→业务分组→通道，缺少标准的系统/虚拟组织关系。
2. 编辑与校验要求普通父ID，无法直接表达首层216只通过BusinessGroupID关联。
3. 按业务分组查询只遍历parentId关系，漏掉仅BusinessGroupID关联的首层216及后代。
4. 216短标签显示为“区划”，与行政区划混淆。

定位：shared/src/commonMain/kotlin/com/uvp/sim/domain/CatalogTreeStore.kt、domain/coord/manscdp/CatalogSubRouter.kt、gb28181/CatalogNotifyBuilder.kt；composeApp/src/commonMain/kotlin/com/uvp/sim/ui/capability/catalog/。

## 平台结果
- 手机重新注册成功；平台日志确认Catalog 6条全部接收完成。
- TestGroup和TestCam已入库；调用真实BuildBusinessTree读取开发库返回TestGroup→TestCam，在线1/总数1。说明平台可兼容当前简化报文，但不证明标准业务树已完整支持。
- 已用SQLite隔离测试复现：同一通道先挂虚拟组织A，再更新为B，产生2条目录节点和2条挂载，只有1条主挂载，业务树仍同时显示在A、B下。旧归属未清理是确定缺陷。
- 根因：catalog/path_builder.go 的 findOrCreateNode 按父节点联合查找，catalog/upsert.go 只确保新目录及挂载，不撤销旧关系。
- 子节点早于父节点到达，以及多业务归属仍需补充独立用例；当前静态实现存在临时父节点与单parent_id投影风险，不将其写成已完成现场复现。

## 后续修复与验收顺序
1. 先修手机版的2022业务场景模板、关系序列化、按分组查询及标签；保留现有普通IPC模式。
2. 用独立标准样本验证平台首次入库、分批逆序、目录改名/移动/删除、通道换组和多归属，不仅做两端同一错误假设的往返测试。
3. 最后用手机上报标准场景，验证平台树、筛选、计数与通道实际点播。虚拟通道名称不代表已配置真实可播放媒体。

本轮为诊断，没有修改协议实现，也未覆盖安装手机APK。源码测试结果与手机已安装版本分别判断。

## 测试补充
- 手机源码使用 Android Studio JBR 启动 Gradle、Java17 toolchain：CatalogSubRouterTest 17、CatalogNotifyBuilderTest 14、CatalogTreeValidationTest 12、CatalogTreeStoreTest 7，共50项通过；现有测试未覆盖标准首层216关系。
- 平台catalog、directory、manscdp、subscribe定向测试通过；另用临时SQLite测试确认通道换虚拟组织后旧挂载残留。临时测试已清理。

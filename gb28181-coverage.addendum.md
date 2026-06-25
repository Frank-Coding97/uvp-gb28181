# GB28181 覆盖矩阵补充项

> 对应原文 `gb28181-coverage.md` 的缺口补齐稿。
> 这些项建议并入原矩阵的对应功能域。

## 功能域 0:基础设施与 SIP 协议栈

- [ ] SIP `OPTIONS`/`CANCEL`/错误响应矩阵与超时重试 `P1`
- [ ] `Allow` 头完整矩阵校验(注册/点播/回放/下载/控制/订阅) `P2`

## 功能域 3:设备信息与状态查询

- [ ] 设备/通道目录字段完整落库(Manufacturer/Model/CivilCode/Address/Parental/ParentID/RegisterWay/Secrecy/IP/Port/BusinessGroupID/PTZType/StreamNumberList/DownloadSpeed/SVC 能力/GrassrootsCode/PointType/FunctionType/EncodeType/MAC) `P1` `[2022]`
- [ ] 设备配置查询 `Query/ConfigDownload` 全量配置类型支持(BasicParam/VideoParamOpt/VideoParamAttribute/VideoRecordPlan/VideoAlarmRecord/PictureMask/FrameMirror/AlarmReport/OSDConfig/SnapShotConfig) `P2` `[2022]`

## 功能域 5:实时点播(直播)

- [ ] `Subject` 头生成/解析与同源流复用(单路上行、多端观看) `P1` `[2022]` 附录 L
- [ ] SDP 完整字段支持(`u` 资源定位/`t` 时间/`f` 媒体参数/`a=streamnumber`/`a=downloadspeed`/`a=filesize`/`a=setup`/`a=connection`) `P1` `[2022]`
- [ ] 媒体流保活/丢流释放(BYE 联动、流清理) `P1` `[2022]` 附录 K

## 功能域 6:录像查询与回放

- [ ] 录像检索条件完整支持(FilePath/Address/Secrecy/Type/RecorderID/IndistinctQuery/StreamNumber/AlarmMethod/AlarmType) `P1`
- [ ] 回放/下载结束通知(MediaStatus/NotifyType) `P2` `[2022]`
- [ ] 录像下载进度参数(`a=filesize`/`a=downloadspeed`)与文件大小反馈 `P2` `[2022]`

## 功能域 7:云台与设备控制

- [ ] 设备配置下发 `DeviceConfig` 全量配置类型支持(BasicParam/VideoParamOpt/VideoParamAttribute/VideoRecordPlan/VideoAlarmRecord/PictureMask/FrameMirror/AlarmReport/OSDConfig/SnapShotConfig) `P2` `[2022]`
- [ ] 存储卡格式化(FormatSDCard) `P3` `[2022]`
- [ ] 目标跟踪(TargetTrack) `P3` `[2022]`
- [ ] 设备升级结果通知(DeviceUpgradeResult) `P3` `[2022]`
- [ ] 图像抓拍完成通知(UploadSnapShotFinished) `P3` `[2022]`
- [ ] 设备实时视音频回传通知(VideoUploadNotify) `P3` `[2022]`

## 功能域 9:语音

- [ ] 语音输出通道目录项上报/广播到主设备(父设备 -> 语音输出设备) `P2` `[2022]`

## 功能域 11:媒体处理与 ZLMediaKit 对接

- [ ] RTP over TCP 媒体协商(`a=setup`/`a=connection`, 单消息 > MTU 自动转 TCP) `P1` `[2022]` 附录 D/G/M
- [ ] H.265/AAC/G.711 之外的兼容矩阵(MPEG-4/SVAC/G.723.1/G.729/G.722.1) `P3` `[2022]`

## 功能域 12:平台级联(上下级互联)

- [ ] 多路径级联头域支持(`X-RoutePath`/`X-PreferredPath`) `P3` `[2022]` 附录 H

## 功能域 14:平台工程化能力

- [ ] 注册编码白名单/信令来源校验/非法设备拦截 `P1`
- [ ] SIP over TLS/IPSec + SM3 + `Date`/`Note` + `Monitor-User-Identity` `P2` `[2022]`
- [ ] 数字证书认证/高安全级别(单向/双向) `P3` `[2022]`


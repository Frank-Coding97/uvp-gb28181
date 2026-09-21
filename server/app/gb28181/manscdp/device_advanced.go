package manscdp

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"strconv"
	"strings"

	"uvplatform.cn/uvp-gb28181/app/gb28181/protocol"
)

// XMLCharset is limited to the declarations used by GB28181 devices.
type XMLCharset string

const (
	XMLCharsetGB2312  XMLCharset = "GB2312"
	XMLCharsetGB18030 XMLCharset = "GB18030"
	XMLCharsetUTF8    XMLCharset = "UTF-8"
)

type RecordAction string

const (
	RecordStart RecordAction = "Record"
	RecordStop  RecordAction = "StopRecord"
)

type GuardAction string

const (
	GuardSet   GuardAction = "SetGuard"
	GuardReset GuardAction = "ResetGuard"
)

type DragZoomDirection string

const (
	DragZoomIn  DragZoomDirection = "DragZoomIn"
	DragZoomOut DragZoomDirection = "DragZoomOut"
)

// AlarmResetOptions narrows a reset to a GB28181 alarm method and/or type.
// Empty fields request the device's all-alarm reset semantics.
type AlarmResetOptions struct {
	AlarmMethod string `xml:"AlarmMethod,omitempty" json:"alarmMethod,omitempty"`
	AlarmType   string `xml:"AlarmType,omitempty" json:"alarmType,omitempty"`
}

// AlarmMethod is the standard GB/T 28181 AlarmMethod code. AlarmMethodAll
// (zero) requests a reset for every alarm method; the remaining values may be
// combined with "/" (for example, "1/5") in AlarmResetOptions.
type AlarmMethod uint8

const (
	AlarmMethodAll         AlarmMethod = 0
	AlarmMethodPhone       AlarmMethod = 1
	AlarmMethodDevice      AlarmMethod = 2
	AlarmMethodSMS         AlarmMethod = 3
	AlarmMethodGPS         AlarmMethod = 4
	AlarmMethodVideo       AlarmMethod = 5
	AlarmMethodDeviceFault AlarmMethod = 6
	AlarmMethodManual      AlarmMethod = 7
)

// AlarmType is the standard GB/T 28181 AlarmType code used by AlarmCmd. The
// current standard set is deliberately finite; unknown vendor values must be
// rejected before a control operation is persisted or sent.
type AlarmType uint8

const (
	AlarmTypeVideoLost    AlarmType = 1
	AlarmTypeDeviceTamper AlarmType = 2
	AlarmTypeStorageFull  AlarmType = 3
	AlarmTypeDeviceFault  AlarmType = 4
	AlarmTypeOther        AlarmType = 5
	// AlarmTypeVideoMax is the highest video-alarm subtype defined by the
	// 2022 profile. AlarmType values are method-specific, so the numeric
	// aliases above remain useful for the device-alarm method.
	AlarmTypeVideoMax AlarmType = 13
)

// DragZoomRegion uses the six integer fields defined by GB28181. Length and
// Width describe the playback window; the remaining fields describe the box.
type DragZoomRegion struct {
	Length    int `xml:"Length" json:"length"`
	Width     int `xml:"Width" json:"width"`
	MidPointX int `xml:"MidPointX" json:"midPointX"`
	MidPointY int `xml:"MidPointY" json:"midPointY"`
	LengthX   int `xml:"LengthX" json:"lengthX"`
	LengthY   int `xml:"LengthY" json:"lengthY"`
}

type DragZoomCommand struct {
	Direction DragZoomDirection `json:"direction"`
	Region    DragZoomRegion    `json:"region"`
}

// TargetTrackMode 是 GB/T 28181-2022 A.2.3.1.14 `TargetTrack` 元素的取值域。
// 标准注释原文：「目标跟踪命令（可选），"Auto"为自动跟踪、"Manual"为手动跟踪（指哪打哪），
// 携带全景图片中框选的区域坐标信息」。
type TargetTrackMode string

const (
	// TargetTrackAuto：设备按已配置参数自行跟踪，**无需平台下发坐标**。
	TargetTrackAuto TargetTrackMode = "Auto"
	// TargetTrackManual：指哪打哪 —— 平台把框选坐标发下去，球机按坐标转过去。
	TargetTrackManual TargetTrackMode = "Manual"
	// TargetTrackStop：停止跟踪。
	TargetTrackStop TargetTrackMode = "Stop"
)

// TargetTrackArea 是 A.2.3.1.14 `TargetArea` 的子元素组（全景播放窗口尺寸 + 跟踪框）。
//
// ⛔ 这六个元素的名与义与 A.2.3.1.8/A.2.3.1.9 的 DragZoomIn/DragZoomOut **逐个相同**
// （`Length`/`Width`/`MidPointX`/`MidPointY`/`LengthX`/`LengthY`），所以这里直接**别名复用**
// [DragZoomRegion]：两份同构的 6 字段结构各自演化时，改一处忘一处会静默走偏。
// ⚠️ 名同义同但**语义基准不同**：DragZoom 的 Length/Width 是"播放窗口"，TargetTrack 的
// 是"**全景**播放窗口"（A.2.3.1.14 注释「全景图片大小、框选的区域坐标信息」）。
type TargetTrackArea = DragZoomRegion

// TargetTrackCommand 是一次目标跟踪下发的全部可选信息（A.2.3.1.14）。
//
// DeviceID2 是「全景相机中的全景通道ID」，可选；SN 之后的 DeviceID（由 [BuildTargetTrackControlWithProfile]
// 的 deviceID 参数给出）才是**必选**的"全景相机的球机通道"（标准尾注原文：
// 「SN后面的目标设备编码（必选）指全景相机的球机通道」）。
// ⇒ 两者**不是同一个通道**，别把球机编码当成全景通道填进 DeviceID2。
type TargetTrackCommand struct {
	Mode      TargetTrackMode  `json:"mode"`
	DeviceID2 string           `json:"deviceId2,omitempty"`
	Area      *TargetTrackArea `json:"area,omitempty"`
}

// ParseTargetTrackMode 解析前端/调用方给出的跟踪模式。
//
// 大小写不敏感（HTTP 层习惯写小写），但**落到报文里的永远是标准枚举的原样拼写**
// —— 设备的 XML 解析多半是大小写敏感的字符串比较，放宽输入不等于放宽输出。
func ParseTargetTrackMode(raw string) (TargetTrackMode, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "auto":
		return TargetTrackAuto, nil
	case "manual":
		return TargetTrackManual, nil
	case "stop":
		return TargetTrackStop, nil
	default:
		return "", fmt.Errorf("目标跟踪模式 %q 不是 Auto/Manual/Stop", raw)
	}
}

// ValidateTargetTrackCommand 在下发前校验命令。
//
// 三条口径，都直接来自标准原文：
//  1. **Manual 必须带 TargetArea**：注释明写「全景图片大小、框选的区域坐标信息…**手动跟踪时需要**」。
//     缺了就是"收到了跟踪指令但没有目标"——设备侧完全不可观测，比不发更糟。
//  2. **Stop 不接受 TargetArea**：停跟踪再带一个跟踪框是自相矛盾的指令，属于调用方 bug，
//     在报文层挡住而不是替设备猜意图。
//  3. **Auto 允许可选携带 TargetArea**：标准正文说「自动或手动跟踪命令携带全景画面中框选的
//     区域坐标信息」，同时又注释"自动跟踪无需平台下发坐标参数"——两种读法都在标准里，
//     所以这里**只做结构校验不做拒绝**，发不发由调用方决定。
//
// ⛔ 与 [validateDragZoomRegion] 刻意**不共用**的检查：这里**不要求框完整落在窗口内**。
// 拉框放大是平台自己算出来的区域，越界一定是我们算错了；而目标跟踪的框是操作员在画面上
// 拖出来的，贴着画面边缘、甚至被 UI 裁剪掉半个框都是正常操作，用同一条"必须完整包含"
// 的规则会把最靠边的目标挡在门外。设备侧本来就要做比例换算，钳位是它的事。
func ValidateTargetTrackCommand(command TargetTrackCommand) error {
	switch command.Mode {
	case TargetTrackAuto:
	case TargetTrackManual:
		if command.Area == nil {
			return fmt.Errorf("手动目标跟踪必须携带 TargetArea 框选坐标")
		}
	case TargetTrackStop:
		if command.Area != nil {
			return fmt.Errorf("停止目标跟踪不应携带 TargetArea 框选坐标")
		}
	default:
		return fmt.Errorf("目标跟踪模式 %q 不是 Auto/Manual/Stop", command.Mode)
	}
	if command.Area == nil {
		return nil
	}
	area := *command.Area
	if area.Length <= 0 || area.Width <= 0 {
		return fmt.Errorf("目标跟踪全景播放窗口尺寸必须为正数")
	}
	if area.LengthX <= 0 || area.LengthY <= 0 {
		return fmt.Errorf("目标跟踪框尺寸必须为正数")
	}
	if area.MidPointX < 0 || area.MidPointX > area.Length {
		return fmt.Errorf("目标跟踪框中心横轴坐标超出全景播放窗口")
	}
	if area.MidPointY < 0 || area.MidPointY > area.Width {
		return fmt.Errorf("目标跟踪框中心纵轴坐标超出全景播放窗口")
	}
	return nil
}

type advancedDeviceControl struct {
	XMLName      xml.Name           `xml:"Control"`
	CmdType      string             `xml:"CmdType"`
	SN           int                `xml:"SN"`
	DeviceID     string             `xml:"DeviceID"`
	IFameCmd     string             `xml:"IFameCmd,omitempty"`
	IFrameCmd    string             `xml:"IFrameCmd,omitempty"`
	RecordCmd    string             `xml:"RecordCmd,omitempty"`
	StreamNumber *int               `xml:"StreamNumber,omitempty"`
	GuardCmd     string             `xml:"GuardCmd,omitempty"`
	AlarmCmd     string             `xml:"AlarmCmd,omitempty"`
	TeleBoot     string             `xml:"TeleBoot,omitempty"`
	Info         *AlarmResetOptions `xml:"Info,omitempty"`
	DragZoomIn   *DragZoomRegion    `xml:"DragZoomIn,omitempty"`
	DragZoomOut  *DragZoomRegion    `xml:"DragZoomOut,omitempty"`
	// FormatSDCard 是 A.2.3.1.13「存储卡格式化控制命令」的取值 —— **就是 SD 卡编号本身**
	// （从 1 开始编号；取 0 表示对所有存储卡格式化）。标准里它是 DeviceControl 体内一个
	// 与 SN/DeviceID 同级的 integer 元素，**没有 DiskNum 之类的包装元素**。
	// ⛔⛔ 必须是指针：`omitempty` 对 int 零值会**整个省略该字段**，而 0 恰好是合法取值
	//   （= 格式化全部卡），用值类型会把「格式化所有卡」静默发成一条不含该元素的命令。
	FormatSDCard *int `xml:"FormatSDCard,omitempty"`
	// 以下三个元素是 A.2.3.1.14「目标跟踪控制命令」（标准页 77-78），也是 A.2.3.1
	// 那条 `Control` 序列里**最后**的三个（排在 FormatSDCard 之后）——字段顺序即报文顺序，
	// 别把它们插到中间去。
	//
	// TargetTrack 是 Auto|Manual|Stop 三选一；DeviceID2 是"全景相机中的全景通道ID"（可选）；
	// TargetArea 是全景窗口尺寸 + 跟踪框（手动跟踪时需要）。
	// ⛔ DeviceID2 与 SN 之后的 DeviceID **不是同一个编码**：后者必选，指"全景相机的球机通道"。
	TargetTrack string          `xml:"TargetTrack,omitempty"`
	DeviceID2   string          `xml:"DeviceID2,omitempty"`
	TargetArea  *DragZoomRegion `xml:"TargetArea,omitempty"`
}

func BuildIFrameControl(deviceID string, sn int, charset XMLCharset) ([]byte, error) {
	profile, err := advancedProfileForCharset(charset)
	if err != nil {
		return nil, err
	}
	return BuildIFrameControlWithProfile(profile, deviceID, sn)
}

func BuildRecordControl(deviceID string, sn int, action RecordAction, charset XMLCharset) ([]byte, error) {
	if action != RecordStart && action != RecordStop {
		return nil, fmt.Errorf("不支持的设备录像动作: %q", action)
	}
	profile, err := advancedProfileForCharset(charset)
	if err != nil {
		return nil, err
	}
	return BuildRecordControlWithProfile(profile, deviceID, sn, action)
}

func BuildGuardControl(deviceID string, sn int, action GuardAction, charset XMLCharset) ([]byte, error) {
	if action != GuardSet && action != GuardReset {
		return nil, fmt.Errorf("不支持的布撤防动作: %q", action)
	}
	profile, err := advancedProfileForCharset(charset)
	if err != nil {
		return nil, err
	}
	return BuildGuardControlWithProfile(profile, deviceID, sn, action)
}

func BuildAlarmResetControl(deviceID string, sn int, options AlarmResetOptions, charset XMLCharset) ([]byte, error) {
	profile, err := advancedProfileForCharset(charset)
	if err != nil {
		return nil, err
	}
	return BuildAlarmResetControlWithProfile(profile, deviceID, sn, options)
}

func BuildTeleBootControl(deviceID string, sn int, confirmed bool, charset XMLCharset) ([]byte, error) {
	if !confirmed {
		return nil, fmt.Errorf("远程重启需要显式确认")
	}
	profile, err := advancedProfileForCharset(charset)
	if err != nil {
		return nil, err
	}
	return BuildTeleBootControlWithProfile(profile, deviceID, sn, confirmed)
}

func BuildDragZoomControl(deviceID string, sn int, command DragZoomCommand, charset XMLCharset) ([]byte, error) {
	if command.Direction != DragZoomIn && command.Direction != DragZoomOut {
		return nil, fmt.Errorf("不支持的 3D 定位动作: %q", command.Direction)
	}
	if err := validateDragZoomRegion(command.Region); err != nil {
		return nil, err
	}
	profile, err := advancedProfileForCharset(charset)
	if err != nil {
		return nil, err
	}
	return BuildDragZoomControlWithProfile(profile, deviceID, sn, command)
}

// BuildFormatSDCardControl builds a storage-card format command.
//
// ⛔ 破坏性动作：调用方必须先拿到**显式确认**（同族先例是 teleboot 的 confirmed 门禁），
// 本函数只负责参数合法性与报文正确，不做业务/权限门禁。
func BuildFormatSDCardControl(deviceID string, sn int, cardIndex int, charset XMLCharset) ([]byte, error) {
	profile, err := advancedProfileForCharset(charset)
	if err != nil {
		return nil, err
	}
	return BuildFormatSDCardControlWithProfile(profile, deviceID, sn, cardIndex)
}

// BuildFormatSDCardControlWithProfile builds the storage-card format command.
//
// 标准原文（GB/T 28181-2022 A.2.3.1.13「存储卡格式化控制命令」，标准页 77-78）：
//
//	<!-- 存储卡格式化命令（可选）-->
//	<element name="FormatSDCard" minOccurs="0">
//	  <simpleType>
//	    <restriction base="integer">
//	      <!-- SD 卡编号，从1开始编号。该值0时，对所有存储卡进行格式化-->
//	      <minInclusive value="0"/>
//	    </restriction>
//	  </simpleType>
//	</element>
//
// ⇒ 元素**值本身就是卡编号**，与 SN/DeviceID 同级直接拼在 DeviceControl 体内，
// 形如 `<FormatSDCard>1</FormatSDCard>`。
//
// ⛔ 别改成 `<FormatSDCard><DiskNum>1</DiskNum></FormatSDCard>`：本仓曾有一处注释把
// `DiskNum` 当成本命令的标准字段，但它在 2022 全文 / 2022 附录 A / 2016 附录 A
// **三处均 0 命中**，是自造名（2026-09-20 核）。
//
// ⛔ 版本口径：本命令是 **2022 独有**（`FormatSDCard` 2022 附录 A 命中 1 次、2016 附录 A 0 命中），
// 但这里**不按版本拒发** —— 与 SDCardStatus 查询同口径（那条同样是 2022 独有），
// 设备是否真支持交给调用方的门禁/操作员判断，协议层只负责把报文拼对。
//
// 上界不校验：标准只给了 `minInclusive=0`（无 maxInclusive）。同族应答
// A.2.6.16 的 `Item` 是 `maxOccurs="8"` ⇒ 一台设备的卡数上限是 8，正常路径下
// 编号来自实际卡列表、不会越界；硬编码上界反而会挡住将来卡数更多的设备。
func BuildFormatSDCardControlWithProfile(profile protocol.Profile, deviceID string, sn int, cardIndex int) ([]byte, error) {
	if cardIndex < 0 {
		return nil, fmt.Errorf("存储卡编号不能为负数: %d", cardIndex)
	}
	return marshalAdvancedControlWithProfile(profile, deviceID, sn, advancedDeviceControl{FormatSDCard: &cardIndex})
}

// BuildTargetTrackControl 按目标跟踪命令构造一帧 DeviceControl（A.2.3.1.14）。
func BuildTargetTrackControl(deviceID string, sn int, command TargetTrackCommand, charset XMLCharset) ([]byte, error) {
	profile, err := advancedProfileForCharset(charset)
	if err != nil {
		return nil, err
	}
	return BuildTargetTrackControlWithProfile(profile, deviceID, sn, command)
}

// BuildTargetTrackControlWithProfile 构造「目标跟踪控制命令」（GB/T 28181-2022 A.2.3.1.14）。
//
// 标准原文（标准页 77-78）：
//
//	<!-- 全景摄像机球机画面中目标进行自动及手动跟踪控制命令。
//	     手动跟踪：在平台端全景画面上进行框选时，平台会将目标框的具体坐标发送给设备，
//	               设备中的球机根据该坐标执行跟踪动作。由于平台与设备画面比例大小不同，
//	               需要进行比例关系转化。因此，平台应提供画面大小：播放窗口长度像素值和
//	               播放窗口宽度像素值。
//	     自动跟踪：平台把这个命令发送给设备，设备根据已配置参数执行跟踪操作，
//	               无需平台下发坐标参数。 -->
//	<element name="TargetTrack" minOccurs="0">
//	  <simpleType><restriction base="string">
//	    <enumeration value="Auto"/><enumeration value="Manual"/><enumeration value="Stop"/>
//	  </restriction></simpleType>
//	</element>
//	<!-- DeviceID2 目标设备编码（可选），指全景相机中的全景通道ID -->
//	<element name="DeviceID2" type="tg:deviceIDType" minOccurs="0"/>
//	<!-- 全景图片大小、框选的区域坐标信息（目标框长宽及中心点坐标），可选，手动跟踪时需要 -->
//	<element name="TargetArea" minOccurs="0">… Length/Width/MidPointX/MidPointY/LengthX/LengthY …</element>
//	注：SN后面的目标设备编码（必选）指全景相机的球机通道。
//
// deviceID 参数 = **球机通道**，不是全景通道；全景通道走 command.DeviceID2。
//
// ⛔ 标准这里**没有**要求平台替设备做比例换算 —— 原文是"平台应提供画面大小"，
// 换算由设备按平台给出的窗口尺寸自己完成。所以平台侧只要把"用户实际看到的播放窗口
// 像素尺寸 + 该窗口坐标系里的框选坐标"如实发出去即可，别自作主张乘一个宽高比。
//
// ⚠️ 本命令是 2022 独有（`TargetTrack` 在 2016 附录 A 零命中），但这里**不按版本拒发**
// —— 与 FormatSDCard / SDCardStatus 同口径：profile 只是"登记的说法"，是否真支持
// 由调用方的门禁与操作员判断，协议层只负责把报文拼对。
func BuildTargetTrackControlWithProfile(profile protocol.Profile, deviceID string, sn int, command TargetTrackCommand) ([]byte, error) {
	if err := ValidateTargetTrackCommand(command); err != nil {
		return nil, err
	}
	fields := advancedDeviceControl{TargetTrack: string(command.Mode)}
	if deviceID2 := strings.TrimSpace(command.DeviceID2); deviceID2 != "" {
		fields.DeviceID2 = deviceID2
	}
	if command.Area != nil {
		area := *command.Area
		fields.TargetArea = &area
	}
	return marshalAdvancedControlWithProfile(profile, deviceID, sn, fields)
}

func marshalAdvancedControl(deviceID string, sn int, charset XMLCharset, fields advancedDeviceControl) ([]byte, error) {
	profile, err := advancedProfileForCharset(charset)
	if err != nil {
		return nil, err
	}
	return marshalAdvancedControlWithProfile(profile, deviceID, sn, fields)
}

// BuildIFrameControlWithProfile builds the force-key-frame command using the
// profile's field spelling: deployed 2016 devices commonly require the
// historical IFameCmd typo, while 2022 uses IFrameCmd.
func BuildIFrameControlWithProfile(profile protocol.Profile, deviceID string, sn int) ([]byte, error) {
	return marshalAdvancedControlWithProfile(profile, deviceID, sn, advancedDeviceControl{IFameCmd: "Send"})
}

// BuildRecordControlWithProfile builds a device-side recording command.
func BuildRecordControlWithProfile(profile protocol.Profile, deviceID string, sn int, action RecordAction) ([]byte, error) {
	if action != RecordStart && action != RecordStop {
		return nil, fmt.Errorf("不支持的设备录像动作: %q", action)
	}
	control := advancedDeviceControl{RecordCmd: string(action)}
	if profile.Version == protocol.Version2022 {
		streamNumber := 0
		control.StreamNumber = &streamNumber
	}
	return marshalAdvancedControlWithProfile(profile, deviceID, sn, control)
}

// BuildGuardControlWithProfile builds a guard/unguard command.
func BuildGuardControlWithProfile(profile protocol.Profile, deviceID string, sn int, action GuardAction) ([]byte, error) {
	if action != GuardSet && action != GuardReset {
		return nil, fmt.Errorf("不支持的布撤防动作: %q", action)
	}
	return marshalAdvancedControlWithProfile(profile, deviceID, sn, advancedDeviceControl{GuardCmd: string(action)})
}

// BuildAlarmResetControlWithProfile builds AlarmCmd=ResetAlarm. Empty
// options intentionally mean all alarms; non-empty values are validated
// against the standard AlarmMethod/AlarmType enumerations first.
func BuildAlarmResetControlWithProfile(profile protocol.Profile, deviceID string, sn int, options AlarmResetOptions) ([]byte, error) {
	options.AlarmMethod = strings.TrimSpace(options.AlarmMethod)
	options.AlarmType = strings.TrimSpace(options.AlarmType)
	if profile.Version != protocol.Version2022 && (options.AlarmMethod != "" || options.AlarmType != "") {
		return nil, fmt.Errorf("2016 AlarmCmd 不支持 AlarmMethod/AlarmType selectors")
	}
	if err := ValidateAlarmResetOptions(options); err != nil {
		return nil, err
	}
	control := advancedDeviceControl{AlarmCmd: "ResetAlarm"}
	if options.AlarmMethod != "" || options.AlarmType != "" {
		control.Info = &options
	}
	return marshalAdvancedControlWithProfile(profile, deviceID, sn, control)
}

// BuildTeleBootControlWithProfile builds a confirmed remote reboot command.
func BuildTeleBootControlWithProfile(profile protocol.Profile, deviceID string, sn int, confirmed bool) ([]byte, error) {
	if !confirmed {
		return nil, fmt.Errorf("远程重启需要显式确认")
	}
	return marshalAdvancedControlWithProfile(profile, deviceID, sn, advancedDeviceControl{TeleBoot: "Boot"})
}

// BuildDragZoomControlWithProfile builds a structured 3D drag command with
// actual playback-window pixels from the caller.
func BuildDragZoomControlWithProfile(profile protocol.Profile, deviceID string, sn int, command DragZoomCommand) ([]byte, error) {
	if command.Direction != DragZoomIn && command.Direction != DragZoomOut {
		return nil, fmt.Errorf("不支持的 3D 定位动作: %q", command.Direction)
	}
	if err := validateDragZoomRegion(command.Region); err != nil {
		return nil, err
	}
	control := advancedDeviceControl{}
	if command.Direction == DragZoomIn {
		control.DragZoomIn = &command.Region
	} else {
		control.DragZoomOut = &command.Region
	}
	return marshalAdvancedControlWithProfile(profile, deviceID, sn, control)
}

func advancedProfileForCharset(charset XMLCharset) (protocol.Profile, error) {
	profile := protocol.ProfileFor(protocol.Version2016)
	switch charset {
	case XMLCharsetGB2312:
		profile.Charset = protocol.CharsetGB2312
	case XMLCharsetGB18030:
		profile.Charset = protocol.CharsetGB18030
	case XMLCharsetUTF8:
		profile.Charset = protocol.CharsetUTF8
	default:
		return protocol.Profile{}, fmt.Errorf("不支持的 XML 编码声明: %q", charset)
	}
	return profile, nil
}

func marshalAdvancedControlWithProfile(profile protocol.Profile, deviceID string, sn int, fields advancedDeviceControl) ([]byte, error) {
	deviceID = strings.TrimSpace(deviceID)
	if deviceID == "" {
		return nil, fmt.Errorf("设备或通道编码不能为空")
	}
	if sn <= 0 {
		return nil, fmt.Errorf("SN 必须为正数")
	}
	if profile.Version == "" {
		profile = protocol.ProfileFor(protocol.Version2016)
	}
	if profile.IFrameElement == "" {
		profile.IFrameElement = protocol.ProfileFor(profile.Version).IFrameElement
	}
	fields.CmdType = CmdDeviceControl
	fields.SN = sn
	fields.DeviceID = deviceID
	if value := strings.TrimSpace(fields.IFameCmd); value != "" || strings.TrimSpace(fields.IFrameCmd) != "" {
		if value == "" {
			value = strings.TrimSpace(fields.IFrameCmd)
		}
		fields.IFameCmd = ""
		fields.IFrameCmd = ""
		if profile.IFrameElement == "IFrameCmd" {
			fields.IFrameCmd = value
		} else {
			fields.IFameCmd = value
		}
	}
	return MarshalProfiledXML(profile, fields)
}

// ParseAlarmMethod parses the optional slash-separated AlarmMethod selector.
// Zero means all methods and therefore cannot be combined with another code.
func ParseAlarmMethod(raw string) ([]AlarmMethod, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	parts := strings.Split(raw, "/")
	result := make([]AlarmMethod, 0, len(parts))
	seen := make(map[AlarmMethod]struct{}, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			return nil, fmt.Errorf("AlarmMethod 包含空枚举值")
		}
		value, err := strconv.Atoi(part)
		if err != nil || value < int(AlarmMethodAll) || value > int(AlarmMethodManual) {
			return nil, fmt.Errorf("AlarmMethod %q 不在 0-7 标准范围内", raw)
		}
		method := AlarmMethod(value)
		if _, exists := seen[method]; exists {
			return nil, fmt.Errorf("AlarmMethod %q 包含重复枚举值", raw)
		}
		if method == AlarmMethodAll && len(parts) != 1 {
			return nil, fmt.Errorf("AlarmMethod=0 不能与其他报警方式组合")
		}
		seen[method] = struct{}{}
		result = append(result, method)
	}
	return result, nil
}

// ParseAlarmType parses the standard AlarmType selector. AlarmType does not
// have the AlarmMethod slash-combination syntax; an empty value is omitted.
func ParseAlarmType(raw string) (AlarmType, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < int(AlarmTypeVideoLost) || value > int(AlarmTypeVideoMax) {
		return 0, fmt.Errorf("AlarmType %q 不在 1-13 标准范围内", raw)
	}
	return AlarmType(value), nil
}

// ValidateAlarmResetOptions validates both optional selectors before XML is
// built. This keeps malformed alarm combinations out of persisted operations.
func ValidateAlarmResetOptions(options AlarmResetOptions) error {
	methods, err := ParseAlarmMethod(options.AlarmMethod)
	if err != nil {
		return err
	}
	alarmType, err := ParseAlarmType(options.AlarmType)
	if err != nil {
		return err
	}
	if options.AlarmType == "" {
		return nil
	}
	if len(methods) == 0 {
		return fmt.Errorf("AlarmType 必须与 AlarmMethod=2/5/6 一起提供")
	}
	for _, method := range methods {
		min, max, ok := alarmTypeRange(method)
		if !ok {
			return fmt.Errorf("AlarmMethod=%d 不支持携带 AlarmType", method)
		}
		if alarmType < min || alarmType > max {
			return fmt.Errorf("AlarmMethod=%d 的 AlarmType 必须在 %d-%d 范围内", method, min, max)
		}
	}
	return nil
}

func alarmTypeRange(method AlarmMethod) (AlarmType, AlarmType, bool) {
	switch method {
	case AlarmMethodDevice:
		return AlarmTypeVideoLost, AlarmTypeOther, true
	case AlarmMethodVideo:
		return AlarmTypeVideoLost, AlarmTypeVideoMax, true
	case AlarmMethodDeviceFault:
		return AlarmTypeVideoLost, AlarmTypeDeviceTamper, true
	default:
		return 0, 0, false
	}
}

func validateDragZoomRegion(region DragZoomRegion) error {
	if region.Length <= 0 || region.Width <= 0 {
		return fmt.Errorf("3D 定位播放窗口尺寸必须为正数")
	}
	if region.LengthX <= 0 || region.LengthY <= 0 {
		return fmt.Errorf("3D 定位矩形尺寸必须为正数")
	}
	if region.MidPointX < 0 || region.MidPointX > region.Length || region.MidPointY < 0 || region.MidPointY > region.Width {
		return fmt.Errorf("3D 定位矩形中心超出播放窗口")
	}
	left := region.LengthX / 2
	right := region.LengthX - left
	top := region.LengthY / 2
	bottom := region.LengthY - top
	if region.LengthX > region.Length || region.LengthY > region.Width ||
		left > region.MidPointX || right > region.Length-region.MidPointX ||
		top > region.MidPointY || bottom > region.Width-region.MidPointY {
		return fmt.Errorf("3D 定位矩形超出播放窗口")
	}
	return nil
}

type CapabilityState string

const (
	CapabilityUnknown     CapabilityState = "unknown"
	CapabilityUnsupported CapabilityState = "unsupported"
	CapabilitySupported   CapabilityState = "supported"
)

type ControlCapability struct {
	State  CapabilityState `json:"state"`
	Reason string          `json:"reason"`
}

type ControlCapabilities struct {
	BasicPTZ   ControlCapability `json:"basicPtz"`
	IFrame     ControlCapability `json:"iFrame"`
	Record     ControlCapability `json:"record"`
	Guard      ControlCapability `json:"guard"`
	AlarmReset ControlCapability `json:"alarmReset"`
	TeleBoot   ControlCapability `json:"teleBoot"`
	DragZoom   ControlCapability `json:"dragZoom"`
	// TargetTrack 对应 GB/T 28181-2022 A.2.3.1.14。它需要"全景相机球机"这种双目结构，
	// 单目通道上报支持也没意义 —— 所以**默认 unknown**，只认设备自己的显式声明
	// （同 DragZoom：不因"PTZType 看起来像"就升格为 supported）。
	TargetTrack ControlCapability `json:"targetTrack"`
}

// ParseControlCapabilities converts explicitly reported booleans into a
// three-state DTO. PTZType is authoritative only for basic PTZ; it never
// promotes an unreported advanced control capability.
func ParseControlCapabilities(raw *string, ptzType int8) ControlCapabilities {
	result := ControlCapabilities{BasicPTZ: basicPTZCapability(ptzType)}
	var reported map[string]json.RawMessage
	missingReason := "设备未上报该能力"
	if raw == nil || strings.TrimSpace(*raw) == "" {
		reported = nil
	} else if err := json.Unmarshal([]byte(*raw), &reported); err != nil {
		reported = nil
		missingReason = "Capabilities JSON 无效"
	}
	result.IFrame = parseReportedCapability(reported, missingReason, "iframe", "iFrame", "i_frame")
	result.Record = parseReportedCapability(reported, missingReason, "recording", "record")
	result.Guard = parseReportedCapability(reported, missingReason, "guard")
	result.AlarmReset = parseReportedCapability(reported, missingReason, "alarm_reset", "alarmReset")
	result.TeleBoot = parseReportedCapability(reported, missingReason, "teleboot", "teleBoot", "tele_boot")
	result.DragZoom = parseReportedCapability(reported, missingReason, "drag_zoom", "dragZoom")
	result.TargetTrack = parseReportedCapability(reported, missingReason, "target_track", "targetTrack")
	return result
}

func basicPTZCapability(ptzType int8) ControlCapability {
	switch ptzType {
	case 1, 2, 4, 5:
		// 1球机 / 2半球 / 4遥控枪机 / 5遥控半球 —— 都是"可遥控"的云台结构。
		// ⛔ 5 是 GB/T 28181-2022 新增(值域由 1-4 扩到 1-7):漏掉它会让 2022 设备上报的
		// 遥控半球被判成 CapabilityUnknown,前端不显示云台控件。
		// ⛔ 6(多目设备的全景/拼接通道)、7(多目设备的分割通道)**刻意不列入**:
		// 标准未声明这两种结构必带云台,不能因"值域内"就臆断为支持 —— 保持
		// CapabilityUnknown,由设备的能力 JSON 上报来定。
		return ControlCapability{State: CapabilitySupported, Reason: "PTZType 明确为可控云台"}
	case 3:
		return ControlCapability{State: CapabilityUnsupported, Reason: "PTZType 明确为固定枪机"}
	default:
		return ControlCapability{State: CapabilityUnknown, Reason: "PTZType 未上报或无法识别"}
	}
}

func parseReportedCapability(reported map[string]json.RawMessage, missingReason string, keys ...string) ControlCapability {
	var value json.RawMessage
	for _, key := range keys {
		if candidate, ok := reported[key]; ok {
			value = candidate
			break
		}
	}
	if value == nil || string(value) == "null" {
		return ControlCapability{State: CapabilityUnknown, Reason: missingReason}
	}
	var supported bool
	if err := json.Unmarshal(value, &supported); err != nil {
		return ControlCapability{State: CapabilityUnknown, Reason: "设备上报的能力值不是布尔值"}
	}
	if supported {
		return ControlCapability{State: CapabilitySupported, Reason: "设备明确上报支持"}
	}
	return ControlCapability{State: CapabilityUnsupported, Reason: "设备明确上报不支持"}
}

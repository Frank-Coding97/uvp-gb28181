package manscdp

import (
	"encoding/xml"
	"fmt"
	"strings"
)

// `OSDConfig`（A.2.1.12）的取值域与上限。
const (
	maxOSDTextItems   = 8  // 标准 `Item maxOccurs="8"`
	maxOSDTextLength  = 32 // 标准 `Text` 长度 0~32
	osdSwitchDefault  = 1  // XSD 给 `TimeEnable` / `TextEnable` 的 default="1"
	osdTimeTypeDate   = 0  // `TimeType`：YYYY-MM-DD HH:MM:SS
	osdTimeTypeCNDate = 1  // `TimeType`：YYYY年MM月DD日HH:MM:SS
)

// OSDTextItem 是前端 OSD 里的一条自由文本（`OSDCfgType/Item`）。
//
//   - `Text` 文字内容，长度 0~32（必选）
//   - `X`    文字 X 像素坐标（必选），原点 = 播放窗口左上角，水平向右为正
//   - `Y`    文字 Y 像素坐标（必选），竖直向下为正
type OSDTextItem struct {
	Text string `json:"text"`
	X    int    `json:"x"`
	Y    int    `json:"y"`
}

// OSDConfigBlock 是 `OSDCfgType`（A.2.1.12）—— **前端 OSD**。
//
// ⛔⛔ 这是「**设备烧进视频流的叠加**」，不是播放器预览里的界面叠层。
// 两者的坐标模型不同：标准用**绝对像素**（左上角原点），而本仓的预览叠层用**锚点**。
// 平台侧只负责这一份协议面的值，不做锚点换算 —— 换算会静默丢信息（8 条自由文本
// 各自的 X/Y 无法反查到 5 个锚点），且回读对账拿到的就不再是平台下发的值。
//
// ⭐ `TimeEnable` / `TextEnable` 是**确定的 0/1**，不留 nil：XSD 的 `default="1"`
// 语义是「元素缺席时取 1」，而设备侧"开关到底是开还是关"总有一个确定值。
// ⛔ 但"确定"的**唯一来源是实到值**（缺席才退回默认）—— 把实到的 0 也算成默认，
// 就不是"确定"而是"篡改"了，见 [osdConfigWire] 里那段真机实证。
// 反过来 `TimeType` 保留指针：标准没给它 default，**"设备不指定格式"是一个真实状态**，
// 构建时整个元素不出现（不是补 0 —— 0 表示「YYYY-MM-DD HH:MM:SS」这个**特定**格式）。
//
// ⭐ `SumNum` **不独立存**，恒等于 `len(Items)`（同 PictureMaskBlock 的口径）。
type OSDConfigBlock struct {
	// Length 配置窗口长度像素值（本仓与设备侧同口径 = 视频**水平**像素数）。
	Length int `json:"length"`
	// Width 配置窗口宽度像素值（= 视频**垂直**像素数）。
	Width int `json:"width"`
	TimeX int `json:"timeX"`
	TimeY int `json:"timeY"`
	// TimeEnable 显示时间开关，0/1。
	TimeEnable int `json:"timeEnable"`
	// TimeType 时间显示类型，0/1；nil = 设备不指定格式（元素缺席）。
	// ⛔ 唯一允许 omitempty 的取值字段：这里的 nil 有独立语义（"设备不定格式"），
	// 而不是"等于 0"。其余取值字段一律如实输出，0 就是 0。
	TimeType *int `json:"timeType,omitempty"`
	// TextEnable 显示文字开关，0/1。
	TextEnable int           `json:"textEnable"`
	Items      []OSDTextItem `json:"items"`
}

// SumNum 是线格式用的行数计数，恒等于实到条数（不独立存，同 PictureMask 的口径）。
func (b *OSDConfigBlock) SumNum() int {
	if b == nil {
		return 0
	}
	return len(b.Items)
}

type osdConfigWire struct {
	XMLName xml.Name `xml:"OSDConfig"`
	Length  int      `xml:"Length"`
	Width   int      `xml:"Width"`
	TimeX   int      `xml:"TimeX"`
	TimeY   int      `xml:"TimeY"`
	// ⛔⛔ 两个开关**必须是指针**，不能用 int。
	//
	// XSD 的 `default="1"` 语义是「**元素缺席**时取 1」，不是「值为 0 时取 1」。
	// 用 int 接就再也分不清这两种情况，而 [osdConfigWire.toBlock] 会把**两者**都
	// 改写成 1 —— 于是「设备明确报关」被静默篡改成「开」。
	//
	// 真机实证（海康 DS-2DC2C040MY-DE / GB28181-2022，2026-09-19）：
	//
	//	设备报文  <TimeEnable>1</TimeEnable><TimeType>1</TimeType><TextEnable>0</TextEnable>
	//	平台落库  "timeEnable":1,"timeType":1,"textEnable":1   ← TextEnable 被改成 1
	//
	// 危害不止展示：回读对账拿它跟下发值比，会把「设备真的没照做」判成一致（假绿），
	// 也可能反过来把一致判成差异。
	//
	// ⭐ `omitempty` 只防御 nil（正常情况下 [osdConfigWire.wire] 保证非 nil）：
	// `encoding/xml` 对 nil 指针**不加 omitempty 也会输出空元素** `<TimeEnable></TimeEnable>`，
	// 那个形态对端解不出来。注意 omitempty 对指针只判 nil、**不判指向的 0**，
	// 所以 `TextEnable=0` 照常序列化成 `<TextEnable>0</TextEnable>`，不会被省略。
	TimeEnable *int `xml:"TimeEnable,omitempty"`
	TimeType   *int `xml:"TimeType,omitempty"`
	TextEnable *int `xml:"TextEnable,omitempty"`
	// SumNum 是 `Item` 的行数计数（必选），取实到条数。
	SumNum int               `xml:"SumNum"`
	Items  []osdTextItemWire `xml:"Item"`
}

// osdSwitchValue 解出一个 OSD 开关的确定值：
// **元素在场就取实到值**（0 就是 0），**缺席才**按 XSD 的 `default="1"` 收。
//
// ⛔ 别把它简化成 `if value == 0 { return 1 }` —— 那正是本次要修的缺陷。
// 缺席取 1 而不是 0 的理由见 [OSDConfigBlock] 的注释（用 0 兜底会把"设备没报这一项"
// 显示成"设备把开关关了"）。
func osdSwitchValue(value *int) int {
	if value == nil {
		return osdSwitchDefault
	}
	return *value
}

type osdTextItemWire struct {
	Text string `xml:"Text"`
	X    int    `xml:"X"`
	Y    int    `xml:"Y"`
}

func (b *OSDConfigBlock) wire() *osdConfigWire {
	if b == nil {
		return nil
	}
	// 两个开关取局部副本再取地址：block 侧是确定的 0/1（值类型），wire 侧要指针才能
	// 表达"缺席"。**下发时一律带上**（虽然 XSD 有 default）—— 让报文自解释，
	// 免得对端/抓包的人还得回 XSD 查"缺席时算开还是算关"。
	timeEnable, textEnable := b.TimeEnable, b.TextEnable
	wire := &osdConfigWire{
		Length: b.Length, Width: b.Width, TimeX: b.TimeX, TimeY: b.TimeY,
		TimeEnable: &timeEnable, TimeType: b.TimeType, TextEnable: &textEnable,
		SumNum: len(b.Items),
	}
	for _, item := range b.Items {
		wire.Items = append(wire.Items, osdTextItemWire{Text: item.Text, X: item.X, Y: item.Y})
	}
	return wire
}

func (w *osdConfigWire) toBlock() *OSDConfigBlock {
	if w == nil {
		return nil
	}
	block := &OSDConfigBlock{
		Length: w.Length, Width: w.Width, TimeX: w.TimeX, TimeY: w.TimeY,
		// ⭐ 实到值优先，缺席才用 XSD 默认 —— 见 [osdSwitchValue]。
		// ⛔ 曾经这里写死 `osdSwitchDefault`，把「设备报关」一并改成「开」。
		TimeEnable: osdSwitchValue(w.TimeEnable),
		TimeType:   w.TimeType,
		TextEnable: osdSwitchValue(w.TextEnable),
	}
	block.Items = make([]OSDTextItem, 0, len(w.Items))
	for _, item := range w.Items {
		block.Items = append(block.Items, OSDTextItem{
			Text: item.Text, X: item.X, Y: item.Y,
		})
	}
	return block
}

// OSD 开关是否合法（0/1）。
func validOSDSwitch(value int) bool {
	return value == AlarmReportOff || value == AlarmReportOn
}

func (b *OSDConfigBlock) validate() error {
	if b == nil {
		return nil
	}
	if b.Length <= 0 || b.Width <= 0 {
		return fmt.Errorf("OSDConfig.Length/Width 必须为正（Length=%d Width=%d）", b.Length, b.Width)
	}
	if b.TimeX < 0 || b.TimeY < 0 {
		return fmt.Errorf("OSDConfig.TimeX/TimeY 不得为负（TimeX=%d TimeY=%d）", b.TimeX, b.TimeY)
	}
	if !validOSDSwitch(b.TimeEnable) {
		return fmt.Errorf("OSDConfig.TimeEnable 非法: %d（只能 0/1）", b.TimeEnable)
	}
	if !validOSDSwitch(b.TextEnable) {
		return fmt.Errorf("OSDConfig.TextEnable 非法: %d（只能 0/1）", b.TextEnable)
	}
	if b.TimeType != nil && *b.TimeType != osdTimeTypeDate && *b.TimeType != osdTimeTypeCNDate {
		return fmt.Errorf("OSDConfig.TimeType 非法: %d（只能 %d/%d，或不发该元素）",
			*b.TimeType, osdTimeTypeDate, osdTimeTypeCNDate)
	}
	if len(b.Items) > maxOSDTextItems {
		return fmt.Errorf("OSDConfig 文本行数 %d 超过标准上限 %d", len(b.Items), maxOSDTextItems)
	}
	for index, item := range b.Items {
		prefix := fmt.Sprintf("OSDConfig 第 %d 条文本", index+1)
		if len([]rune(item.Text)) > maxOSDTextLength {
			// ⛔ 按**字符**数而不是字节数判：标准写的是"长度0~32"，
			// 中文一个字占 3 字节，按字节判会让 11 个汉字就被拒。
			return fmt.Errorf("%s: Text 长度超过标准上限 %d（按字符计）", prefix, maxOSDTextLength)
		}
		if item.X < 0 || item.Y < 0 {
			return fmt.Errorf("%s: 坐标不得为负（X=%d Y=%d）", prefix, item.X, item.Y)
		}
	}
	return nil
}

// OSDPositionOptions 是 `TimeType` 的人读串（**只给日志/前端提示用，绝不进报文**）。
func osdTimeTypeLabel(timeType *int) string {
	if timeType == nil {
		return "不指定"
	}
	switch strings.TrimSpace(fmt.Sprint(*timeType)) {
	case "0":
		return "YYYY-MM-DD HH:MM:SS"
	case "1":
		return "YYYY年MM月DD日HH:MM:SS"
	default:
		return fmt.Sprintf("非法值 %d", *timeType)
	}
}

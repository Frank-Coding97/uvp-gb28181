package manscdp

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// 图像文件标识（`SnapShotFileID` / 上传文件名）的分段。
//
// 规则出处：GB/T 28181-2022 §9.14.1 与表 4 ——「图像文件命名」：
//
//	设备编码 20 位 + 图像编码 2 位 + 时间 17 位 + 序列码 2 位 = 41 位
//
// ⛔⛔ 但**真机不补零**，总长可能只有 40 位（2026-09-20 海康实测，三张连拍文件名）：
//
//	37010301021320000002 02 20260920133058161 01   ← 41 位（序列码 01）
//	37010301021320000002 02 20260920133101400 2    ← 40 位（序列码 2）
//	37010301021320000002 02 20260920133104400 3    ← 40 位（序列码 3）
//
// ⇒ 序列码是**不定长（不补零）**的，标准表的"2 位"是**上限**而不是定长。
// 这直接决定了本解析器的取段策略：**从前往后切，时间字段用固定偏移**，
// 剩下的全归序列码 —— 而不是"从后往前切 2 位当序列码"（那样第二、三张会把
// 时间字段的最后一位吃掉，得到 13:31:01.400 变 13:31:01.40 之外的错误时刻）。
//
// ⛔ 也别用 2 位序列码**校验**真机报文：40 位的文件名是合规的，不是"设备不守规矩"。
// 若真的按 41 位硬校验，海康三张里有两张会被判非合规 ⇒ 时间轴回落到接收时刻。
const (
	snapshotDeviceCodeLength = 20
	snapshotImageCodeLength  = 2
	snapshotTimeLength       = 17
	// snapshotMaxSequenceLength 是序列码的**上限**（标准表 4 的"2 位"）；
	// 真机可能只给 1 位，所以它是上限而不是定长。
	snapshotMaxSequenceLength = 2
	// snapshotStandardLength 是标准定义的 41 位 —— **合规性判定**用它，
	// 但**解析**不要求它（见上面 40 位真机样例）。
	snapshotStandardLength = snapshotDeviceCodeLength + snapshotImageCodeLength +
		snapshotTimeLength + snapshotMaxSequenceLength
)

// SnapshotFileID 是一个解析成功的图像文件标识。
type SnapshotFileID struct {
	// Raw 是原始输入（已去掉图片后缀、去空白），便于日志与对账时原样引用。
	Raw string
	// DeviceCode 是前 20 位设备编码 —— ⛔ 这是**设备编码**，不是通道编码；
	// 通道抓拍时设备用它自己的编码命名文件，所以它与 `SnapShotConfig` 里下发的
	// `SessionID` 属于两个维度，不能互相校验。
	DeviceCode string
	// ImageCode 是 2 位图像编码（真机是 `02`）。
	ImageCode string
	// CapturedAt 是**拍摄时刻**，由 17 位时间字段反解（本地时区）。
	CapturedAt time.Time
	// Sequence 是序列码，长度 1~2 位（真机不补零），可能为空（总长恰为 39 位时）。
	Sequence string
}

// StandardCompliant 表示这条标识是否**严格符合表 4 的 41 位**（含 2 位序列码）。
//
// ⭐ 单独给出这个判定而不是让 [ParseSnapshotFileID] 直接拒绝 40 位：两者是**两件事** ——
// "能不能解析出拍摄时刻"与"设备有没有严格照表 4 命名"。真机是 40 位，平台照样要能定位图与排序；
// 但"设备命名不合规"这件事本身值得告警（backlog E-4 的要求），所以判定要能被调用方拿到。
func (snapshot SnapshotFileID) StandardCompliant() bool {
	return len(snapshot.Raw) == snapshotStandardLength
}

// ParseSnapshotFileID 解析图像文件标识（设备文件名去掉 `.jpg` 后的串）并反解拍摄时刻。
//
// ⏩ 调用方约定：**解析失败不是错误路径**，是"设备没按标准命名"。所以它返回 error 但
// 调用方应把失败降级成"captured_at 回落到接收时刻 + 记下原文件名"，**不要**因此拒绝收图
// —— 图片内容已经收到了，因为文件名不合规就把图丢掉是本末倒置。
//
// 长度约束是 39~41 位：**下界 39 = 必须凑齐 设备码20 + 图像码2 + 时间17**（少一位时间就
// 反解不出时刻）；上界 41 = 标准全长（多出来的部分归序列码，超出即非合规命名，宁可报错
// 也不猜）。中间的 40 位是真机形态，正常接受。
func ParseSnapshotFileID(raw string) (SnapshotFileID, error) {
	identifier := strings.TrimSpace(raw)
	// 容忍带图片后缀的形态：上传的文件名（`…16801.jpg`）与完成通知里的标识（`…16801`）
	// 是同一样东西的两种出现形式，**两者必须解析出同一个 CapturedAt**，否则"按时间排序"
	// 会因来源不同而不一致。
	if index := strings.LastIndex(identifier, "."); index > 0 {
		identifier = identifier[:index]
	}
	result := SnapshotFileID{Raw: identifier}
	if len(identifier) < snapshotDeviceCodeLength+snapshotImageCodeLength+snapshotTimeLength || len(identifier) > snapshotStandardLength {
		return result, fmt.Errorf("图像文件标识长度应在 %d~%d 位之间, 实际 %d 位: %q",
			snapshotDeviceCodeLength+snapshotImageCodeLength+snapshotTimeLength, snapshotStandardLength,
			len(identifier), identifier)
	}
	// 先按"全是数字"筛一遍：长度对但含字母时，后面 time.Parse 会给出一个难以定位的错误
	// （报错位置指向时间字段，而真正的原因可能在设备编码段里混了字符）。
	for index := 0; index < len(identifier); index++ {
		if identifier[index] < '0' || identifier[index] > '9' {
			return result, fmt.Errorf("图像文件标识第 %d 位不是数字: %q", index+1, identifier)
		}
	}
	deviceEnd := snapshotDeviceCodeLength
	imageEnd := deviceEnd + snapshotImageCodeLength
	timeEnd := imageEnd + snapshotTimeLength
	result.DeviceCode = identifier[:deviceEnd]
	result.ImageCode = identifier[deviceEnd:imageEnd]
	result.Sequence = identifier[timeEnd:]

	stamp := identifier[imageEnd:timeEnd]
	// 时间字段按 `yyyyMMddHHmmss`（14 位）+ 3 位毫秒读。
	// ⛔ 秒段必须由**切片固定为 2 位**：若把 17 位整个丢给时间解析器，它会按自己的规则
	// 切分，`…143310` 这种输入可能被读成"秒 = 10"而不是"毫秒 = 310"，
	// 静默给出偏移几百毫秒的时刻 —— 这种错在库里看不出来（时间戳合法），只能在实拍比对时发现。
	seconds, err := time.ParseInLocation("20060102150405", stamp[:14], time.Local)
	if err != nil {
		return result, fmt.Errorf("图像文件标识的时间字段不合法: %q", stamp)
	}
	millis, err := strconv.Atoi(stamp[14:])
	if err != nil || millis < 0 || millis > 999 {
		return result, fmt.Errorf("图像文件标识的毫秒字段不合法: %q", stamp[14:])
	}
	result.CapturedAt = seconds.Add(time.Duration(millis) * time.Millisecond)
	return result, nil
}

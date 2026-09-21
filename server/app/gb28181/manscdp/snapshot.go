package manscdp

import (
	"encoding/xml"
	"fmt"
	"strings"

	"uvplatform.cn/uvp-gb28181/app/gb28181/protocol"
)

const (
	CmdNotify      = "Notify"
	SubCmdSnapshot = "SnapShot"
	// CmdUploadSnapShotFinished 是 A.2.5.7「图像抓拍传输完成通知」的 CmdType 取值
	// （标准里是 `fixed="UploadSnapShotFinished"`）。
	CmdUploadSnapShotFinished = "UploadSnapShotFinished"
)

// ⛔ 抓拍配置（`SnapShotConfig`）的**下发构建**不在这里，也不需要在这里：
// 它是配置家族的一个普通配置块（A.2.3.2.12），由
// [BuildDeviceConfigBlocksWithProfile] 统一产出，块结构见 [SnapShotBlock]。
//
// 2026-09-20 真机 A/B 取证之前，这里曾有一份独立的 `snapshotControl` 构建器，
// 且把 `CmdType` 写成了 `DeviceControl`（A.2.3.1 的设备控制）—— 而 A.2.3.2.1 明文
// `fixed="DeviceConfig"`。海康 `37010301021320000002` 实测：`DeviceControl` 形态
// 设备**连业务应答都不回**、回读 `SnapNum` 仍是 0（配置根本没进去）；
// 换成 `DeviceConfig` 才回 `<Result>OK</Result>`、按张数抓拍并上报，回读逐字段一致。
// ⇒ 同一件事有两份构建实现时，"哪一份是对的"没有任何测试能回答；合并成一份。

// ==================== A.2.5.7 抓拍传输完成通知（标准形态，一对多） ====================

// UploadSnapShotFinished 是 A.2.5.7「图像抓拍传输完成通知」—— 设备抓拍并上传完图片后
// **主动**上报的结果。
//
// 真机原文（海康 `37010301021320000002`，2026-09-20 13:31:04，`SN=9402` 那次 `DeviceConfig`
// 下发的 3 张抓拍）：
//
//	<Notify><CmdType>UploadSnapShotFinished</CmdType><SN>9402</SN>
//	<DeviceID>37010301021320000002</DeviceID><SessionID>probe-b-…</SessionID>
//	<SnapShotList><SnapShotFileID>37010301021320000002022026092013305816101</SnapShotFileID></SnapShotList>
//	<SnapShotList><SnapShotFileID>3701030102132000000202202609201331014002</SnapShotFileID></SnapShotList>
//	<SnapShotList><SnapShotFileID>3701030102132000000202202609201331044003</SnapShotFileID></SnapShotList>
//	</Notify>
type UploadSnapShotFinished struct {
	CmdType   string
	SN        string
	DeviceID  string
	SessionID string
	// FileIDs 是本次抓拍完成的图像文件标识（A.2.5.7 限 ≤10 个）。已去重、保持报文顺序。
	// 真机样例：`37010301021320000002022026092013305816101` = 设备编码 20 位 + 图像编码 2 位
	// + 时间 17 位 + 序列码 2 位，共 41 位（E-4 那条规则的真机锚点；本函数不校验它）。
	FileIDs []string
}

// uploadSnapShotFinishedWire 只服务于解析，不对外，**刻意不带 `XMLName`**。
//
// ⛔⛔ 两个必须照抄真机的地方，少一个就静默解析出 0 个文件标识（"有应答无数据"，两侧都不报错）：
//  1. 根元素是 **`<Notify>`**，不是 `<Response>` —— 不写 `XMLName` 才不会把根标签写死，
//     与 [ParseHead] 同款做法。
//  2. `<SnapShotList>` 在真机上是**重复的单元素**（一张图一个 `<SnapShotList>`），
//     而标准文字写的是"一个 `<SnapShotList>` 里放 N 个 `<SnapShotFileID>`"。
//     所以外层收切片、内层再收一层切片 —— 两种写法都能吃下。
type uploadSnapShotFinishedWire struct {
	CmdType   string             `xml:"CmdType"`
	SN        string             `xml:"SN"`
	DeviceID  string             `xml:"DeviceID"`
	SessionID string             `xml:"SessionID"`
	Lists     []snapShotListWire `xml:"SnapShotList"`
}

type snapShotListWire struct {
	FileIDs []string `xml:"SnapShotFileID"`
}

// ParseUploadSnapShotFinished 解析 A.2.5.7 完成通知。
//
// ⭐ 这是**入站**解析，校验取舍与同族的 [SnapShotBlock.validate] **刻意相反**：那边是平台
// 可控的下发，越界一律拒；这边是设备说什么就得收什么 —— 收得越严，越容易把"抓拍完成"这个
// 信号本身弄丢，而丢掉的后果是 `devicecapture` 的会话**永远到不了 `completed`**（前端会一直
// 显示未完成）。所以：
//   - 标准写 ≤10 个标识，真机可能重复元素或超量 —— **照收，不按标准拒绝**；
//   - 只对"CmdType 不对""没有 `SessionID`""一个文件标识都没有"判非法：这三种无法关联到会话、
//     也无法推进完成计数，属于"解析出来也没用"。
func ParseUploadSnapShotFinished(body []byte) (UploadSnapShotFinished, error) {
	var wire uploadSnapShotFinishedWire
	result := UploadSnapShotFinished{}
	if err := DecodeProfiledXML(protocol.ProfileFor(protocol.Version2022), body, &wire); err != nil {
		return result, err
	}
	result.CmdType = strings.TrimSpace(wire.CmdType)
	result.SN = strings.TrimSpace(wire.SN)
	result.DeviceID = strings.TrimSpace(wire.DeviceID)
	result.SessionID = strings.TrimSpace(wire.SessionID)
	if result.CmdType != CmdUploadSnapShotFinished {
		return result, fmt.Errorf("不是图像抓拍传输完成通知: CmdType=%q", result.CmdType)
	}
	if result.SessionID == "" {
		return result, fmt.Errorf("图像抓拍传输完成通知缺 SessionID")
	}

	seen := make(map[string]struct{}, len(wire.Lists))
	for _, list := range wire.Lists {
		for _, raw := range list.FileIDs {
			id := strings.TrimSpace(raw)
			if id == "" {
				continue
			}
			if _, duplicate := seen[id]; duplicate {
				continue
			}
			seen[id] = struct{}{}
			result.FileIDs = append(result.FileIDs, id)
		}
	}
	if len(result.FileIDs) == 0 {
		return result, fmt.Errorf("图像抓拍传输完成通知没有任何 SnapShotFileID")
	}
	return result, nil
}

// ==================== 私有形态完成通知（本仓自造口径，兼容保留） ====================

type SnapshotNotify struct {
	XMLName     xml.Name `xml:"Notify"`
	CmdType     string   `xml:"CmdType"`
	SubCmd      string   `xml:"SubCmd"`
	SN          string   `xml:"SN"`
	DeviceID    string   `xml:"DeviceID"`
	SessionID   string   `xml:"SessionID"`
	SnapshotID  string   `xml:"SnapShotID"`
	Time        string   `xml:"Time"`
	StoragePath string   `xml:"StoragePath"`
}

// ParseSnapshotNotify 解析**私有形态**的抓拍 Notify："一图一条"。
//
// ⛔ 这是本仓自造、**不是标准**：标准 A.2.5.7 的形态见 [ParseUploadSnapShotFinished]。
// `SubCmd` / `SnapShotID` 这两个名字在 GB/T 28181-2022 全文**零命中**（2026-09-20 复验）。
//
// ⭐ 既然不是标准，为什么还留着：**模拟器**（`uvp-gb28181-sim` 的 `SnapShotNotifyBuilder.kt`）
// 当前发的就是这个形状，它是本仓唯一的端到端联调对象。平台侧两条路都收 ⇒ 真机与模拟器
// 各自都能推进会话完成计数。等 E-2 的模拟器侧也改成标准形态后，这一路可以删。
// ⚠️ 别把它当成"另一种合法写法"去对外宣称 —— 对外只有 A.2.5.7 一种。
func ParseSnapshotNotify(body []byte) (SnapshotNotify, error) {
	var notify SnapshotNotify
	if err := DecodeProfiledXML(protocol.ProfileFor(protocol.Version2022), body, &notify); err != nil {
		return notify, err
	}
	notify.CmdType = strings.TrimSpace(notify.CmdType)
	notify.SubCmd = strings.TrimSpace(notify.SubCmd)
	notify.DeviceID = strings.TrimSpace(notify.DeviceID)
	notify.SessionID = strings.TrimSpace(notify.SessionID)
	notify.SnapshotID = strings.TrimSpace(notify.SnapshotID)
	if notify.CmdType != CmdNotify || notify.SubCmd != SubCmdSnapshot || notify.SessionID == "" || notify.SnapshotID == "" {
		return notify, fmt.Errorf("SnapShot Notify 内容不合法")
	}
	return notify, nil
}

func IsSnapshotNotify(body []byte) bool {
	notify, err := ParseSnapshotNotify(body)
	return err == nil && notify.CmdType == CmdNotify && notify.SubCmd == SubCmdSnapshot
}

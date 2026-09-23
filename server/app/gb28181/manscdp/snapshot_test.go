package manscdp

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"uvplatform.cn/uvp-gb28181/app/gb28181/protocol"
)

// 本文件的锚点分两类，缺一不可：
//   - **真机原文**（海康 `37010301021320000002`，2026-09-20）—— 钉住"设备真的会发什么"；
//   - **平台构建结果** —— 钉住"平台真的会发什么"。
// 两边都钉住，才能证明"我们把设备说的听懂了、设备也把我们要的听懂了"。

// hikSnapShotConfigResponse 是真机对 `ConfigType=SnapShotConfig` 查询的应答原文（逐字节照抄）。
//
// ⛔ 两个形态细节都是真实设备给出来的，别"顺手规范一下"：
//   - 元素名是 **`SnapShot`**（不是 `SnapShotConfig`）—— 对应 A.2.6.9；
//   - `SnapNum` = 0，**越出** A.2.1.24 的 1~10 取值域 —— 设备没配抓拍时就这么回，
//     解析侧必须"宽松收"（收下原值让对账去显示），不能报错把整条应答丢掉。
const hikSnapShotConfigResponse = `<?xml version="1.0" encoding="GB18030"?><Response><CmdType>ConfigDownload</CmdType><SN>9201</SN><DeviceID>37010301021320000002</DeviceID><Result>OK</Result><SnapShot><SnapNum>0</SnapNum><Interval>0</Interval><UploadURL></UploadURL><SessionID></SessionID></SnapShot></Response>`

// snapShotFixture 是一份恰好合法的抓拍配置（写入方向）。
func snapShotFixture() DeviceConfigBlocks {
	snapNum, interval := 2, 3
	uploadURL := "http://127.0.0.1/api/gb28181/device-snapshots/uploads/token/"
	sessionID := strings.Repeat("s", MinSnapShotSessionIDLen)
	return DeviceConfigBlocks{SnapShot: &SnapShotBlock{
		SnapNum: &snapNum, Interval: &interval, UploadURL: &uploadURL, SessionID: &sessionID,
	}}
}

// TestBuildSnapShotConfigUsesDeviceConfigCmdType 钉住抓拍配置的**命令类型与元素名**。
//
// ⛔ 这条用例来自 2026-09-20 的海康真机 A/B 取证，是本轮最贵的一条结论：
// 同一份 `<SnapShotConfig>`，`CmdType=DeviceControl` 发出去设备**连业务应答都不回**、
// 回读 `SnapNum` 仍是 0（配置压根没进去）；`CmdType=DeviceConfig` 才回
// `<Result>OK</Result>`、按张数抓拍并上报，回读逐字段一致。
// A.2.3.2.1 明文 `fixed="DeviceConfig"` —— 旧实现违反的是**标准**，
// 而它在平台侧的表现是"下发成功了（status=sent）、只是设备没反应"，极难归因。
func TestBuildSnapShotConfigUsesDeviceConfigCmdType(t *testing.T) {
	body, err := BuildDeviceConfigBlocksWithProfile(
		protocol.ProfileFor(protocol.Version2022), testDeviceID, 7, snapShotFixture())
	require.NoError(t, err)

	wire := string(body)
	require.Contains(t, wire, `encoding="GB18030"`)
	require.Contains(t, wire, "<CmdType>DeviceConfig</CmdType>")
	require.NotContains(t, wire, "<CmdType>DeviceControl</CmdType>")

	// ⛔ 下发侧元素名是 `SnapShotConfig`，**不能**是应答侧那个 `SnapShot`。
	// 两个方向共用一个 [snapShotWire]，元素名只由父结构的字段 tag 决定 ——
	// 所以这里同时断言"有 SnapShotConfig"且"没有裸的 SnapShot 元素"。
	require.Contains(t, wire, "<SnapShotConfig>")
	require.NotContains(t, wire, "<SnapShot>",
		"下发元素名不能退化成应答侧的 SnapShot（多半是给 snapShotWire 加了 XMLName）")
	require.Contains(t, wire, "<SnapNum>2</SnapNum>")
	require.Contains(t, wire, "<Interval>3</Interval>")
}

// TestParseSnapShotReadResponse_FollowsHikvisionWire 用真机原文钉住读取解析。
func TestParseSnapShotReadResponse_FollowsHikvisionWire(t *testing.T) {
	result, err := ParseDeviceConfigReadResponse([]byte(hikSnapShotConfigResponse))
	require.NoError(t, err)
	require.True(t, result.Blocks.Has(ConfigTypeSnapShotConfig),
		"`<SnapShot>` 没被认出来 —— 读取侧又把抓拍配置整块丢了")

	block, present := result.Blocks.Block(ConfigTypeSnapShotConfig)
	require.True(t, present)
	snap, ok := block.(*SnapShotBlock)
	require.True(t, ok, "Block 取到的不是 *SnapShotBlock: %T", block)

	// 宽松收：越界的 0 也要原样带上来，交给对账显示成"值不一致"。
	require.NotNil(t, snap.SnapNum)
	require.Equal(t, 0, *snap.SnapNum)
	require.NotNil(t, snap.Interval)
	require.Equal(t, 0, *snap.Interval)
	// 空元素（`<UploadURL></UploadURL>`）归一成缺席：协议层"在场但空"与"没给"都指向"没有配置"。
	require.Nil(t, snap.UploadURL)
	require.Nil(t, snap.SessionID)
}

// TestSnapShotBlockJSONKeyFollowsResponseElement 钉住 json 键名。
//
// ⛔ 本族**只有这一个类型**两端名字不同：标准 ConfigType 名是 `SnapShotConfig`（查询/下发），
// 库里的 `payload_json` 与 API 却按**应答元素名** `SnapShot` 走小驼峰。
// 键名抄成 `snapShotConfig` 的后果是前端词汇表对不上，界面表现为"回读成功但表单全空"。
func TestSnapShotBlockJSONKeyFollowsResponseElement(t *testing.T) {
	require.Equal(t, "SnapShotConfig", ConfigTypeSnapShotConfig, "标准 ConfigType 名（A.2.4.7 明文）")

	snapNum := 3
	payload, err := json.Marshal(DeviceConfigBlocks{SnapShot: &SnapShotBlock{SnapNum: &snapNum}})
	require.NoError(t, err)
	require.JSONEq(t, `{"snapShot":{"snapNum":3}}`, string(payload))

	// 反向：容器里没有这一块时，键整个不出现（omitempty），而不是给个 null。
	payload, err = json.Marshal(DeviceConfigBlocks{})
	require.NoError(t, err)
	require.NotContains(t, string(payload), "snapShot")
}

// TestSnapShotValidateRejectsMissingCredentials 钉住三项必选。
//
// ⛔ 三项（`SnapNum` / `UploadURL` / `SessionID`）正是设备"要不要抓、往哪传、算哪一次"
// 的全部依据。缺一项设备只能自己编默认值，而平台这边**不会有任何反馈** ——
// 写入应答 A.2.6.8 只有 `Result`，没有回显，所谓"下发失败"在界面上就是"设备没照做"。
func TestSnapShotValidateRejectsMissingCredentials(t *testing.T) {
	sessionID := strings.Repeat("s", MinSnapShotSessionIDLen)
	snapNum := 1
	uploadURL := "http://127.0.0.1/upload/"

	cases := []struct {
		name    string
		block   SnapShotBlock
		wantMsg string
	}{
		{"缺 SnapNum", SnapShotBlock{UploadURL: &uploadURL, SessionID: &sessionID}, "SnapNum 必选"},
		{"缺 UploadURL", SnapShotBlock{SnapNum: &snapNum, SessionID: &sessionID}, "UploadURL 必选"},
		{"缺 SessionID", SnapShotBlock{SnapNum: &snapNum, UploadURL: &uploadURL}, "SessionID 必选"},
		{"SnapNum 越上界", SnapShotBlock{SnapNum: intPtr(MaxSnapShotCount + 1), UploadURL: &uploadURL, SessionID: &sessionID}, "SnapNum 越界"},
		{"SnapNum 越下界", SnapShotBlock{SnapNum: intPtr(MinSnapShotCount - 1), UploadURL: &uploadURL, SessionID: &sessionID}, "SnapNum 越界"},
		{"SessionID 太短", SnapShotBlock{SnapNum: &snapNum, UploadURL: &uploadURL, SessionID: stringPtr("short")}, "SessionID 长度越界"},
		{"SessionID 太长", SnapShotBlock{SnapNum: &snapNum, UploadURL: &uploadURL, SessionID: stringPtr(strings.Repeat("s", MaxSnapShotSessionIDLen+1))}, "SessionID 长度越界"},
		{"UploadURL 不是 http", SnapShotBlock{SnapNum: &snapNum, UploadURL: stringPtr("file:///tmp/x.jpg"), SessionID: &sessionID}, "http/https"},
		{"UploadURL 缺主机", SnapShotBlock{SnapNum: &snapNum, UploadURL: stringPtr("http:///upload"), SessionID: &sessionID}, "主机名"},
		{"Interval 越界", SnapShotBlock{SnapNum: &snapNum, Interval: intPtr(MaxSnapShotInterval + 1), UploadURL: &uploadURL, SessionID: &sessionID}, "Interval 越界"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			blocks := DeviceConfigBlocks{SnapShot: &testCase.block}
			err := ValidateDeviceConfigBlocks(blocks)
			require.Error(t, err)
			require.Contains(t, err.Error(), testCase.wantMsg)
		})
	}
}

// TestSnapShotValidateAcceptsBoundary 反向锚点：边界值必须放行。
//
// ⛔ 少了这条，"把所有可选字段都当必选"也能全绿 —— 那样设备真的不指定间隔时
// （A.2.1.24 里 `Interval` 确实是 `minOccurs="0"`）平台会拒发一份标准允许的配置。
func TestSnapShotValidateAcceptsBoundary(t *testing.T) {
	snapNum := MaxSnapShotCount
	sessionID := strings.Repeat("s", MinSnapShotSessionIDLen)
	uploadURL := "https://example.com/upload/"

	require.NoError(t, ValidateDeviceConfigBlocks(DeviceConfigBlocks{SnapShot: &SnapShotBlock{
		SnapNum: &snapNum, UploadURL: &uploadURL, SessionID: &sessionID,
	}}), "Interval 缺席是合法的（minOccurs=0）")

	// 全零面积之外的边界：SnapNum=1、SessionID 恰好下界。
	one := MinSnapShotCount
	require.NoError(t, ValidateDeviceConfigBlocks(DeviceConfigBlocks{SnapShot: &SnapShotBlock{
		SnapNum: &one, Interval: intPtr(MaxSnapShotInterval), UploadURL: &uploadURL, SessionID: &sessionID,
	}}))
}

// TestReadOnlyDeviceConfigTypeIsRejectedOnWrite 钉住"只读类型不许下发"。
//
// ⛔ 这不是洁癖：`VideoParamOpt` 在容器里有字段、`PresentConfigTypes()` 也会把它算进去，
// 但 A.2.3.2 的下发清单里**没有这个元素**。不拒发的话，平台会发出一条
// "声称要配 VideoParamOpt、报文里却一个块都没有"的 DeviceConfig —— 设备回 OK、
// 平台记 accepted，配置从头到尾没传出去，而两侧日志都正常。
func TestReadOnlyDeviceConfigTypeIsRejectedOnWrite(t *testing.T) {
	reason, readOnly := ReadOnlyDeviceConfigTypeReason(ConfigTypeVideoParamOpt)
	require.True(t, readOnly)
	require.NotEmpty(t, reason)

	blocks := DeviceConfigBlocks{BasicParam: &BasicParamBlock{Expiration: intPtr(3600)},
		VideoParamOpt: &VideoParamOptBlock{DownloadSpeed: "1/2", Resolution: "5/6"}}
	err := ValidateDeviceConfigBlocks(blocks)
	require.Error(t, err, "混了只读类型也必须整条拒绝，不能只丢掉那一块")
	require.Contains(t, err.Error(), ConfigTypeVideoParamOpt)

	// 反向锚点：只读类型**单独**出现时也要拒（别写成"至少有一个可下发块就放过"）。
	require.Error(t, ValidateDeviceConfigBlocks(DeviceConfigBlocks{
		VideoParamOpt: &VideoParamOptBlock{DownloadSpeed: "1/2"},
	}))
}

func stringPtr(value string) *string { return &value }

// ==================== A.2.5.7 完成通知（标准形态，一对多） ====================

// hikUploadSnapShotFinishedBody 是海康 `37010301021320000002` 的真机原文
// （2026-09-20 13:31:04 主动上报，`SN=9402`）。⛔ 两处照抄：根元素 `<Notify>`；
// `<SnapShotList>` 是**一张一个并列元素**（不是"一个列表里塞三个文件标识"）。
const hikUploadSnapShotFinishedBody = `<?xml version="1.0" encoding="GB18030"?><Notify><CmdType>UploadSnapShotFinished</CmdType><SN>9402</SN><DeviceID>37010301021320000002</DeviceID><SessionID>probe-b-000000000000000000000000000000</SessionID><SnapShotList><SnapShotFileID>37010301021320000002022026092013305816101</SnapShotFileID></SnapShotList><SnapShotList><SnapShotFileID>3701030102132000000202202609201331014002</SnapShotFileID></SnapShotList><SnapShotList><SnapShotFileID>3701030102132000000202202609201331044003</SnapShotFileID></SnapShotList></Notify>`

// TestParseUploadSnapShotFinished_FollowsHikvisionWire 用真机原文钉住标准完成通知的解析。
//
// ⛔ 真机把 `<SnapShotList>` **重复了三遍**（一张一个）。若只按标准文字"一个 SnapShotList
// 里放 N 个 SnapShotFileID"去写，这里会解析出 0 个标识 —— 而现象是"有应答无数据"，
// 两侧都不报错，会话则永远完不成。
func TestParseUploadSnapShotFinished_FollowsHikvisionWire(t *testing.T) {
	finished, err := ParseUploadSnapShotFinished([]byte(hikUploadSnapShotFinishedBody))
	require.NoError(t, err)
	require.Equal(t, CmdUploadSnapShotFinished, finished.CmdType)
	require.Equal(t, "9402", finished.SN)
	require.Equal(t, "37010301021320000002", finished.DeviceID)
	require.Equal(t, "probe-b-000000000000000000000000000000", finished.SessionID)
	require.Equal(t, []string{
		"37010301021320000002022026092013305816101",
		"3701030102132000000202202609201331014002",
		"3701030102132000000202202609201331044003",
	}, finished.FileIDs, "必须收全三张，且保持报文顺序")
}

// TestParseUploadSnapShotFinished_AcceptsStandardSingleList 反向锚点：**标准写法**
// （一个 `<SnapShotList>` 里放多个 `<SnapShotFileID>`）也必须吃下 —— 只认真机那一种
// 是把厂商实现当标准，换一家设备就瞎。
func TestParseUploadSnapShotFinished_AcceptsStandardSingleList(t *testing.T) {
	body := `<?xml version="1.0" encoding="UTF-8"?><Notify><CmdType>UploadSnapShotFinished</CmdType><SN>1</SN><DeviceID>D</DeviceID><SessionID>s</SessionID><SnapShotList><SnapShotFileID>a</SnapShotFileID><SnapShotFileID>b</SnapShotFileID></SnapShotList></Notify>`
	finished, err := ParseUploadSnapShotFinished([]byte(body))
	require.NoError(t, err)
	require.Equal(t, []string{"a", "b"}, finished.FileIDs)
}

// TestParseUploadSnapShotFinishedDedupesFileIDs 去重：同一个标识出现两次只算一次
// （`NotifiedCount` 是"完成了多少个文件"的基数，重复报文不能把计数灌上去）。
func TestParseUploadSnapShotFinishedDedupesFileIDs(t *testing.T) {
	body := `<?xml version="1.0" encoding="UTF-8"?><Notify><CmdType>UploadSnapShotFinished</CmdType><SN>1</SN><DeviceID>D</DeviceID><SessionID>s</SessionID><SnapShotList><SnapShotFileID>a</SnapShotFileID></SnapShotList><SnapShotList><SnapShotFileID>a</SnapShotFileID></SnapShotList></Notify>`
	finished, err := ParseUploadSnapShotFinished([]byte(body))
	require.NoError(t, err)
	require.Equal(t, []string{"a"}, finished.FileIDs)
}

// TestParseUploadSnapShotFinishedRejectsUnusable 只有"解析出来也没用"的三种才判非法。
//
// ⛔ 刻意**不**按标准校验"≤10 个标识"：这是入站数据，设备说几个就是几个。收严了会把
// "抓拍完成"这个信号本身弄丢（会话永远完不成），代价远大于收益。越界的处置留给
// `devicecapture` 的完成判定（E-5 那条"部分失败语义"）。
func TestParseUploadSnapShotFinishedRejectsUnusable(t *testing.T) {
	cases := []struct {
		name   string
		body   string
		errMsg string
	}{
		{"CmdType 不对", `<Notify><CmdType>Notify</CmdType><DeviceID>D</DeviceID><SessionID>s</SessionID><SnapShotList><SnapShotFileID>a</SnapShotFileID></SnapShotList></Notify>`, "CmdType"},
		{"缺 SessionID", `<Notify><CmdType>UploadSnapShotFinished</CmdType><DeviceID>D</DeviceID><SnapShotList><SnapShotFileID>a</SnapShotFileID></SnapShotList></Notify>`, "SessionID"},
		{"没有文件标识", `<Notify><CmdType>UploadSnapShotFinished</CmdType><DeviceID>D</DeviceID><SessionID>s</SessionID></Notify>`, "SnapShotFileID"},
		{"空标签的文件标识", `<Notify><CmdType>UploadSnapShotFinished</CmdType><DeviceID>D</DeviceID><SessionID>s</SessionID><SnapShotList><SnapShotFileID>  </SnapShotFileID></SnapShotList></Notify>`, "SnapShotFileID"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseUploadSnapShotFinished([]byte(tc.body))
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.errMsg)
		})
	}
}

// ==================== 私有形态（模拟器口径，兼容保留） ====================

// TestParseSnapshotNotify 钉住私有形态（一图一条）。
//
// ⛔ 这不是标准：A.2.5.7 的形态见 [ParseUploadSnapShotFinished]。
// `SubCmd` / `SnapShotID` 在 GB/T 28181-2022 全文**零命中**。
// ⭐ 保留的唯一理由：模拟器（`uvp-gb28181-sim` 的 `SnapShotNotifyBuilder.kt`）当前发的
// 就是这个形状，它是本仓唯一的端到端联调对象。等 E-2 模拟器侧也改标准后可删。
func TestParseSnapshotNotify(t *testing.T) {
	notify, err := ParseSnapshotNotify([]byte(`<?xml version="1.0" encoding="UTF-8"?><Notify><CmdType>Notify</CmdType><SubCmd>SnapShot</SubCmd><SN>9</SN><DeviceID>34020000001320000001</DeviceID><SessionID>session-1</SessionID><SnapShotID>shot-1</SnapShotID><Time>2026-08-30T23:30:00+08:00</Time><StoragePath>http://localhost/shot-1.jpg</StoragePath></Notify>`))
	require.NoError(t, err)
	require.Equal(t, "session-1", notify.SessionID)
	require.Equal(t, "shot-1", notify.SnapshotID)
}

// TestParseUploadSnapShotFinishedDoesNotSwallowPrivateShape 反向锚点：**私有形态不能**被
// 标准解析器收下。两者容器名完全不同（`SnapShotID` vs `SnapShotList/SnapShotFileID`），
// 一旦标准解析器"宽容到"连私有形态也吃，两条入站门禁的区分就没了。
func TestParseUploadSnapShotFinishedDoesNotSwallowPrivateShape(t *testing.T) {
	_, err := ParseUploadSnapShotFinished([]byte(`<?xml version="1.0" encoding="UTF-8"?><Notify><CmdType>Notify</CmdType><SubCmd>SnapShot</SubCmd><SN>9</SN><DeviceID>34020000001320000001</DeviceID><SessionID>session-1</SessionID><SnapShotID>shot-1</SnapShotID></Notify>`))
	require.Error(t, err)
}

package ptz

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"uvplatform.cn/uvp-gb28181/app/gb28181/manscdp"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/protocol"
)

func storageCardFormatTarget() Target {
	return Target{
		DeviceID: 2, DeviceCode: "D", ChannelID: 1, ChannelCode: "C",
		IP: "192.0.2.10", Port: 5060, Transport: "UDP",
		DeviceOnline: true, ChannelOnline: true,
	}
}

// TestFormatStorageCardPersistsDestructiveActionContract 下发存储卡格式化后落库的 operation
// 必须满足三条契约（每一条错了都只在真机上才看得出来）：
//
//  1. `action='format_sd'` —— 设备维护记录按它筛选（controllers.maintenanceOperationActions）；
//     写成别的名字的后果是"格式化了但维护记录里查不到"，事后无从追责。
//  2. `ResponseRequired=true` 且 `MaxAttempts=1` —— 破坏性动作**不重发**。查询族用 3 次重发
//     是合理的（重发查询无害），对格式化则可能把"已下发、仅应答丢失"再执行一遍。
//  3. `TargetCode` = 通道编码 —— 与 SDCardStatus 查询同源。两边不同源的话，
//     "查询看到的卡"与"格式化动的卡"会落在两个 target_code 上，回读对账永远对不上。
func TestFormatStorageCardPersistsDestructiveActionContract(t *testing.T) {
	service, db := newStorageCardTestService(t)
	operation, err := service.FormatStorageCard(context.Background(), storageCardFormatTarget(), 2, 17, 3, "fmt-1")
	require.NoError(t, err)

	require.Equal(t, "fmt-1", operation.IdempotencyKey)
	require.Equal(t, "format_sd", operation.Action)
	require.Equal(t, manscdp.CmdDeviceControl, operation.CmdType)
	require.False(t, operation.ResponseRequired, "9.3.1 d)：存储卡格式化是无应答命令")
	require.Equal(t, 1, operation.MaxAttempts, "破坏性动作不得重发")
	require.Equal(t, gbmodels.ControlTargetScopeChannel, operation.TargetScope)
	require.Equal(t, "C", operation.TargetCode)
	require.Equal(t, uint(17), operation.ActorID)
	require.Equal(t, uint(3), operation.ActorDeptID)
	// payload 必须带上卡编号：scheduler 重发时只能靠它重建报文。
	require.Contains(t, operation.PayloadJSON, `"cardIndex":2`)

	var stored gbmodels.GbPTZOperation
	require.NoError(t, db.Where("operation_id = ?", operation.OperationID).First(&stored).Error)
	require.Equal(t, "format_sd", stored.Action)
}

// TestFormatStorageCardAcceptsZeroAsFormatAllCards 0 是标准里的**合法值**（"对所有存储卡进行格式化"），
// 不能被当成"没传"。这条用例钉的是"值类型零值 vs 显式 0"的分界 —— 用 `*int` 收参、
// 用指针取字段、payload 里原样落 0，三处都必须保留这个区别。
func TestFormatStorageCardAcceptsZeroAsFormatAllCards(t *testing.T) {
	service, db := newStorageCardTestService(t)
	operation, err := service.FormatStorageCard(context.Background(), storageCardFormatTarget(), 0, 17, 3, "fmt-all")
	require.NoError(t, err)
	require.Contains(t, operation.PayloadJSON, `"cardIndex":0`)

	var stored gbmodels.GbPTZOperation
	require.NoError(t, db.Where("operation_id = ?", operation.OperationID).First(&stored).Error)
	require.Contains(t, stored.PayloadJSON, `"cardIndex":0`)
}

// TestFormatStorageCardRejectsInvalidTargets 参数门禁在服务层也要有一道：
// 控制器会先挡一遍，但内部编排可以绕过控制器直接调服务。
func TestFormatStorageCardRejectsInvalidTargets(t *testing.T) {
	service, _ := newStorageCardTestService(t)

	_, err := service.FormatStorageCard(context.Background(), storageCardFormatTarget(), -1, 17, 3, "neg")
	require.ErrorContains(t, err, "不能为负数")

	broken := storageCardFormatTarget()
	broken.ChannelCode = ""
	_, err = service.FormatStorageCard(context.Background(), broken, 1, 17, 3, "no-target")
	require.ErrorContains(t, err, "缺少目标通道编码")
}

// TestBuildScheduledPTZBodyRebuildsFormatStorageCard 钉**调度重发**这条路径。
//
// ⛔ 这是本功能最容易漏的一半：控制器那条路走的是 `Command.Build` 闭包，
// 而 scheduler 重发走的是 `buildScheduledPTZBody` 里的 action 白名单 + switch ——
// 两处都要登记。只改一处的话，首次下发正常、重发时报
// `不支持持久化调度的 PTZ control: format_sd`，而单测（只测 Build 闭包）全绿。
func TestBuildScheduledPTZBodyRebuildsFormatStorageCard(t *testing.T) {
	profile := protocol.ProfileFor(protocol.Version2022)
	operation := gbmodels.GbPTZOperation{
		OperationID: "op-fmt", CmdType: manscdp.CmdDeviceControl, Action: "format_sd",
		TargetCode: "C", ChannelCode: "C", SN: 9, PayloadJSON: `{"action":"format_sd","cardIndex":2}`,
		ProfileVersion: string(profile.Version), ProfileCharset: string(profile.Charset),
	}

	body, err := buildScheduledPTZBody(operation)
	require.NoError(t, err)
	text := string(body)
	require.Contains(t, text, "<FormatSDCard>2</FormatSDCard>")
	// ⛔ 元素**就是**卡编号本身，没有 DiskNum 这层包装（标准里没有 DiskNum 这个名字）。
	require.NotContains(t, text, "DiskNum")
	// ⛔ FormatSDCard 与 SN/DeviceID 是**同级**直接子元素（A.2.3.1.1 的请求序列），
	//    根元素是 <Control>；写成 <DeviceControl> 包一层会被设备当作非法报文。
	require.Contains(t, text, "<CmdType>DeviceControl</CmdType>")
	require.Contains(t, text, "<DeviceID>C</DeviceID>")

	// 0 = 格式化全部卡：`omitempty` 对 int 零值会整个省略字段，指针才能保住它。
	operation.PayloadJSON = `{"action":"format_sd","cardIndex":0}`
	body, err = buildScheduledPTZBody(operation)
	require.NoError(t, err)
	require.Contains(t, string(body), "<FormatSDCard>0</FormatSDCard>",
		"0 是合法取值（全部卡），不能被 omitempty 吃掉")

	// 反向对照：白名单外的 action 仍必须报错，证明上面走的确实是那条分支。
	operation.Action = "not_whitelisted"
	operation.PayloadJSON = `{}`
	_, err = buildScheduledPTZBody(operation)
	require.ErrorContains(t, err, "不支持持久化调度的 PTZ control")
}

// TestFormatStorageCardIsDeliberatelyOneWay 钉住"无应答"这条协议事实（防回归）。
//
// 依据是标准原文，不是实现偏好：
//   - 9.3.1 d)：源设备发送…「存储卡格式化」…命令后，**目标设备不发送应答命令**，流程见 9.3.2.1；
//   - 表 1 序号 12：「存储卡格式化 A.2.3.1.13」的应答命令章节 = 「（无）」。
//
// ⛔ 若有人"体贴地"把它改成 ResponseRequired=true，不会报错、单测之外的路径也照跑，
//    只是在真机上每次格式化都要白等到 transport deadline 才落 timeout —— 表现为
//    "格式化功能总是结果未知"。所以这条契约必须在服务层钉住。
//
// 与之配套：判"格式化成没成"要靠**事后再查一次 SDCardStatus**（Status=formatting
// 带 FormatProgress，或已回到 ok/unformatted），前端也是按这个口径提示用户的。
func TestFormatStorageCardIsDeliberatelyOneWay(t *testing.T) {
	service, db := newStorageCardTestService(t)
	operation, err := service.FormatStorageCard(context.Background(), storageCardFormatTarget(), 1, 0, 0, "one-way-check")
	require.NoError(t, err)
	require.False(t, operation.ResponseRequired, "存储卡格式化是无应答命令（9.3.1 d) / 表1 序号12）")
	require.NotEqual(t, gbmodels.PTZOperationTimeout, operation.Status,
		"单向命令不该出现等待应答导致的超时态")

	var stored gbmodels.GbPTZOperation
	require.NoError(t, db.Where("operation_id = ?", operation.OperationID).First(&stored).Error)
	require.False(t, stored.ResponseRequired)
	require.Equal(t, 1, stored.MaxAttempts)
	// 单向下发是**同步**发出的（Execute 的注释：legacy one-way commands retain their
	// synchronous send semantics），所以一次调用之后它就已经是终态 `sent` 了 ——
	// 不存在"等设备应答"这个中间态。前端因此只需要把 `sent` 显示成"已下发"，
	// 真正的结果靠再查一次 SDCardStatus 得到。
	require.Equal(t, gbmodels.PTZOperationSent, stored.Status)
}

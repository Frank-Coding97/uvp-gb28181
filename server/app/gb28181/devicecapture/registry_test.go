package devicecapture

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRegistryCorrelatesUploadAndSnapshotNotify(t *testing.T) {
	registry := NewRegistry(t.TempDir())
	session := registry.Create(CreateRequest{OwnerID: "12", ChannelID: "31", ChannelCode: "34020000001320000001", DeviceCode: "34020000002000000001", SnapNum: 1, Interval: 1})
	jpeg := []byte{0xff, 0xd8, 0x01, 0x02, 0xff, 0xd9}
	file, err := registry.Upload(session.UploadToken, "shot-1.jpg", "image/jpeg", bytes.NewReader(jpeg))
	require.NoError(t, err)
	require.FileExists(t, file.Path)

	err = registry.OnSnapshotNotify(context.Background(), session.DeviceCode, []byte(`<?xml version="1.0" encoding="UTF-8"?><Notify><CmdType>Notify</CmdType><SubCmd>SnapShot</SubCmd><SN>9</SN><DeviceID>34020000001320000001</DeviceID><SessionID>`+session.ID+`</SessionID><SnapShotID>shot-1</SnapShotID><Time>2026-08-30T23:30:00+08:00</Time><StoragePath>http://localhost/shot-1.jpg</StoragePath></Notify>`))
	require.NoError(t, err)
	got, ok := registry.GetForOwner(session.ID, "12")
	require.True(t, ok)
	require.Equal(t, StateCompleted, got.State)
	require.Len(t, got.Files, 1)
	require.Equal(t, 1, got.NotifiedCount)
	require.NoError(t, os.Remove(file.Path))
}

func TestRegistryRejectsInvalidJPEGAndCapability(t *testing.T) {
	registry := NewRegistry(t.TempDir())
	session := registry.Create(CreateRequest{OwnerID: "12", ChannelID: "31", ChannelCode: "channel", DeviceCode: "device", SnapNum: 1, Interval: 1})
	_, err := registry.Upload("wrong", "shot.jpg", "image/jpeg", bytes.NewReader([]byte{0xff, 0xd8, 0xff, 0xd9}))
	require.ErrorIs(t, err, ErrNotFound)
	_, err = registry.Upload(session.UploadToken, "shot.jpg", "image/jpeg", bytes.NewReader([]byte("not-jpeg")))
	require.ErrorIs(t, err, ErrInvalidImage)
}

// uploadJPEG 往会话里塞一张最小合法 JPEG（抓拍会话的完成判定要求 Files 也够数）。
func uploadJPEG(t *testing.T, registry *Registry, session Session, name string) {
	t.Helper()
	_, err := registry.Upload(session.UploadToken, name, "image/jpeg", bytes.NewReader([]byte{0xff, 0xd8, 0x01, 0x02, 0xff, 0xd9}))
	require.NoError(t, err)
}

// uploadSnapShotFinishedBody 按 A.2.5.7 的标准形态拼一条完成通知。
// 用**并列的多个 `<SnapShotList>`**（真机口径），而不是"一个列表塞多个标识"。
func uploadSnapShotFinishedBody(sessionID, deviceID string, fileIDs ...string) []byte {
	body := `<?xml version="1.0" encoding="GB18030"?><Notify><CmdType>UploadSnapShotFinished</CmdType><SN>1</SN><DeviceID>` + deviceID + `</DeviceID><SessionID>` + sessionID + `</SessionID>`
	for _, id := range fileIDs {
		body += `<SnapShotList><SnapShotFileID>` + id + `</SnapShotFileID></SnapShotList>`
	}
	return []byte(body + `</Notify>`)
}

// TestRegistryCompletesSessionOnStandardFinishedNotify 是本轮 E-2 的**主锚点**。
//
// ⛔ 要验的语义是"**按文件标识**计数，不按通知条数"：一条标准通知带 3 个标识，必须把
// `NotifiedCount` 推到 3。若按条数计（或把标准通知整个漏掉），计数恒为 1 / 恒为 0 ⇒
// `completeLocked` 的 `NotifiedCount >= SnapNum` 永远不成立 ⇒ 会话永远停在 `receiving`，
// 前端（`SnapshotConfigPanel.vue`）一直显示未完成，而图片其实早就落盘了。
func TestRegistryCompletesSessionOnStandardFinishedNotify(t *testing.T) {
	registry := NewRegistry(t.TempDir())
	session := registry.Create(CreateRequest{
		OwnerID: "12", ChannelID: "31", ChannelCode: "34020000001320000001",
		DeviceCode: "34020000002000000001", SnapNum: 3, Interval: 1,
	})
	for _, name := range []string{"shot-1.jpg", "shot-2.jpg", "shot-3.jpg"} {
		uploadJPEG(t, registry, session, name)
	}

	body := uploadSnapShotFinishedBody(session.ID, "34020000001320000001", "id-1", "id-2", "id-3")
	require.NoError(t, registry.OnUploadSnapShotFinished(context.Background(), session.DeviceCode, body))

	got, ok := registry.GetForOwner(session.ID, "12")
	require.True(t, ok)
	require.Equal(t, 3, got.NotifiedCount, "3 个文件标识必须计成 3，不是 1")
	require.Equal(t, StateCompleted, got.State, "文件齐 + 标识齐 ⇒ 会话必须终结")
	// ⛔ 对外返回的副本必须不带内部记账 map（`cloneSession` 负责置空）：那是个并发读写的
	// 内部对象，跟着 JSON/调用方出去就等于把没有锁保护的 map 交出去。
	require.Nil(t, got.notifiedIDs, "对外副本不得携带内部记账 map")
}

// TestRegistryStandardFinishedNotifyCountsFileIDsNotMessages 反向锚点：**图片齐了但标识只报 1 个**
// 时，会话仍不许判完成 —— 同时钉住两件事：
//   - `NotifiedCount` 只能数"标识"，不能数"收到几条通知"；
//   - `completeLocked` 的 `NotifiedCount >= SnapNum` 这一半**是承重的**（只有 `Files` 够不算完）。
func TestRegistryStandardFinishedNotifyCountsFileIDsNotMessages(t *testing.T) {
	registry := NewRegistry(t.TempDir())
	session := registry.Create(CreateRequest{
		OwnerID: "12", ChannelID: "31", ChannelCode: "34020000001320000001",
		DeviceCode: "34020000002000000001", SnapNum: 3, Interval: 1,
	})
	for _, name := range []string{"shot-1.jpg", "shot-2.jpg", "shot-3.jpg"} {
		uploadJPEG(t, registry, session, name)
	}

	body := uploadSnapShotFinishedBody(session.ID, "34020000001320000001", "id-1")
	require.NoError(t, registry.OnUploadSnapShotFinished(context.Background(), session.DeviceCode, body))

	got, _ := registry.GetForOwner(session.ID, "12")
	require.Len(t, got.Files, 3, "三张图都到了")
	require.Equal(t, 1, got.NotifiedCount)
	require.Equal(t, StateReceiving, got.State, "设备只说完成 1/3 ⇒ 不许判完成（图齐不等于标识齐）")
}

// TestRegistryStandardFinishedNotifyIsIdempotent 去重：同一条通知重发（设备没收到 200 就会
// 重发）不能把 `NotifiedCount` 灌到超过实际完成数，否则"部分失败"会被误判成"全成功"。
func TestRegistryStandardFinishedNotifyIsIdempotent(t *testing.T) {
	registry := NewRegistry(t.TempDir())
	session := registry.Create(CreateRequest{
		OwnerID: "12", ChannelID: "31", ChannelCode: "34020000001320000001",
		DeviceCode: "34020000002000000001", SnapNum: 3, Interval: 1,
	})
	for _, name := range []string{"shot-1.jpg", "shot-2.jpg", "shot-3.jpg"} {
		uploadJPEG(t, registry, session, name)
	}
	body := uploadSnapShotFinishedBody(session.ID, "34020000001320000001", "id-1", "id-2", "id-3")
	require.NoError(t, registry.OnUploadSnapShotFinished(context.Background(), session.DeviceCode, body))
	require.NoError(t, registry.OnUploadSnapShotFinished(context.Background(), session.DeviceCode, body))

	got, _ := registry.GetForOwner(session.ID, "12")
	require.Equal(t, 3, got.NotifiedCount, "重复报文不得重复计数")
}

// TestRegistryFinishedNotifyRejectsForeignDevice 归属校验（这次重写动过，必须钉住）。
//
// ⛔ **两条独立校验，必须各有一条用例**——2026-09-20 变异自检发现：只写"来源也外来"的用例时，
// 报文体 `DeviceID` 那道校验被删掉仍然全绿（因为来源校验先把它挡住了）。所以这里分别覆盖：
//  1. SIP 来源（`ptzDeviceCode` 取的 `From`）不是本会话设备/通道 ⇒ 拒；
//  2. 来源合法、但**报文自己声称的 `DeviceID`** 是别的设备 ⇒ 也必须拒（设备可以把自己的
//     `From` 写对，却在 body 里塞另一个设备编码）。
func TestRegistryFinishedNotifyRejectsForeignDevice(t *testing.T) {
	const foreign = "99999999999999999999"

	t.Run("来源设备不对", func(t *testing.T) {
		registry := NewRegistry(t.TempDir())
		session := registry.Create(CreateRequest{
			OwnerID: "12", ChannelID: "31", ChannelCode: "34020000001320000001",
			DeviceCode: "34020000002000000001", SnapNum: 1, Interval: 1,
		})
		body := uploadSnapShotFinishedBody(session.ID, foreign, "id-1")
		require.Error(t, registry.OnUploadSnapShotFinished(context.Background(), foreign, body))

		got, _ := registry.GetForOwner(session.ID, "12")
		require.Equal(t, 0, got.NotifiedCount)
		require.NotEqual(t, StateCompleted, got.State)
	})

	t.Run("来源对但报文声称别的设备", func(t *testing.T) {
		registry := NewRegistry(t.TempDir())
		session := registry.Create(CreateRequest{
			OwnerID: "12", ChannelID: "31", ChannelCode: "34020000001320000001",
			DeviceCode: "34020000002000000001", SnapNum: 1, Interval: 1,
		})
		body := uploadSnapShotFinishedBody(session.ID, foreign, "id-1")
		err := registry.OnUploadSnapShotFinished(context.Background(), session.DeviceCode, body)
		require.Error(t, err, "来源合法不代表报文可以声称任何设备")

		got, _ := registry.GetForOwner(session.ID, "12")
		require.Equal(t, 0, got.NotifiedCount)
		require.NotEqual(t, StateCompleted, got.State)
	})
}

// TestRegistryFinishedNotifyUnknownSession 通知到不存在的会话时，两条路都必须报
// `ErrNotFound`（入站门禁放行得更宽了，这里不能跟着松）。
func TestRegistryFinishedNotifyUnknownSession(t *testing.T) {
	registry := NewRegistry(t.TempDir())
	standard := uploadSnapShotFinishedBody("no-such-session", "34020000001320000001", "id-1")
	require.ErrorIs(t, registry.OnUploadSnapShotFinished(context.Background(), "34020000001320000001", standard), ErrNotFound)

	private := []byte(`<?xml version="1.0" encoding="UTF-8"?><Notify><CmdType>Notify</CmdType><SubCmd>SnapShot</SubCmd><SN>9</SN><DeviceID>34020000001320000001</DeviceID><SessionID>no-such-session</SessionID><SnapShotID>shot-1</SnapShotID></Notify>`)
	require.ErrorIs(t, registry.OnSnapshotNotify(context.Background(), "34020000001320000001", private), ErrNotFound)
}

// TestUploadRecordsRelativePathAndDigest 上传时必须同时产出**落库要用**的两样东西：
// 相对路径（`rel_path`）与图片摘要（`md5`）。
//
// ⛔ rel_path 必须是相对基目录、正斜杠分隔：库里的路径得能跟着 serverroot 一起搬卷，
// 存绝对路径的库一搬机器就全部指向不存在的文件。
func TestUploadRecordsRelativePathAndDigest(t *testing.T) {
	registry := NewRegistry(t.TempDir())
	session := registry.Create(CreateRequest{OwnerID: "12", ChannelID: "31", ChannelCode: "channel", SnapNum: 1, Interval: 1})
	jpeg := []byte{0xff, 0xd8, 0x01, 0x02, 0xff, 0xd9}
	file, err := registry.Upload(session.UploadToken, "shot-1.jpg", "image/jpeg", bytes.NewReader(jpeg))
	require.NoError(t, err)

	require.Equal(t, "gb-device-snapshots/"+session.ID+"/shot-1.jpg", file.RelPath)
	require.NotContains(t, file.RelPath, "\\", "rel_path 不得出现反斜杠（库要跨平台可读）")
	// 摘要是**图片字节**的 md5（10 字节输入 ⇒ e4f4a6… 之类），不是文件名的。
	require.Len(t, file.MD5, 32)
	require.NotContains(t, file.MD5, "-", "必须是裸 hex，不带 uuid 风格分隔符")
	require.Equal(t, md5Hex(jpeg), file.MD5)

	// 同一份字节重传：大小与摘要不变（幂等的前提）。
	again, err := registry.Upload(session.UploadToken, "shot-1.jpg", "image/jpeg", bytes.NewReader(jpeg))
	require.NoError(t, err)
	require.Equal(t, file.MD5, again.MD5)
	require.Equal(t, file.RelPath, again.RelPath)
}

// TestFilePathResolvesLibraryRelativePath 是**稳定读接口**的路径解析锚点：
// 库里存的 rel_path 必须能还原成本进程可读的路径，且与落盘路径是同一个文件。
func TestFilePathResolvesLibraryRelativePath(t *testing.T) {
	registry := NewRegistry(t.TempDir())
	session := registry.Create(CreateRequest{OwnerID: "12", ChannelID: "31", ChannelCode: "channel", SnapNum: 1, Interval: 1})
	file, err := registry.Upload(session.UploadToken, "shot-1.jpg", "image/jpeg", bytes.NewReader([]byte{0xff, 0xd8, 0x01, 0x02, 0xff, 0xd9}))
	require.NoError(t, err)

	resolved, err := registry.FilePath(file.RelPath)
	require.NoError(t, err)
	require.FileExists(t, resolved)
	// 与 Upload 写文件用的那个路径指向同一个文件（不是"另一个恰好也存在的路径"）。
	direct, err := filepath.Abs(file.Path)
	require.NoError(t, err)
	require.Equal(t, direct, resolved)
}

// TestFilePathRejectsEscapingRelativePath 反向锚点：库里的 rel_path 可被人工改写、
// 也可被旧版本写入，所以解析必须挡住"逃出基目录"的值。
//
// ⛔ 没有这一层时 `filepath.Join` 会把 `../…` 正常"清理"成一条基目录之外的真实路径，
// 于是读图接口变成一个任意文件读取口子 —— 而且请求侧看不出来（它只是个 id）。
func TestFilePathRejectsEscapingRelativePath(t *testing.T) {
	registry := NewRegistry(t.TempDir())
	for _, relPath := range []string{
		"",
		"../secret.jpg",
		"../../../../etc/passwd",
		"gb-device-snapshots/../../secret.jpg",
	} {
		t.Run("拒绝 "+relPath, func(t *testing.T) {
			_, err := registry.FilePath(relPath)
			require.ErrorIs(t, err, ErrNotFound, "逃出基目录的路径必须被拒")
		})
	}

	// ⛔ 绝对路径形态（`/etc/passwd`）不是"拒绝"而是"被限制在基目录内"：
	// `filepath.Join` 会把后续元素一律当相对段，所以解析结果是 `<基目录>/etc/passwd`。
	// 这同样安全（读不到基目录外的文件），但**性质不同** —— 混在"必须报错"里断言
	// 会把一个本来安全的行为写成失败。这里显式钉住它的真实语义。
	t.Run("绝对路径被限制在基目录内", func(t *testing.T) {
		resolved, err := registry.FilePath("/etc/passwd")
		require.NoError(t, err)
		base, err := filepath.Abs(registry.root)
		require.NoError(t, err)
		require.True(t, strings.HasPrefix(resolved, base+string(os.PathSeparator)),
			"解析结果必须仍在基目录内: %s", resolved)
	})
}

// TestSessionForTokenIsTheOnlyWayToAttributeAnUpload 收图侧只能靠令牌把
// "这张图属于哪个设备/通道/会话"问出来（设备 POST 的路径只有 token）。
func TestSessionForTokenIsTheOnlyWayToAttributeAnUpload(t *testing.T) {
	registry := NewRegistry(t.TempDir())
	session := registry.Create(CreateRequest{
		OwnerID: "12", ChannelID: "31", ChannelCode: "34020000001320000001",
		DeviceCode: "34020000002000000001", SnapNum: 1, Interval: 1,
	})
	got, ok := registry.SessionForToken(session.UploadToken)
	require.True(t, ok)
	require.Equal(t, session.ID, got.ID)
	require.Equal(t, "31", got.ChannelID)
	require.Equal(t, "34020000001320000001", got.ChannelCode)
	require.Equal(t, "34020000002000000001", got.DeviceCode)

	_, ok = registry.SessionForToken("no-such-token")
	require.False(t, ok)
	_, ok = registry.SessionForToken("")
	require.False(t, ok)
}

func md5Hex(payload []byte) string {
	digest := md5.Sum(payload)
	return hex.EncodeToString(digest[:])
}

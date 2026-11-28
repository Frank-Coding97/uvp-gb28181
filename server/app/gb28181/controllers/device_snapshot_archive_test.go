package controllers_test

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"uvplatform.com/uvp-gb28181/app/gb28181/devicecapture"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
	basemodels "uvplatform.com/uvp-gb28181/app/models"
)

// snapshotArchiveFixture 造一个只挂了批量下载路由的路由器，外加**真实的**抓拍存储目录。
//
// ⛔ 用真目录而不是桩：`registry.FilePath` 内含**路径穿越**校验，桩掉它等于把
// "rel_path 被篡改"这条防线一起删了，而这条正是打包下载最危险的一处。
func snapshotArchiveFixture(t *testing.T) (*gin.Engine, *gorm.DB, *devicecapture.Registry, string) {
	t.Helper()
	useRealResponseHandler(t)
	controller, db, _, _ := newPTZResourceController(t)
	require.NoError(t, db.AutoMigrate(&gbmodels.GbChannelSnapshot{},
		&basemodels.SysDepartment{}, &basemodels.SysRole{}, &basemodels.SysUserRole{}, &basemodels.User{}))
	root := t.TempDir()
	registry := devicecapture.NewRegistry(root)
	controller.SetCaptureRuntime(registry)
	router := gin.New()
	router.Use(gin.Recovery())
	router.GET("/snapshots/archive", controller.ArchiveSnapshots)
	return router, db, registry, root
}

// writeSnapshotFile 把一张"图"落到 registry 根目录下，返回库行要写的相对路径。
func writeSnapshotFile(t *testing.T, root, relPath string, payload []byte) string {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(relPath))
	require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
	require.NoError(t, os.WriteFile(full, payload, 0o644))
	return relPath
}

func archiveRequest(t *testing.T, router *gin.Engine, query string) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/snapshots/archive"+query, nil))
	return recorder
}

// readZipEntries 解出 zip 里的全部条目（名字 → 字节）。
func readZipEntries(t *testing.T, body []byte) (*zip.Reader, map[string][]byte) {
	t.Helper()
	reader, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	require.NoError(t, err, "响应体必须是合法 zip: %s", truncateForLog(body))
	out := make(map[string][]byte, len(reader.File))
	for _, file := range reader.File {
		rc, err := file.Open()
		require.NoError(t, err)
		content, err := io.ReadAll(rc)
		require.NoError(t, rc.Close())
		require.NoError(t, err)
		out[file.Name] = content
	}
	return reader, out
}

func truncateForLog(body []byte) string {
	if len(body) > 200 {
		return string(body[:200])
	}
	return string(body)
}

// TestArchiveSnapshotsPacksSelectedImagesIntoOneZip 是本次改造的**主锚点**：
// 选 3 张 → 一个 zip、3 个条目、字节逐张对得上、名字带通道标签与拍摄时刻。
//
// ⛔ 断言 `Flags` 的 UTF-8 位（0x800）：中文文件名不置这个位，Windows 资源管理器
// 解压出来是 GBK 乱码（Linux/macOS 上一切正常，所以本地测不出来）。
func TestArchiveSnapshotsPacksSelectedImagesIntoOneZip(t *testing.T) {
	router, db, _, root := snapshotArchiveFixture(t)
	const channelCode = "37010301021320000112"
	seedSnapshotChannel(t, db, "37010301021320000111", channelCode, 10)

	base := time.Date(2026, 9, 20, 14, 33, 10, 0, time.UTC)
	payloads := map[uint]string{
		seedSnapshotImage(t, db, channelCode, "a.jpg", base, gbmodels.SnapshotSourceDevice, func(row *gbmodels.GbChannelSnapshot) {
			row.RelPath = writeSnapshotFile(t, root, "gb-device-snapshots/s1/a.jpg", []byte("IMAGE-A"))
		}): "IMAGE-A",
		seedSnapshotImage(t, db, channelCode, "b.jpg", base.Add(time.Minute), gbmodels.SnapshotSourceDevice, func(row *gbmodels.GbChannelSnapshot) {
			row.RelPath = writeSnapshotFile(t, root, "gb-device-snapshots/s1/b.jpg", []byte("IMAGE-B"))
		}): "IMAGE-B",
		seedSnapshotImage(t, db, channelCode, "c.jpg", base.Add(2*time.Minute), gbmodels.SnapshotSourceZLM, func(row *gbmodels.GbChannelSnapshot) {
			row.RelPath = writeSnapshotFile(t, root, "gb-device-snapshots/s1/c.jpg", []byte("IMAGE-C"))
		}): "IMAGE-C",
	}
	ids := make([]string, 0, len(payloads))
	for id := range payloads {
		ids = append(ids, fmt.Sprint(id))
	}

	recorder := archiveRequest(t, router, "?ids="+strings.Join(ids, ","))
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.Equal(t, "application/zip", recorder.Header().Get("Content-Type"))
	// ⛔ 文件名要同时给 ASCII 与 RFC 5987 两份：只给中文名时老浏览器会退化成
	// URL 最后一段（乱码 + 丢扩展名）。
	disposition := recorder.Header().Get("Content-Disposition")
	require.Contains(t, disposition, "attachment;")
	require.Contains(t, disposition, ".zip")
	require.Contains(t, disposition, "filename*=UTF-8''")

	reader, entries := readZipEntries(t, recorder.Body.Bytes())
	require.Len(t, entries, 3, "三张图必须进同一个包")
	// 名字必须是「通道标签_拍摄时刻.jpg」而不是原始设备文件名（41 位标识人看不懂）。
	namePattern := regexp.MustCompile(`^通道-` + channelCode + `_\d{8}-\d{6}\.jpg$`)
	for _, file := range reader.File {
		require.Regexp(t, namePattern, file.Name, "zip 内文件名格式不对")
		require.NotEqual(t, 0, file.Flags&0x800, "中文名必须置 UTF-8 标志位（否则 Windows 解压乱码）")
	}
	// 内容逐张对上（不能拿错文件）。
	got := make([]string, 0, 3)
	for _, content := range entries {
		got = append(got, string(content))
	}
	require.ElementsMatch(t, []string{"IMAGE-A", "IMAGE-B", "IMAGE-C"}, got)
}

// TestArchiveSnapshotsSkipsMissingFilesInsteadOfFailingTheWholeArchive 磁盘上少几张时
// 必须**跳过并照常出包**，不能返回半截 zip，也不能整单失败。
//
// ⚠️ 这条与 TestArchiveSnapshotsRefusesWhenEveryFileIsGone 是一对：
// 少了**一部分**要能给（运营常态：手工清过目录）；少了**全部**不能给空包（用户会以为下载成功）。
func TestArchiveSnapshotsSkipsMissingFilesInsteadOfFailingTheWholeArchive(t *testing.T) {
	router, db, _, root := snapshotArchiveFixture(t)
	const channelCode = "37010301021320000212"
	seedSnapshotChannel(t, db, "37010301021320000211", channelCode, 10)
	base := time.Date(2026, 9, 20, 15, 0, 0, 0, time.UTC)

	present := seedSnapshotImage(t, db, channelCode, "keep.jpg", base, gbmodels.SnapshotSourceDevice,
		func(row *gbmodels.GbChannelSnapshot) {
			row.RelPath = writeSnapshotFile(t, root, "gb-device-snapshots/s2/keep.jpg", []byte("KEEP"))
		})
	// 库行在、文件不在。
	gone := seedSnapshotImage(t, db, channelCode, "gone.jpg", base.Add(time.Minute), gbmodels.SnapshotSourceDevice)

	recorder := archiveRequest(t, router, fmt.Sprintf("?ids=%d,%d", present, gone))
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	_, entries := readZipEntries(t, recorder.Body.Bytes())
	require.Len(t, entries, 1, "文件不在的那张应被跳过，而不是让整包失败")
	for _, content := range entries {
		require.Equal(t, "KEEP", string(content))
	}
}

// TestArchiveSnapshotsRefusesWhenEveryFileIsGone 一张都取不到时**不许**给空 zip。
//
// ⛔ 空 zip 的浏览器表现是"下载成功"，用户会以为图没了（而其实是文件被清了）——
// 这类"看起来成功"的失败最难排查。
func TestArchiveSnapshotsRefusesWhenEveryFileIsGone(t *testing.T) {
	router, db, _, _ := snapshotArchiveFixture(t)
	const channelCode = "37010301021320000312"
	seedSnapshotChannel(t, db, "37010301021320000311", channelCode, 10)
	id := seedSnapshotImage(t, db, channelCode, "gone.jpg", time.Now(), gbmodels.SnapshotSourceDevice)

	recorder := archiveRequest(t, router, fmt.Sprintf("?ids=%d", id))
	require.NotEqual(t, http.StatusOK, recorder.Code)
	require.NotEqual(t, "application/zip", recorder.Header().Get("Content-Type"),
		"不能把错误当成 zip 发出去（浏览器会存成一个打不开的文件）")
	require.Contains(t, recorder.Body.String(), "不在服务器上")
}

// TestArchiveSnapshotsPreflightReportsWhatWouldBePacked 预检只回 JSON，不打包。
//
// ⭐ 它存在的理由：`<a download>` 拿不到失败状态 —— 后端回 400 时浏览器会把那段
// JSON 当文件存下来。所以前端先问一句"这 N 张能打包吗"，确认后才真触发下载。
func TestArchiveSnapshotsPreflightReportsWhatWouldBePacked(t *testing.T) {
	router, db, _, root := snapshotArchiveFixture(t)
	const channelCode = "37010301021320000412"
	seedSnapshotChannel(t, db, "37010301021320000411", channelCode, 10)
	base := time.Date(2026, 9, 20, 16, 0, 0, 0, time.UTC)
	present := seedSnapshotImage(t, db, channelCode, "ok.jpg", base, gbmodels.SnapshotSourceDevice,
		func(row *gbmodels.GbChannelSnapshot) {
			row.RelPath = writeSnapshotFile(t, root, "gb-device-snapshots/s4/ok.jpg", []byte("0123456789"))
		})
	gone := seedSnapshotImage(t, db, channelCode, "gone.jpg", base, gbmodels.SnapshotSourceDevice)

	recorder := archiveRequest(t, router, fmt.Sprintf("?preflight=1&ids=%d,%d", present, gone))
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.NotEqual(t, "application/zip", recorder.Header().Get("Content-Type"))
	var body struct {
		Data struct {
			Count   int   `json:"count"`
			Skipped int   `json:"skipped"`
			Bytes   int64 `json:"bytes"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body), recorder.Body.String())
	require.Equal(t, 1, body.Data.Count, "预检必须报出可打包张数")
	require.Equal(t, 1, body.Data.Skipped, "文件不在的那张要计入 skipped，用户才知道少了")
	require.EqualValues(t, 10, body.Data.Bytes, "字节数取磁盘实际大小，不是库里的 size 列")
}

// TestArchiveSnapshotsRejectsBadRequests 参数与上限的拒绝分支。
func TestArchiveSnapshotsRejectsBadRequests(t *testing.T) {
	router, db, _, _ := snapshotArchiveFixture(t)

	t.Run("没选图", func(t *testing.T) {
		recorder := archiveRequest(t, router, "")
		require.NotEqual(t, http.StatusOK, recorder.Code)
		require.Contains(t, recorder.Body.String(), "请先选择")
	})

	t.Run("超过上限", func(t *testing.T) {
		// 不用造 200 行数据：超限判定发生在查库之前。
		ids := make([]string, 0, 201)
		for i := 1; i <= 201; i++ {
			ids = append(ids, fmt.Sprint(i))
		}
		recorder := archiveRequest(t, router, "?ids="+strings.Join(ids, ","))
		require.NotEqual(t, http.StatusOK, recorder.Code)
		require.Contains(t, recorder.Body.String(), "一次最多打包下载")
	})

	t.Run("非法 id 被忽略而不是整单报错", func(t *testing.T) {
		const channelCode = "37010301021320000512"
		seedSnapshotChannel(t, db, "37010301021320000511", channelCode, 10)
		id := seedSnapshotImage(t, db, channelCode, "x.jpg", time.Now(), gbmodels.SnapshotSourceDevice)
		// `abc` 与 `0` 都不是合法 id，只有最后那个是真的；文件不存在 ⇒ 走到"都不在服务器上"。
		recorder := archiveRequest(t, router, fmt.Sprintf("?ids=abc,0,%d", id))
		require.Contains(t, recorder.Body.String(), "不在服务器上",
			"非法值应被跳过，不能把整个请求判成参数错误")
	})
}

// TestArchiveSnapshotsRefusesPathTraversal 库里 rel_path 被篡改成 `../../` 时**必须**拒绝。
//
// ⛔ 这是打包下载最危险的入口：读接口最多泄漏一个文件，打包接口会把它**打进包里
// 主动送给调用方**。防线是 registry.FilePath 的前缀校验，本用例保证那条校验没被绕过。
func TestArchiveSnapshotsRefusesPathTraversal(t *testing.T) {
	router, db, _, root := snapshotArchiveFixture(t)
	const channelCode = "37010301021320000612"
	seedSnapshotChannel(t, db, "37010301021320000611", channelCode, 10)

	// 在 registry 根目录**之外**放一个"机密文件"。
	secret := filepath.Join(filepath.Dir(root), "secret-"+filepath.Base(root)+".txt")
	require.NoError(t, os.WriteFile(secret, []byte("TOP-SECRET"), 0o600))
	t.Cleanup(func() { _ = os.Remove(secret) })

	id := seedSnapshotImage(t, db, channelCode, "evil.jpg", time.Now(), gbmodels.SnapshotSourceDevice,
		func(row *gbmodels.GbChannelSnapshot) {
			row.RelPath = "../" + filepath.Base(secret)
		})

	recorder := archiveRequest(t, router, fmt.Sprintf("?ids=%d", id))
	// 打包项被跳过后没有任何可打包内容 ⇒ 拒绝出包。
	require.NotEqual(t, http.StatusOK, recorder.Code)
	require.NotContains(t, recorder.Body.String(), "TOP-SECRET")
	require.NotEqual(t, "application/zip", recorder.Header().Get("Content-Type"),
		"路径非法的条目不能被包进去")
}

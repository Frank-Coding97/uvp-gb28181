package upgrade

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// fakeClock 让节流窗口可测，避免用真实 sleep 把用例拖慢。
type fakeClock struct{ now time.Time }

func (c *fakeClock) Now() time.Time { return c.now }

func newTransferFixture(t *testing.T) (*ControlService, *fakeClock) {
	t.Helper()
	clock := &fakeClock{now: time.Date(2026, 11, 21, 8, 0, 0, 0, time.UTC)}
	service := &ControlService{Now: clock.Now}
	return service, clock
}

func TestTransferReporterPublishesFirstChunkAndThrottles(t *testing.T) {
	service, clock := newTransferFixture(t)
	reporter := service.beginTransfer("0123456789abcdef0123456789abcdef", 10<<20)
	require.NotNil(t, reporter)

	reporter.Add(64 << 10)
	first := service.TransferFor("0123456789abcdef0123456789abcdef")
	require.NotNil(t, first, "首个分片必须立刻可见，否则页面会白等一个节流窗口")
	require.EqualValues(t, 64<<10, first.DownloadedBytes)
	require.EqualValues(t, 10<<20, first.TotalBytes)

	// 时间没走、字节没到阈值 ⇒ 不重复发布，避免每 32KB 写一次状态。
	reporter.Add(64 << 10)
	require.EqualValues(t, 64<<10, service.TransferFor("0123456789abcdef0123456789abcdef").DownloadedBytes)

	clock.now = clock.now.Add(transferReportInterval)
	reporter.Add(64 << 10)
	require.EqualValues(t, 192<<10, service.TransferFor("0123456789abcdef0123456789abcdef").DownloadedBytes)

	// 大块直接顶到阈值也要发布，即使时间没到。
	clock.now = clock.now.Add(time.Millisecond)
	reporter.Add(transferReportBytes)
	require.EqualValues(t, 192<<10+transferReportBytes, service.TransferFor("0123456789abcdef0123456789abcdef").DownloadedBytes)
}

func TestTransferReporterFinishPublishesTailThenClears(t *testing.T) {
	service, _ := newTransferFixture(t)
	reporter := service.beginTransfer("0123456789abcdef0123456789abcdef", 4<<20)
	reporter.Add(1 << 20)
	reporter.Add(3 << 20) // 落在节流窗口内，不会立刻发布

	reporter.Finish()
	require.Nil(t, service.TransferFor("0123456789abcdef0123456789abcdef"), "下载结束后必须清空，避免把满格条留给校验/准备阶段")

	// Finish 幂等：重复调用不 panic、也不会复活快照。
	reporter.Finish()
	require.Nil(t, service.TransferFor("0123456789abcdef0123456789abcdef"))
}

func TestTransferForIgnoresOtherTasksAndEmptyIDs(t *testing.T) {
	service, _ := newTransferFixture(t)
	reporter := service.beginTransfer("0123456789abcdef0123456789abcdef", 1<<20)
	reporter.Add(1024)

	require.Nil(t, service.TransferFor("ffffffffffffffffffffffffffffffff"), "别的任务不能继承这条进度")
	require.Nil(t, service.TransferFor(""))
	require.Nil(t, (&ControlService{}).TransferFor("0123456789abcdef0123456789abcdef"))

	// 没有 taskID 的下载（自动准备路径）不产生快照。
	require.Nil(t, service.beginTransfer("", 1<<20))
	require.Nil(t, service.beginTransfer("0123456789abcdef0123456789abcdef", 0))
}

func TestTransferWriterCountsOnlyWrittenBytes(t *testing.T) {
	service, _ := newTransferFixture(t)
	reporter := service.beginTransfer("0123456789abcdef0123456789abcdef", 1<<20)
	writer := &transferWriter{dst: &shortWriter{limit: 10}, reporter: reporter}

	n, err := writer.Write(make([]byte, 32))
	require.NoError(t, err)
	require.Equal(t, 10, n)
	require.EqualValues(t, 10, service.TransferFor("0123456789abcdef0123456789abcdef").DownloadedBytes,
		"只按真实落盘的字节计数，短写不能虚报进度")
}

type shortWriter struct{ limit int }

func (w *shortWriter) Write(p []byte) (int, error) {
	if len(p) > w.limit {
		return w.limit, nil
	}
	return len(p), nil
}

// 端到端口径：下载过程中 Status 能读到在涨的字节数，下载结束后立刻清空。
// 这条用例直接走 downloadArtifact，确保「报进度」接在真实写入路径上，
// 而不是只测一个孤立的计数器。
func TestDownloadArtifactReportsGrowingTransferThenClears(t *testing.T) {
	payload := bytes.Repeat([]byte("x"), 96<<10)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
		w.WriteHeader(http.StatusOK)
		for offset := 0; offset < len(payload); offset += 16 << 10 {
			end := offset + (16 << 10)
			if end > len(payload) {
				end = len(payload)
			}
			_, _ = w.Write(payload[offset:end])
			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}
			time.Sleep(60 * time.Millisecond)
		}
	}))
	t.Cleanup(server.Close)

	sum := sha256.Sum256(payload)
	artifact := Artifact{
		Deployment: "standalone", OS: "linux", Arch: "amd64", Filename: "uvp.tar.gz",
		URL: server.URL + "/uvp.tar.gz", Size: int64(len(payload)), SHA256: hex.EncodeToString(sum[:]),
	}
	taskID := strings.Repeat("a", 32)
	service := &ControlService{StagingRoot: t.TempDir(), HTTPClient: server.Client()}

	finished := make(chan error, 1)
	go func() {
		finished <- service.downloadArtifact(context.Background(), taskID, Manifest{Envelope: []byte("{}")}, artifact)
	}()

	var observed *TaskTransferProgress
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if progress := service.TransferFor(taskID); progress != nil {
			observed = progress
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	require.NotNil(t, observed, "下载期间必须能看到进度，否则页面依旧是死的")
	require.Equal(t, taskID, observed.TaskID)
	require.EqualValues(t, len(payload), observed.TotalBytes)
	require.Positive(t, observed.DownloadedBytes)
	require.LessOrEqual(t, observed.DownloadedBytes, int64(len(payload)))

	require.NoError(t, <-finished)
	require.Nil(t, service.TransferFor(taskID), "下载完成后进度必须让位给校验/准备阶段")
}

package recordcache

import (
	"context"
	"errors"
	"net/http"

	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
	gbrecording "uvplatform.com/uvp-gb28181/app/gb28181/recording"
)

var (
	// ErrDownloadUnavailable 票据下载未装配（对象存储/装配失败），入口应回 503。
	ErrDownloadUnavailable = errors.New("record cache download unavailable")
	// ErrTaskNotReady 任务还在收尾整理，产物尚未就绪。
	//
	// ⛔ 必须单独区分出来，不能并进 ErrNotDownloadable：文件明明在、
	// 只是几十秒后才能一次给全，报成"暂无可下载的文件"会让用户以为录像丢了。
	ErrTaskNotReady = errors.New("record cache task is not ready for download")
	// ErrDownloadTicketInvalid 票据无效 / 过期 / 已被认领 / 超出并发预算。
	ErrDownloadTicketInvalid = errors.New("record cache download ticket invalid")
)

// WholeTaskIndex 表示「整段录像」这个下载目标。
//
// 源文件多于一个时它就是收尾期合好的那个文件；只有一个源文件时就是它本身。
// ⛔ 它只是服务内部把两条入口（整段 / 指定分片）收敛到同一个 index 参数的写法，
// 不体现在 HTTP 地址上：整段走不带序号的地址，指定分片走 `…/segments/<n>/content`。
const WholeTaskIndex = -1

// CreateDownload 为某个任务的产出建一次「票据下载」。
//
// 返回的一次性票据由控制器种进 HttpOnly cookie；真正的下载走**免鉴权**路由，
// 因为浏览器自己发起的下载带不了 JWT 头 —— 这也是改造前只能退化成
// "整包收进内存 Blob" 的唯一原因。
//
// index 取 WholeTaskIndex 表示整段，否则是分片序号。
func (s *Service) CreateDownload(ctx context.Context, userID uint, taskID string, scope Scope, index int) (gbrecording.DownloadTaskView, string, error) {
	if s == nil || s.downloads == nil {
		return gbrecording.DownloadTaskView{}, "", ErrDownloadUnavailable
	}
	task, err := s.repo.FindByTaskID(ctx, taskID, scope)
	if err != nil {
		return gbrecording.DownloadTaskView{}, "", err
	}
	if err := ensureDownloadable(task); err != nil {
		return gbrecording.DownloadTaskView{}, "", err
	}
	// ⛔ 越界序号在**签发**这一步就要挡住，不能等认领时再说：
	// 签发是带 JWT 的正常请求，前端能拿到干净的 404 并给出提示；
	// 认领是浏览器自己发的下载，那时候报错就只剩下载栏里一条失败。
	if err := ensureDownloadableIndex(task, index); err != nil {
		return gbrecording.DownloadTaskView{}, "", err
	}
	return s.downloads.Create(userID, task.TaskID)
}

// ensureDownloadable 判定"现在能不能给这个任务下载"。
func ensureDownloadable(task *gbmodels.GbRecordCacheTask) error {
	if task == nil {
		return ErrNotDownloadable
	}
	// ⛔ 看的是**源文件总数**而不是分片数：一片可能由多次补拉拼成，
	// 分片数 0 与"没有文件"并不等价（反过来，片级三元组为空的老数据
	// 仍能通过 Sources() 回退成单文件一片）。
	if task.SourceFileCount() == 0 {
		return ErrNotDownloadable
	}
	// ⛔ 整理中不给下载：这几十秒里产物还没落地，放行只会让用户拿到的
	// 第一次下载退化成"现场拼接"（进度长时间为 0），正是这次改造要消灭的体验。
	if task.State == gbmodels.RecordCacheStateMerging {
		return ErrTaskNotReady
	}
	return nil
}

// ensureDownloadableIndex 校验"要下的那一段"确实存在。
func ensureDownloadableIndex(task *gbmodels.GbRecordCacheTask, index int) error {
	if index == WholeTaskIndex {
		return nil
	}
	segments := task.DecodeSegments()
	if index < 0 || index >= len(segments) {
		return ErrNotDownloadable
	}
	// 片内任一源文件都可用即可（片级 Path 已经过时：它只是第一个源文件的副本）。
	if !downloadableSegment(segments[index]) {
		return ErrNotDownloadable
	}
	return nil
}

// ClaimDownload 认领票据并把文件流给 writer（单次消费）。
//
// onClaimed 在认领成功后立刻回调，供控制器清掉 cookie —— 票据一旦用过就作废，
// 不能留在浏览器里被重放。
func (s *Service) ClaimDownload(ctx context.Context, writer http.ResponseWriter, downloadID, ticket string, index int, rangeHeader string, onClaimed func()) error {
	if s == nil || s.downloads == nil {
		return ErrDownloadUnavailable
	}
	view, taskCtx, _, err := s.downloads.Claim(downloadID, ticket)
	if err != nil {
		return ErrDownloadTicketInvalid
	}
	if onClaimed != nil {
		onClaimed()
	}
	// 浏览器关掉下载/取消时，注册表会取消这条下载任务的 ctx；
	// 把它接到转发上下文上，别让后端抱着一条没人要的连接继续拉流。
	transferCtx, transferCancel := context.WithCancel(ctx)
	defer transferCancel()
	go func() {
		select {
		case <-taskCtx.Done():
			transferCancel()
		case <-transferCtx.Done():
		}
	}()

	finish := func(status, code string) { s.downloads.Finish(downloadID, status, code) }

	task, err := s.repo.FindByTaskID(transferCtx, view.FileID, nil)
	if err != nil {
		finish(gbrecording.DownloadStatusFailed, "unavailable")
		return err
	}
	if err := ensureDownloadable(task); err != nil {
		finish(gbrecording.DownloadStatusFailed, "unavailable")
		return err
	}
	if err := ensureDownloadableIndex(task, index); err != nil {
		finish(gbrecording.DownloadStatusFailed, "unavailable")
		return err
	}
	counted := &countingWriter{ResponseWriter: writer, onWrite: func(written uint64) {
		s.downloads.AddBytes(downloadID, written)
	}}
	if size := s.downloadableSize(task, index); size > 0 {
		s.downloads.SetTotal(downloadID, size)
	}
	if err := s.serveTaskContent(transferCtx, counted, task, index, rangeHeader); err != nil {
		if errors.Is(err, context.Canceled) {
			finish(gbrecording.DownloadStatusCancelled, "cancelled")
		} else {
			finish(gbrecording.DownloadStatusFailed, "transfer_failed")
		}
		return err
	}
	finish(gbrecording.DownloadStatusCompleted, "")
	return nil
}

// DownloadStatus / CancelDownload 供"下载中心"查询与取消（按 owner 校验）。
func (s *Service) DownloadStatus(downloadID string, userID uint) (gbrecording.DownloadTaskView, error) {
	if s == nil || s.downloads == nil {
		return gbrecording.DownloadTaskView{}, ErrDownloadUnavailable
	}
	view, err := s.downloads.Get(downloadID, userID)
	if err != nil {
		return gbrecording.DownloadTaskView{}, ErrDownloadTicketInvalid
	}
	return view, nil
}

func (s *Service) CancelDownload(downloadID string, userID uint) (gbrecording.DownloadTaskView, error) {
	if s == nil || s.downloads == nil {
		return gbrecording.DownloadTaskView{}, ErrDownloadUnavailable
	}
	view, err := s.downloads.Cancel(downloadID, userID)
	if err != nil {
		return gbrecording.DownloadTaskView{}, ErrDownloadTicketInvalid
	}
	return view, nil
}

// downloadableSize 是这次下载能报给下载中心的字节数（拿不到就 0）。
//
// ⛔ 按 index 分别算：整段是多片之和，按段下载时只该报那一段 ——
// 报成整段大小会让下载中心永远显示 30%，看起来像"卡住了"。
//
// ⛔ 整段且产物就绪时按**产物自身的真实大小**报：各源文件之和比合并产物略大
// （合并会去掉后续分段的文件头尾），按源文件之和报会让进度条永远差最后一点点。
func (s *Service) downloadableSize(task *gbmodels.GbRecordCacheTask, index int) uint64 {
	segments := task.DecodeSegments()
	if index != WholeTaskIndex {
		if index < 0 || index >= len(segments) || segments[index].Size <= 0 {
			return 0
		}
		return uint64(segments[index].Size)
	}
	if _, size, ok := s.mergedArtifact(task); ok && size > 0 {
		return uint64(size)
	}
	if len(segments) == 1 {
		if segments[0].Size > 0 {
			return uint64(segments[0].Size)
		}
		return 0
	}
	var total uint64
	for _, segment := range segments {
		if segment.Size <= 0 {
			return 0
		}
		total += uint64(segment.Size)
	}
	return total
}

// countingWriter 统计实际写出的字节数，喂给下载任务做进度/速率。
//
// ⛔ 必须实现 Unwrap：http.ResponseController 靠它下钻到真正的
// ResponseWriter 去设写超时，包一层却不暴露底层会让"写超时"静默失效。
type countingWriter struct {
	http.ResponseWriter
	onWrite func(uint64)
}

func (w *countingWriter) Write(data []byte) (int, error) {
	written, err := w.ResponseWriter.Write(data)
	if written > 0 && w.onWrite != nil {
		w.onWrite(uint64(written))
	}
	return written, err
}

func (w *countingWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

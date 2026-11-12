package recordcache

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
)

// defaultMergedDirName 是「已合并产物」的默认目录名（挂在系统临时目录下）。
//
// 背景：多分片任务在**收尾时**就把各片拼成一个文件，下载时才不用先干等一段
// 现场拼接 —— 浏览器拿到地址就能立刻开始（原生下载），进度条反映的是真实网速。
//
// ⛔ 为什么允许它随重启/系统清理丢失：产物是**可重建的派生物**。分片文件仍在
// 媒体节点上，产物没了顶多退回「下载时现场合并」（改造前的行为），不会丢数据。
// 所以这里刻意不做持久化保证、也不进数据库 —— 用两个新列换来的只是「重启后
// 少合并一次」，代价却是一次 schema 迁移 + 更新列契约测试，不划算。
const defaultMergedDirName = "uvp-record-cache-merged"

// mergedStagingSuffix 是产物落地时的临时后缀。
//
// ⛔ 必须先写临时名再 rename：下载侧靠「目标文件存在」判定产物就绪，
// 直接写目标名会让半成品被当成成品发出去（用户拿到一个截断的录像，
// 而且播得动，是最难发现的那类错）。
const mergedStagingSuffix = ".staging"

// mergeWorkPattern 是单次合并的工作目录前缀（拉回来的分片放这里）。
const mergeWorkPattern = ".work-"

// mergedDir 返回产物落盘目录（Options.MergedDir 可覆盖，主要给测试用）。
func (s *Service) mergedDir() string {
	if dir := strings.TrimSpace(s.mergedDirPath); dir != "" {
		return dir
	}
	return filepath.Join(os.TempDir(), defaultMergedDirName)
}

// mergedArtifactPath 是产物在本地磁盘上的固定位置（按任务号命名）。
func (s *Service) mergedArtifactPath(taskID string) string {
	return filepath.Join(s.mergedDir(), strings.TrimSpace(taskID)+".mp4")
}

// mergedArtifact 返回**已就绪**的合并产物；不存在或大小为 0 时 ok=false。
//
// ⛔ 这是下载热路径上的调用，只做一次 Stat：不能在这里触发任何耗时动作，
// 否则"点下载立即开始"就退化成"点下载先等一次拼接"。
func (s *Service) mergedArtifact(task *gbmodels.GbRecordCacheTask) (string, int64, bool) {
	if task == nil || strings.TrimSpace(task.TaskID) == "" {
		return "", 0, false
	}
	path := s.mergedArtifactPath(task.TaskID)
	info, err := os.Stat(path)
	if err != nil || info.IsDir() || info.Size() <= 0 {
		return "", 0, false
	}
	return path, info.Size(), true
}

// ensureMergedArtifact 把全部源文件合成一个文件放进产物目录（幂等：已就绪直接返回）。
//
// ⛔ 判据是**源文件总数 < 2** 才跳过，不是"分片数 < 2"：补拉收进同一片之后，
// 30 分钟录像的常见形态是「1 片 3 个源文件」——按分片数判会直接返回、不合成，
// 用户下到的只有第一段。
func (s *Service) ensureMergedArtifact(ctx context.Context, task *gbmodels.GbRecordCacheTask) error {
	if task.SourceFileCount() < 2 {
		return nil
	}
	if _, _, ok := s.mergedArtifact(task); ok {
		return nil
	}
	segments := task.DecodeSegments()
	if total := totalSegmentSize(segments); total > mergeTotalSizeLimit {
		// ⛔ 超上限不合成，但**不是任务失败**：分段文件本身是好的，
		// 用户仍能按分段一个个下（下载接口会回 ErrMergeFailed 并说明原因）。
		return fmt.Errorf("%w: 分段合计 %d 字节超过自动合并上限", ErrMergeFailed, total)
	}
	recorder, err := s.recorderFor(task.NodeID)
	if err != nil {
		return err
	}
	dir := s.mergedDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("%w: %v", ErrMergeFailed, err)
	}
	work, err := os.MkdirTemp(dir, mergeWorkPattern)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrMergeFailed, err)
	}
	defer os.RemoveAll(work)

	dest := s.mergedArtifactPath(task.TaskID)
	staging := dest + mergedStagingSuffix
	if err := s.fetchAndConcat(ctx, recorder, sourcesOf(segments), work, staging); err != nil {
		_ = os.Remove(staging)
		return err
	}
	if err := os.Rename(staging, dest); err != nil {
		_ = os.Remove(staging)
		return fmt.Errorf("%w: %v", ErrMergeFailed, err)
	}
	return nil
}

// removeMergedArtifact 删除合并产物（任务删除 / 保留期清理时调用）。
// 尽力而为：删不掉只会在临时目录里留一份，不影响任务本身的正确性。
func (s *Service) removeMergedArtifact(taskID string) {
	if strings.TrimSpace(taskID) == "" {
		return
	}
	_ = os.Remove(s.mergedArtifactPath(taskID))
	// 残留的中间态一并清掉（上一次合并崩在 rename 之前会留下它）。
	_ = os.Remove(s.mergedArtifactPath(taskID) + mergedStagingSuffix)
}

package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"uvplatform.com/uvp-gb28181/app/models"
)

// uploadIDPattern 分片上传会话 ID 的合法形态。
//
//	生成方（InitChunkUpload）：fmt.Sprintf("upload_%d_%s", unix, uuid.New().String()[:8])
//
// ⛔⛔ 为什么必须校验格式：这个值直接参与 `filepath.Join(localPath, "tmp", uploadID)`，
//
//	还参与 `os.RemoveAll`。不校验时 `../../..` 会被 Clean 成**安装根目录**，
//	取消上传就等于把整个安装目录删掉（不是"删掉上传目录外的一个子目录"）。
//	前端只回传服务端下发的 ID，所以收紧格式不会影响正常上传/续传。
var uploadIDPattern = regexp.MustCompile(`^upload_[0-9]{10,19}_[0-9a-f]{8}$`)

// validateUploadID 校验分片会话 ID 的形态。
func validateUploadID(uploadID string) error {
	if !uploadIDPattern.MatchString(uploadID) {
		return fmt.Errorf("非法的上传ID")
	}
	return nil
}

// ensurePathWithin 断言 target 规范化后仍位于 root 之内。
//
// ⛔ 只靠正则还不够：正则挡的是「已知的坏字符」，而路径归一化会因为
//
//	符号链接、大小写、重复分隔符产生意外结果。两道路闸都要有，且这里必须用
//	filepath.Abs + filepath.Rel 判定，不能用 strings.HasPrefix ——
//	后者会把 `/tmp/evil-tmp` 判成在 `/tmp` 之内。
func ensurePathWithin(root, target string) error {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return fmt.Errorf("解析上传根目录失败: %w", err)
	}
	absTarget, err := filepath.Abs(target)
	if err != nil {
		return fmt.Errorf("解析上传路径失败: %w", err)
	}
	rel, err := filepath.Rel(absRoot, absTarget)
	if err != nil {
		return fmt.Errorf("非法的上传路径")
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return fmt.Errorf("非法的上传路径")
	}
	return nil
}

// chunkTmpDir 返回（并校验）某个上传会话的临时分片目录。
//
// 校验两件事：① uploadID 形态合法；② 拼接结果没有跳出 `<localPath>/tmp`。
func chunkTmpDir(localPath, uploadID string) (string, error) {
	if err := validateUploadID(uploadID); err != nil {
		return "", err
	}
	tmpRoot := filepath.Join(localPath, "tmp")
	tmpDir := filepath.Join(tmpRoot, uploadID)
	if err := ensurePathWithin(tmpRoot, tmpDir); err != nil {
		return "", err
	}
	return tmpDir, nil
}

// ensureChunkSessionOwner 确认分片会话属于当前用户。
//
// ⛔ 会话里**还没有任何分片记录**时放行：首片上传前不可能有"归属"，
//
//	而 uploadID 由服务端生成（unix 秒 + uuid 前 8 位），猜中别人的空闲会话
//	不构成可利用面。一旦有记录，就必须是同一个 created_by。
func ensureChunkSessionOwner(ctx context.Context, uploadID string, userID uint) error {
	if err := validateUploadID(uploadID); err != nil {
		return err
	}
	ownerID, found, err := models.GetChunkOwnerID(ctx, uploadID)
	if err != nil {
		return fmt.Errorf("查询上传会话失败: %v", err)
	}
	if found && ownerID != userID {
		// 不区分「不存在」与「不属于你」：两种结论都不该让调用方探测会话是否存在
		return fmt.Errorf("上传会话不存在或已过期")
	}
	return nil
}

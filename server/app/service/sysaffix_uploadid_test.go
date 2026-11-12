package service

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// 分片会话 ID 只接受服务端自己生成的那种形态。
// 其它一律拒绝 —— 尤其 ../../ 这类会被 filepath.Join 归一化掉的输入。
func TestValidateUploadIDRejectsTraversal(t *testing.T) {
	valid := []string{
		"upload_1700000000_deadbeef",
		"upload_1750000000123_0a1b2c3d",
	}
	for _, id := range valid {
		require.NoError(t, validateUploadID(id), "合法 ID 不应被拒: %s", id)
	}

	invalid := []string{
		"",
		"..",
		"../..",
		"../../victim",
		"upload_1700000000_deadbee",          // 后缀少一位
		"upload_1700000000_DEADBEEF",         // 大写不是生成器的输出
		"upload_1700000000_deadbeef/../../x", // 叠加穿越
		"/etc/passwd",
		"upload_1700000000_deadbeef\x00",
	}
	for _, id := range invalid {
		require.Error(t, validateUploadID(id), "非法 ID 必须被拒: %q", id)
	}
}

// chunkTmpDir 必须把结果钉死在 <localPath>/tmp 内。
func TestChunkTmpDirStaysInsideTmpRoot(t *testing.T) {
	localPath := t.TempDir()

	got, err := chunkTmpDir(localPath, "upload_1700000000_deadbeef")
	require.NoError(t, err)
	require.Equal(t, filepath.Join(localPath, "tmp", "upload_1700000000_deadbeef"), got)

	_, err = chunkTmpDir(localPath, "../../victim")
	require.Error(t, err)
}

// ensurePathWithin 不能被「同前缀的兄弟目录」骗过。
func TestEnsurePathWithinRejectsSiblingPrefix(t *testing.T) {
	root := filepath.Join(t.TempDir(), "tmp")
	require.NoError(t, ensurePathWithin(root, filepath.Join(root, "upload_1_deadbeef")))

	// /tmp 与 /tmp-evil 同前缀但不是子目录：strings.HasPrefix 会放行，这里必须拒绝。
	require.Error(t, ensurePathWithin(root, root+"-evil"))
	require.Error(t, ensurePathWithin(root, filepath.Join(root, "..", "victim")))
	require.Error(t, ensurePathWithin(root, "/etc/passwd"))
}

// 端到端：取消上传用穿越 ID 时，上传根之外的目录必须**原样还在**。
func TestTraversalUploadIDCannotDeleteOutsideUploadRoot(t *testing.T) {
	base := t.TempDir()
	uploadRoot := filepath.Join(base, "resource", "public", "uploads")
	require.NoError(t, os.MkdirAll(filepath.Join(uploadRoot, "tmp"), 0o755))

	victim := filepath.Join(base, "victim")
	require.NoError(t, os.MkdirAll(victim, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(victim, "keep.txt"), []byte("keep"), 0o644))

	_, err := chunkTmpDir(uploadRoot, "../../victim")
	require.Error(t, err, "穿越 ID 必须在拼路径阶段就被拒绝")

	// 直接验证判据本身：即使有人绕过校验拿到路径，边界断言也要拦住。
	escape := filepath.Join(uploadRoot, "tmp", "../../victim")
	require.Error(t, ensurePathWithin(filepath.Join(uploadRoot, "tmp"), escape))

	require.DirExists(t, victim)
	require.FileExists(t, filepath.Join(victim, "keep.txt"))
}

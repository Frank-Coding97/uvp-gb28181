package upgrade

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// 升级助手（uvp-updater）住在**安装根**，不随 release 目录切换而更新：
// ctl.sh 里写死 `UPDATER_BIN="$ROOT/updater/uvp-updater"`，Windows 侧同理。
//
// 后果：助手自身的任何改进（例如 1.1.16 才加入的后台定时探测）都到不了已经在
// 运行的现场机 —— 业务升到新版本，助手还是几周前编译的那一个。2026-11-04 线上
// 实测就是这个状态：业务 1.1.15、助手还是 10-27 的二进制，里面根本没有
// `update-check.json` 这段代码，于是侧边栏永远不会出现「发现新版本」。
//
// 这里统一成「旁挂 + 启动时替换」两段式，两个平台语义一致：
//  1. 升级激活时把 release 自带的助手写成 `<image>.new`（本文件）；
//  2. 控制脚本或升级收尾的重启动作把它换上（见 helper_sync_unix.go / ctl 脚本）。
//
// ⛔ 不要在助手运行期间就地覆盖：Linux 会换掉正在执行的 inode（旧进程继续跑旧
// 代码，看着"换了却没生效"），Windows 直接锁死文件报 sharing violation。
const (
	// HelperEntryDir 是助手在安装根与 release 内的固定子目录。
	HelperEntryDir = "updater"
	// HelperUnixImage / HelperWindowsImage 是各平台的助手文件名。
	HelperUnixImage    = "uvp-updater"
	HelperWindowsImage = "uvp-updater.exe"
	// HelperStagedSuffix 是「已就位、等待替换」的旁挂后缀。
	HelperStagedSuffix = ".new"
	// HelperSystemdUnit 是「助手被装成独立 systemd 单元」时的单元名。⛔ 必须与
	// deploy/standalone/uvp-gb28181-ctl.sh 的 UPDATER_UNIT_NAME 逐字一致（契约
	// 测试钉住）：两处不同名会让重启落空，助手永远是旧的那个。
	HelperSystemdUnit = "uvp-gb28181-updater.service"
	// helperImageMaxBytes 给助手体积封顶，避免畸形包把安装盘写满。
	helperImageMaxBytes = 128 << 20
)

// HelperSyncResult 是一次助手同步的结果。Reason 只用于事件日志与排障，
// 不影响升级任务自身的成败：业务版本已经切过去并自检通过了。
type HelperSyncResult struct {
	// Staged 表示本次从 release 里取到了助手镜像并落成旁挂文件。
	Staged bool
	// RestartScheduled 表示已排程一次助手重启，重启后新镜像即生效。立刻生效只在
	// 能安全重启助手的平台上成立；Windows 交给控制脚本在下次启动时替换。
	RestartScheduled bool
	// Reason 说明「没做」的原因，便于现场对账。
	Reason string
}

// helperSynchronizer 是部署适配器的可选能力。workflow 在任务已经持久化为
// Succeeded 之后才调用它，因此这里返回的任何错误都只能记事件，不能反过来
// 把一次成功的升级标成失败。
type helperSynchronizer interface {
	SyncHelper(context.Context, TaskRecord) (HelperSyncResult, error)
}

// stageHelperImage copies the helper binary shipped inside a candidate release
// to <installRoot>/updater/<image>.new. A release without a helper is not an
// error: custom or older packages simply keep the installed one.
func stageHelperImage(releaseRoot, installRoot, image string) (bool, error) {
	releaseRoot = strings.TrimSpace(releaseRoot)
	installRoot = strings.TrimSpace(installRoot)
	image = strings.TrimSpace(image)
	if releaseRoot == "" || installRoot == "" || image == "" {
		return false, errors.New("helper staging needs release root, installation root and image name")
	}
	if filepath.Base(image) != image {
		return false, errors.New("helper image name must be a bare file name")
	}
	source := filepath.Join(releaseRoot, HelperEntryDir, image)
	if !isWithin(releaseRoot, source) {
		return false, errors.New("candidate helper path escapes the release")
	}
	info, err := os.Lstat(source)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect candidate helper: %w", err)
	}
	// ⛔ 必须是普通文件：包里若把助手做成符号链接，等于让「以特权身份运行的东西」
	//   指向任意路径，必须在落盘前拒绝。
	if !info.Mode().IsRegular() {
		return false, errors.New("candidate helper is not a regular file")
	}
	if info.Size() == 0 || info.Size() > helperImageMaxBytes {
		return false, fmt.Errorf("candidate helper size %d is out of range", info.Size())
	}
	destination := filepath.Join(installRoot, HelperEntryDir, image+HelperStagedSuffix)
	if !isWithin(installRoot, destination) {
		return false, errors.New("staged helper path escapes the installation root")
	}
	same, err := filesHaveSameContent(source, destination)
	if err != nil {
		return false, err
	}
	if same {
		// 已就位同一份镜像：保留原旁挂文件，避免每次重试都重写一遍。
		return false, nil
	}
	if err := copyFileAtomically(source, destination, 0o755); err != nil {
		return false, fmt.Errorf("stage helper image: %w", err)
	}
	return true, nil
}

func filesHaveSameContent(left, right string) (bool, error) {
	leftInfo, err := os.Stat(left)
	if err != nil {
		return false, fmt.Errorf("inspect helper source: %w", err)
	}
	rightInfo, err := os.Stat(right)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect staged helper: %w", err)
	}
	if rightInfo.IsDir() || leftInfo.Size() != rightInfo.Size() {
		return false, nil
	}
	leftDigest, err := fileSHA256(left)
	if err != nil {
		return false, err
	}
	rightDigest, err := fileSHA256(right)
	if err != nil {
		return false, err
	}
	return leftDigest == rightDigest, nil
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path) // #nosec G304 -- caller passes a path validated by isWithin.
	if err != nil {
		return "", fmt.Errorf("open %s: %w", filepath.Base(path), err)
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", fmt.Errorf("hash %s: %w", filepath.Base(path), err)
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

// copyFileAtomically writes destination via a temporary sibling and a rename so
// a crash can never leave a half-written helper behind.
func copyFileAtomically(source, destination string, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return err
	}
	input, err := os.Open(source) // #nosec G304 -- path validated by the caller.
	if err != nil {
		return err
	}
	defer input.Close()
	temporary := destination + ".tmp"
	output, err := os.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(output, input); err != nil {
		_ = output.Close()
		_ = os.Remove(temporary)
		return err
	}
	if err := output.Sync(); err != nil {
		_ = output.Close()
		_ = os.Remove(temporary)
		return err
	}
	if err := output.Close(); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	if err := os.Rename(temporary, destination); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	return syncDirectory(filepath.Dir(destination))
}

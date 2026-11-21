package httpsconfig

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Start returns before changing listeners so the browser can retain the new
// address. The operation is bounded and independent of the HTTP request.
func (m *Manager) Start(req Request) (Result, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.busy {
		return Result{}, errors.New("已有 HTTPS 配置操作正在执行，请稍后重试")
	}
	if err := m.Supported(); err != nil {
		return Result{}, err
	}
	if _, err := os.Stat(m.recoveryPath()); err == nil {
		return Result{}, errors.New("上次 HTTPS 操作存在待检查的恢复材料，请先检查 data/https/recovery.json 及服务入口")
	} else if !os.IsNotExist(err) {
		return Result{}, err
	}
	if b, err := os.ReadFile(m.operationPath()); err == nil {
		var previous Result
		if json.Unmarshal(b, &previous) != nil || previous.State == "running" || previous.State == "recovery_required" {
			return Result{}, errors.New("上次 HTTPS 操作被中断或尚未恢复，请先检查 data/https 下的操作记录及服务入口")
		}
	} else if !os.IsNotExist(err) {
		return Result{}, err
	}
	if _, err := RenderFile(m, req.Settings); err != nil {
		return Result{}, err
	}
	if len(req.Certificate) > 0 || len(req.PrivateKey) > 0 {
		if _, err := ValidateCertificate(req.Certificate, req.PrivateKey); err != nil {
			return Result{}, err
		}
	} else if req.SyncZLM {
		return Result{}, errors.New("同步到 ZLMediaKit 需要同时上传新证书与私钥")
	}
	id := make([]byte, 16)
	if _, err := rand.Read(id); err != nil {
		return Result{}, err
	}
	op := Result{ID: hex.EncodeToString(id), State: "running", Nginx: "pending", ZLM: "unchanged", UpdatedAt: time.Now()}
	if err := writeJSON(m.operationPath(), op); err != nil {
		return Result{}, fmt.Errorf("无法保存操作记录: %w", err)
	}
	m.last = nil
	m.busy = true
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		result := m.apply(ctx, req)
		result.ID = op.ID
		result.UpdatedAt = time.Now()
		m.mu.Lock()
		defer m.mu.Unlock()
		if err := writeJSON(m.operationPath(), result); err != nil {
			// Keep a visible terminal error in memory even when storage is full.
			result.State = "recovery_required"
			result.Error += "；操作记录写入失败，请检查磁盘和权限"
		}
		m.last = &result
		m.busy = false
	}()
	return op, nil
}
func RenderFile(m *Manager, s Settings) (string, error) {
	b, err := os.ReadFile(m.basePath())
	if err != nil {
		return "", err
	}
	return Render(string(b), s)
}

type snapshot struct {
	path   string
	data   []byte
	mode   os.FileMode
	exists bool
}

func capture(paths ...string) ([]snapshot, error) {
	var out []snapshot
	for _, path := range paths {
		info, err := os.Lstat(path)
		if os.IsNotExist(err) {
			out = append(out, snapshot{path: path})
			continue
		}
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() {
			return nil, errors.New("配置或证书文件不是普通文件")
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		out = append(out, snapshot{path, b, info.Mode().Perm(), true})
	}
	return out, nil
}
func restore(files []snapshot) error {
	var failures []error
	for _, file := range files {
		var err error
		if file.exists {
			err = AtomicWrite(file.path, file.data, file.mode)
		} else {
			err = os.Remove(file.path)
			if os.IsNotExist(err) {
				err = nil
			}
		}
		if err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

// Recovery material is private and retained if the process stops mid-apply.
// Paths are relative to the fixed installation root for manual recovery.
func (m *Manager) saveRecovery(nginx, zlm []snapshot) error {
	type file struct {
		Path   string `json:"path"`
		Data   []byte `json:"data"`
		Mode   uint32 `json:"mode"`
		Exists bool   `json:"exists"`
	}
	backup := map[string][]file{}
	for name, snapshots := range map[string][]snapshot{"nginx": nginx, "zlm": zlm} {
		for _, s := range snapshots {
			relative, err := filepath.Rel(m.root, s.path)
			if err != nil {
				return err
			}
			backup[name] = append(backup[name], file{relative, s.data, uint32(s.mode), s.exists})
		}
	}
	return writeJSON(m.recoveryPath(), backup)
}
func (m *Manager) nginxCommand(ctx context.Context, action ...string) error {
	args := []string{"-p", m.root + string(os.PathSeparator), "-c", m.confPath()}
	args = append(args, action...)
	return m.run(ctx, m.nginx(), args...)
}
func (m *Manager) restartZLM(ctx context.Context) error {
	if m.platform == "windows" {
		return m.run(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", filepath.Join(m.root, "uvp-gb28181-ctl.ps1"), "restart-zlm")
	}
	return m.run(ctx, "bash", filepath.Join(m.root, "uvp-gb28181-ctl.sh"), "restart-zlm")
}
func (m *Manager) apply(ctx context.Context, req Request) (result Result) {
	result = Result{State: "failed", Nginx: "unchanged", ZLM: "unchanged", UpdatedAt: time.Now()}
	fail := func(err error) Result { result.Error = err.Error(); return result }
	old, err := m.Settings()
	if err != nil {
		return fail(err)
	}
	rendered, err := RenderFile(m, req.Settings)
	if err != nil {
		return fail(err)
	}
	if req.Settings.Enabled && (!old.Enabled || old.Port != req.Port) {
		if err := m.checkPort(req.Port); err != nil {
			return fail(err)
		}
	}
	crt, key := req.Certificate, req.PrivateKey
	upload := len(crt) > 0 || len(key) > 0
	if !upload {
		crt, err = os.ReadFile(m.certPath())
		if err != nil {
			return fail(err)
		}
		key, err = os.ReadFile(m.keyPath())
		if err != nil {
			return fail(err)
		}
	}
	info, err := ValidateCertificate(crt, key)
	if err != nil && (upload || req.Enabled) {
		return fail(err)
	}
	var zlmFiles []snapshot
	var oldZLM CertificateInfo
	var ports []int
	if req.SyncZLM && upload {
		zlmFiles, err = capture(m.zlmPEM())
		if err != nil || len(zlmFiles) != 1 || !zlmFiles[0].exists {
			return fail(errors.New("无法读取 ZLM 原证书，未执行变更；请检查内置 ZLM 安装及文件权限"))
		}
		oldZLM, err = parseCertificate(zlmFiles[0].data, zlmFiles[0].data, false)
		if err != nil {
			return fail(fmt.Errorf("ZLM 原证书无法用于恢复，未执行变更: %w", err))
		}
		ports = m.zlmPorts()
		if len(ports) == 0 {
			return fail(errors.New("未找到内置 ZLM 的 HTTPS/WSS 端口，未执行变更"))
		}
	}
	files, err := capture(m.certPath(), m.keyPath(), m.confPath(), m.settingsPath())
	if err != nil {
		return fail(err)
	}
	if err := m.saveRecovery(files, zlmFiles); err != nil {
		return fail(fmt.Errorf("保存恢复材料失败，未执行变更: %w", err))
	}
	defer func() {
		if result.State != "recovery_required" {
			if err := os.Remove(m.recoveryPath()); err != nil && !os.IsNotExist(err) {
				result.State = "recovery_required"
				result.Error += "；清理恢复材料失败，请检查 data/https/recovery.json 及文件权限"
			}
		}
	}()
	reloadAttempted := false
	rollback := func(cause error) Result {
		rollbackErr := restore(files)
		if reloadAttempted && rollbackErr == nil {
			recovery, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			rollbackErr = m.nginxCommand(recovery, "-s", "reload")
			if rollbackErr == nil {
				port := old.Port
				if !old.Enabled {
					port = m.httpPort()
				}
				originalInfo, _ := parseCertificate(files[0].data, files[1].data, false)
				rollbackErr = m.nginxProbe(recovery, port, originalInfo.Fingerprint, old.Enabled)
			}
		}
		result.Nginx = "restored"
		if rollbackErr != nil {
			result.State = "recovery_required"
			result.Nginx = "recovery_required"
			cause = fmt.Errorf("%w；恢复旧配置或入口失败: %v", cause, rollbackErr)
		}
		return fail(cause)
	}
	if upload {
		if err = AtomicWrite(m.keyPath(), key, 0600); err != nil {
			return rollback(err)
		}
		if err = AtomicWrite(m.certPath(), crt, 0644); err != nil {
			return rollback(err)
		}
	}
	if err = AtomicWrite(m.confPath(), []byte(rendered), 0600); err != nil {
		return rollback(err)
	}
	if err = m.nginxCommand(ctx, "-t"); err != nil {
		return rollback(fmt.Errorf("Nginx 配置校验失败: %w", err))
	}
	reloadAttempted = true
	if err = m.nginxCommand(ctx, "-s", "reload"); err != nil {
		return rollback(fmt.Errorf("Nginx 重载失败: %w", err))
	}
	port := req.Port
	if !req.Enabled {
		port = m.httpPort()
	}
	if err = m.nginxProbe(ctx, port, info.Fingerprint, req.Enabled); err != nil {
		return rollback(fmt.Errorf("Nginx 新入口未生效: %w", err))
	}
	if err = writeJSON(m.settingsPath(), req.Settings); err != nil {
		return rollback(fmt.Errorf("保存 HTTPS 配置失败: %w", err))
	}
	result.Nginx = "applied"
	result.State = "succeeded"
	if !req.SyncZLM || !upload {
		return result
	}
	err = AtomicWrite(m.zlmPEM(), append(append(append([]byte{}, key...), '\n'), crt...), 0600)
	if err == nil {
		err = m.restartZLM(ctx)
	}
	if err == nil {
		for _, p := range ports {
			if err = m.probe(ctx, p, info.Fingerprint, true); err != nil {
				break
			}
		}
	}
	if err == nil {
		result.ZLM = "applied"
		return result
	}
	cause := err
	result.State = "partial"
	result.ZLM = "restored"
	recovery, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	restoreErr := restore(zlmFiles)
	if restoreErr == nil {
		restoreErr = m.restartZLM(recovery)
	}
	if restoreErr == nil {
		for _, p := range ports {
			if restoreErr = m.probe(recovery, p, oldZLM.Fingerprint, true); restoreErr != nil {
				break
			}
		}
	}
	if restoreErr != nil {
		result.State = "recovery_required"
		result.ZLM = "recovery_required"
		cause = fmt.Errorf("%w；恢复 ZLM 失败: %v", cause, restoreErr)
	}
	return fail(fmt.Errorf("Nginx 已生效，ZLM 证书同步失败: %w", cause))
}

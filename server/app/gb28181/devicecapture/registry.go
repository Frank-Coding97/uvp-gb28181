package devicecapture

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"uvplatform.cn/uvp-gb28181/app/gb28181/manscdp"
)

const maxImageBytes = 10 << 20

var (
	ErrNotFound     = errors.New("snapshot session not found")
	ErrInvalidImage = errors.New("invalid snapshot image")
)

type State string

const (
	StateCreating  State = "creating"
	StateWaiting   State = "waiting"
	StateReceiving State = "receiving"
	StateCompleted State = "completed"
	StateFailed    State = "failed"
)

type File struct {
	Name string `json:"name"`
	// Path 是**落盘绝对/相对路径**（给本进程读文件用），不进库、不出接口。
	Path string `json:"-"`
	// RelPath 是相对**抓拍图片基目录**的路径（`gb-device-snapshots/<sessionId>/<文件名>`），
	// 落库用。⛔ 与 Path 分开是必要的：库里的路径必须能跟着 serverroot 一起搬卷，
	// 存绝对路径的库一搬机器就全部指向不存在的文件。
	RelPath string `json:"-"`
	Size    int64  `json:"size"`
	// MD5 是图片字节的摘要（小写 hex），用于"平台手上的图"与"设备说的图"做完整性对账。
	MD5        string    `json:"md5"`
	ReceivedAt time.Time `json:"receivedAt"`
}

type Session struct {
	ID            string    `json:"sessionId"`
	UploadToken   string    `json:"-"`
	OwnerID       string    `json:"-"`
	ChannelID     string    `json:"channelId"`
	ChannelCode   string    `json:"channelCode"`
	DeviceCode    string    `json:"deviceCode"`
	SnapNum       int       `json:"snapNum"`
	Interval      int       `json:"interval"`
	State         State     `json:"state"`
	Files         []File    `json:"files"`
	NotifiedCount int       `json:"notifiedCount"`
	Error         string    `json:"error"`
	CreatedAt     time.Time `json:"createdAt"`
	UpdatedAt     time.Time `json:"updatedAt"`
	// notifiedIDs 是设备**已声明完成**的图像文件标识集合（去重），`NotifiedCount` 是它的基数。
	//
	// ⛔ 必须按**文件标识**而不是"一次通知"去重：通知识别走两种形态 ——
	// A.2.5.7 标准形态**一条报文带多个标识**（真机是 N 个并列的 `<SnapShotList>`），
	// 私有形态则是一图一条。若按"报文条数"计数，标准形态下 `NotifiedCount` 恒为 1，
	// 而 `completeLocked` 要求 `NotifiedCount >= SnapNum` ⇒ 会话永远完不成。
	notifiedIDs map[string]struct{}
}

type CreateRequest struct {
	OwnerID, ChannelID, ChannelCode, DeviceCode string
	SnapNum, Interval                           int
}

type Registry struct {
	mu       sync.RWMutex
	root     string
	sessions map[string]*Session
	byToken  map[string]string
}

func NewRegistry(root string) *Registry {
	return &Registry{root: root, sessions: make(map[string]*Session), byToken: make(map[string]string)}
}

func (r *Registry) Create(request CreateRequest) Session {
	now := time.Now()
	session := &Session{
		ID: uuid.NewString(), UploadToken: uuid.NewString(), OwnerID: request.OwnerID,
		ChannelID: request.ChannelID, ChannelCode: request.ChannelCode, DeviceCode: request.DeviceCode,
		SnapNum: request.SnapNum, Interval: request.Interval, State: StateCreating,
		CreatedAt: now, UpdatedAt: now, notifiedIDs: make(map[string]struct{}),
	}
	r.mu.Lock()
	r.sessions[session.ID] = session
	r.byToken[session.UploadToken] = session.ID
	r.pruneLocked()
	r.mu.Unlock()
	return cloneSession(session)
}

func (r *Registry) MarkWaiting(id string) {
	r.updateState(id, StateWaiting, "")
}

func (r *Registry) MarkFailed(id string, err error) {
	message := "抓拍配置下发失败"
	if err != nil {
		message = err.Error()
	}
	r.updateState(id, StateFailed, message)
}

func (r *Registry) updateState(id string, state State, message string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if session := r.sessions[id]; session != nil {
		session.State, session.Error, session.UpdatedAt = state, message, time.Now()
	}
}

func (r *Registry) GetForOwner(id, ownerID string) (Session, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	session := r.sessions[id]
	if session == nil || session.OwnerID != ownerID {
		return Session{}, false
	}
	copy := cloneSession(session)
	return copy, true
}

func (r *Registry) Upload(token, filename, contentType string, source io.Reader) (File, error) {
	r.mu.RLock()
	sessionID := r.byToken[strings.TrimSpace(token)]
	r.mu.RUnlock()
	if sessionID == "" {
		return File{}, ErrNotFound
	}
	filename = filepath.Base(strings.TrimSpace(filename))
	if filename == "." || filename == "" || !strings.HasSuffix(strings.ToLower(filename), ".jpg") && !strings.HasSuffix(strings.ToLower(filename), ".jpeg") {
		return File{}, ErrInvalidImage
	}
	if contentType != "" && !strings.HasPrefix(strings.ToLower(contentType), "image/jpeg") && !strings.HasPrefix(strings.ToLower(contentType), "application/octet-stream") {
		return File{}, ErrInvalidImage
	}
	payload, err := io.ReadAll(io.LimitReader(source, maxImageBytes+1))
	if err != nil || len(payload) < 4 || len(payload) > maxImageBytes || !bytes.HasPrefix(payload, []byte{0xff, 0xd8}) || !bytes.HasSuffix(payload, []byte{0xff, 0xd9}) {
		return File{}, ErrInvalidImage
	}
	directory := filepath.Join(r.root, "gb-device-snapshots", sessionID)
	if err := os.MkdirAll(directory, 0o750); err != nil {
		return File{}, err
	}
	path := filepath.Join(directory, filename)
	if err := os.WriteFile(path, payload, 0o640); err != nil {
		return File{}, err
	}
	// RelPath 用正斜杠手工拼（不用 filepath.Join）：它是要**落库**的，而库可能被
	// Windows 与 Linux 两种部署形态读同一份数据，带反斜杠的值换平台就失效。
	// ⛔ 拼接是安全的（无需转义/清洗）：sessionID 是 uuid、filename 已过 filepath.Base，
	// 两者都不可能包含路径分隔符。
	relative := "gb-device-snapshots/" + sessionID + "/" + filename
	digest := md5.Sum(payload)
	file := File{
		Name: filename, Path: path, RelPath: relative,
		Size: int64(len(payload)), MD5: hex.EncodeToString(digest[:]), ReceivedAt: time.Now(),
	}
	r.mu.Lock()
	session := r.sessions[sessionID]
	if session == nil {
		r.mu.Unlock()
		return File{}, ErrNotFound
	}
	replaced := false
	for index := range session.Files {
		if session.Files[index].Name == filename {
			session.Files[index] = file
			replaced = true
			break
		}
	}
	if !replaced {
		session.Files = append(session.Files, file)
	}
	session.State, session.UpdatedAt = StateReceiving, time.Now()
	r.completeLocked(session)
	r.mu.Unlock()
	return file, nil
}

// OnUploadSnapShotFinished 处理 **A.2.5.7 标准形态**的抓拍传输完成通知（**一对多**）。
//
// ⛔ 这是"抓拍会话能否收尾"的唯一入口：`completeLocked` 要求 `NotifiedCount >= SnapNum`，
// 而 `NotifiedCount` 只在这里（与私有形态那条）推进。2026-09-20 之前平台只认私有形态，
// 而真机发的是标准形态 ⇒ 会话永远停在 `receiving`，前端（`SnapshotConfigPanel.vue`）
// 轮询到的一直是"未完成"，而图片其实已经按 `UploadURL` 落盘了。
func (r *Registry) OnUploadSnapShotFinished(_ context.Context, senderDeviceID string, body []byte) error {
	finished, err := manscdp.ParseUploadSnapShotFinished(body)
	if err != nil {
		return err
	}
	return r.recordFinished(senderDeviceID, finished.DeviceID, finished.SessionID, finished.FileIDs)
}

// OnSnapshotNotify 处理**私有形态**（`Notify` + `SubCmd=SnapShot` + `SnapShotID`，"一图一条"）。
//
// ⭐ 为什么留着：模拟器（`uvp-gb28181-sim` 的 `SnapShotNotifyBuilder.kt`）当前发的仍是这个形状，
// 它是本仓唯一的端到端联调对象。两条路都收 ⇒ 真机与模拟器各自都能推进完成计数。
// 等 E-2 的模拟器侧也改成标准形态后，这一路连同 [manscdp.ParseSnapshotNotify] 可以删。
func (r *Registry) OnSnapshotNotify(_ context.Context, senderDeviceID string, body []byte) error {
	notify, err := manscdp.ParseSnapshotNotify(body)
	if err != nil {
		return err
	}
	return r.recordFinished(senderDeviceID, notify.DeviceID, notify.SessionID, []string{notify.SnapshotID})
}

// recordFinished 是两种完成通知形态的**共用记账**：定位会话 → 校验来源 → 按文件标识去重计数
// → 推进状态机。
//
// ⛔ 两条路必须共用这一份。形态可以有两种，"哪些标识算完成"只能有一个答案；分开写就等于把
// `NotifiedCount` 的语义拆成两套（各自去重、各自判归属），出问题时没人能回答"以哪条为准"。
func (r *Registry) recordFinished(senderDeviceID, deviceID, sessionID string, fileIDs []string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	session := r.sessions[sessionID]
	if session == nil {
		return ErrNotFound
	}
	senderDeviceID = strings.TrimSpace(senderDeviceID)
	if senderDeviceID != "" && senderDeviceID != session.DeviceCode && senderDeviceID != session.ChannelCode {
		return fmt.Errorf("snapshot notify sender mismatch: sender=%q", senderDeviceID)
	}
	// ⛔ 这里保持**严格**（要求报文里的 DeviceID 命中设备或通道编码），不因"设备可能漏写"
	// 就放宽成 OR：真机与模拟器都带了 DeviceID，放宽只会静默削掉一层归属校验。
	// 若将来真有设备漏写，错误信息里已经带了两个值，一眼能看出是"对方没写"而不是"会话找不到"。
	if deviceID != session.DeviceCode && deviceID != session.ChannelCode {
		return fmt.Errorf("snapshot notify device mismatch: sender=%q device=%q", senderDeviceID, deviceID)
	}
	for _, raw := range fileIDs {
		id := strings.TrimSpace(raw)
		if id == "" {
			continue
		}
		if _, exists := session.notifiedIDs[id]; exists {
			continue
		}
		session.notifiedIDs[id] = struct{}{}
		session.NotifiedCount++
	}
	session.UpdatedAt = time.Now()
	r.completeLocked(session)
	return nil
}

// FilePath 把**库里的 `rel_path`** 解析成本进程可读的文件路径，并确保它没逃出基目录。
//
// ⛔ 为什么解析规则放在这里而不是调用方：目录布局是 registry 自己定义的
// （`<root>/gb-device-snapshots/<sessionId>/<file>`），只有它知道怎么还原。
// 调用方自己拼路径 = 第二份布局知识，改布局时必然有一边漏改。
//
// ⛔ 前缀校验不是形式主义：`rel_path` 来自库，而库行可被人工改写、也可被回滚后的旧版本写入。
// 没有这一层时 `../../../etc/passwd` 这类值会被 `filepath.Join` 正常"清理"成一条基目录之外
// 的真实路径，于是读图接口变成一个任意文件读取口子。
func (r *Registry) FilePath(relPath string) (string, error) {
	base, err := filepath.Abs(r.root)
	if err != nil {
		return "", err
	}
	target, err := filepath.Abs(filepath.Join(base, filepath.FromSlash(strings.TrimSpace(relPath))))
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(target, base+string(os.PathSeparator)) {
		return "", ErrNotFound
	}
	return target, nil
}

// SessionForToken 按**上传令牌**取会话快照（不校验 owner —— 调用方是收图侧，
// 那时还没有登录态；令牌本身就是凭证）。
//
// ⭐ 用途：设备 POST 回来时，平台只知道令牌，要靠它把"这张图属于哪个设备/通道/会话"
// 问出来才能落库。返回的是 [cloneSession] 的副本（并发安全），不含 `notifiedIDs`。
func (r *Registry) SessionForToken(token string) (Session, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	session := r.sessions[r.byToken[strings.TrimSpace(token)]]
	if session == nil {
		return Session{}, false
	}
	return cloneSession(session), true
}

func (r *Registry) Content(token, filename string) (File, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	sessionID := r.byToken[strings.TrimSpace(token)]
	session := r.sessions[sessionID]
	if session == nil {
		return File{}, ErrNotFound
	}
	filename = filepath.Base(filename)
	for _, file := range session.Files {
		if file.Name == filename {
			return file, nil
		}
	}
	return File{}, ErrNotFound
}

func (r *Registry) completeLocked(session *Session) {
	if len(session.Files) >= session.SnapNum && session.NotifiedCount >= session.SnapNum {
		session.State = StateCompleted
	}
}

func (r *Registry) pruneLocked() {
	if len(r.sessions) <= 256 {
		return
	}
	items := make([]*Session, 0, len(r.sessions))
	for _, session := range r.sessions {
		items = append(items, session)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt.Before(items[j].CreatedAt) })
	for _, session := range items[:len(items)-256] {
		delete(r.sessions, session.ID)
		delete(r.byToken, session.UploadToken)
	}
}

func cloneSession(source *Session) Session {
	copy := *source
	copy.Files = append([]File(nil), source.Files...)
	// ⛔ 必须置空：`cloneSession` 的返回值会经 JSON 出去，而 `notifiedIDs` 是并发读写的 map
	// （`recordFinished` 持写锁改它）。带上就等于把内部记账对象交给调用方裸读。
	copy.notifiedIDs = nil
	return copy
}

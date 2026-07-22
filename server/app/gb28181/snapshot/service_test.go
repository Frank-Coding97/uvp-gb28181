package snapshot

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

// fakeZLMClient 记录被调次数 + 返回可配置结果
type fakeZLMClient struct {
	called   int32
	returnMe []byte
	returnErr error
	sleep     time.Duration
}

func (f *fakeZLMClient) GetSnap(ctx context.Context, streamID string, timeoutSec, expireSec int) ([]byte, error) {
	atomic.AddInt32(&f.called, 1)
	if f.sleep > 0 {
		select {
		case <-time.After(f.sleep):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return f.returnMe, f.returnErr
}

// fakeRepo 记录 UpdateSnapshot 调用
type fakeRepo struct {
	called    int32
	lastURL   string
	returnErr error
}

func (r *fakeRepo) UpdateSnapshot(ctx context.Context, deviceID, channelID, url string, at time.Time) error {
	atomic.AddInt32(&r.called, 1)
	r.lastURL = url
	return r.returnErr
}

func newServiceForTest(t *testing.T, tmpDir string, client *fakeZLMClient, repo *fakeRepo) *Service {
	t.Helper()
	return New(Config{
		UploadRoot:  tmpDir,
		URLPrefix:   "/uploads",
		DedupTTL:    30 * time.Second,
		DelayBefore: 10 * time.Millisecond, // 单测里加速
		ZLMTimeout:  5,
		ZLMExpire:   30,
		GetClient:   func(nodeID string) (ZLMClient, error) { return client, nil },
		BuildStreamURL: func(ctx context.Context, nodeID, streamID string) (string, error) {
			return "rtsp://mock/" + streamID, nil
		},
		Repo: repo,
	})
}

// TestService_DoCapture_HappyPath 单测:成功抓帧落盘并更新 DB
func TestService_DoCapture_HappyPath(t *testing.T) {
	tmp := t.TempDir()
	fakeJPEG := []byte{0xFF, 0xD8, 0xFF, 'X', 'Y', 'Z'}
	client := &fakeZLMClient{returnMe: fakeJPEG}
	repo := &fakeRepo{}
	svc := newServiceForTest(t, tmp, client, repo)

	if err := svc.doCapture("node-1", "stream-1", "did-1", "cid-1"); err != nil {
		t.Fatalf("doCapture 报错: %v", err)
	}
	if atomic.LoadInt32(&client.called) != 1 {
		t.Errorf("ZLM getSnap 应被调 1 次,实际 %d", client.called)
	}
	if atomic.LoadInt32(&repo.called) != 1 {
		t.Errorf("repo UpdateSnapshot 应被调 1 次,实际 %d", repo.called)
	}
	// 检查文件落盘
	matches, _ := filepath.Glob(filepath.Join(tmp, "gb-channel-snapshot", "*", "did-1_cid-1.jpg"))
	if len(matches) != 1 {
		t.Fatalf("期望落盘 1 个文件,实际 %d: %v", len(matches), matches)
	}
	got, _ := os.ReadFile(matches[0])
	if string(got) != string(fakeJPEG) {
		t.Errorf("落盘内容不匹配")
	}
	// URL 传给 DB
	if repo.lastURL == "" || repo.lastURL[:9] != "/uploads/" {
		t.Errorf("relURL 应以 /uploads/ 开头,实际 %s", repo.lastURL)
	}
}

// TestService_FireAfterPlay_DedupHit_NoRPC 30s 内二次触发不打 ZLM
func TestService_FireAfterPlay_DedupHit_NoRPC(t *testing.T) {
	tmp := t.TempDir()
	client := &fakeZLMClient{returnMe: []byte{0xFF, 0xD8, 0xFF}}
	repo := &fakeRepo{}
	svc := newServiceForTest(t, tmp, client, repo)

	svc.FireAfterPlay(context.Background(), "node-1", "stream-1", "did-1", "cid-1")
	// 等第一次 goroutine 落地
	time.Sleep(100 * time.Millisecond)

	// 第二次触发,应被 dedup 直接挡掉,不 spawn goroutine
	svc.FireAfterPlay(context.Background(), "node-1", "stream-1", "did-1", "cid-1")
	time.Sleep(100 * time.Millisecond)

	if atomic.LoadInt32(&client.called) != 1 {
		t.Errorf("dedup 命中时不应二次调 ZLM,实际 called=%d", client.called)
	}
}

// TestService_FireAfterPlay_ZLMFail_NoPanic ZLM 报错时 warn 不 panic,repo 不被调
func TestService_FireAfterPlay_ZLMFail_NoPanic(t *testing.T) {
	tmp := t.TempDir()
	client := &fakeZLMClient{returnErr: errors.New("stream not found")}
	repo := &fakeRepo{}
	svc := newServiceForTest(t, tmp, client, repo)

	svc.FireAfterPlay(context.Background(), "node-1", "stream-1", "did-1", "cid-1")
	time.Sleep(150 * time.Millisecond)

	if atomic.LoadInt32(&repo.called) != 0 {
		t.Errorf("ZLM 失败时 repo 不应被调,实际 called=%d", repo.called)
	}
}

// TestService_FireAfterPlay_NilSafe s 为 nil 时不 panic
func TestService_FireAfterPlay_NilSafe(t *testing.T) {
	var s *Service
	// 不 panic 即可
	s.FireAfterPlay(context.Background(), "node-1", "stream-1", "did-1", "cid-1")
}

// TestService_DoCapture_EmptyBytesRejected 空 body 视为失败
func TestService_DoCapture_EmptyBytesRejected(t *testing.T) {
	tmp := t.TempDir()
	client := &fakeZLMClient{returnMe: []byte{}}
	repo := &fakeRepo{}
	svc := newServiceForTest(t, tmp, client, repo)

	err := svc.doCapture("node-1", "stream-1", "did-1", "cid-1")
	if err == nil {
		t.Fatal("空 body 应视为失败")
	}
	if atomic.LoadInt32(&repo.called) != 0 {
		t.Error("空 body 时不应 UPDATE DB")
	}
}

// TestService_DoCapture_NilClientLookup GetClient 返 nil 时 warn 不 panic
func TestService_DoCapture_NilClientLookup(t *testing.T) {
	tmp := t.TempDir()
	svc := New(Config{
		UploadRoot:  tmp,
		GetClient:   func(nodeID string) (ZLMClient, error) { return nil, nil },
		BuildStreamURL: func(ctx context.Context, nodeID, streamID string) (string, error) {
			return "rtsp://mock/" + streamID, nil
		},
		Repo:        &fakeRepo{},
		DelayBefore: 10 * time.Millisecond,
	})
	err := svc.doCapture("node-1", "stream-1", "did", "cid")
	if err == nil {
		t.Error("GetClient 返 nil client 应报错")
	}
}

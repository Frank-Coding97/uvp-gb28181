package playback

import (
	"context"
	"errors"
	"testing"
	"time"

	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
)

// 回归锚点(2026-10-03):ServiceConfig.Intents 必须与 rtp opener 的 Intent 子步骤能力匹配。
//
// legacy opener 场景下若传入非 nil Intents,Create 会在工厂断言处返回 ErrRTPUnavailable。
// 该错误不是 ServiceError,控制器把它映射成 502 +「回放控制失败」,且失败发生在节点调度
// 之前 —— 所以既没有 scheduler_log 记录,也没有 SIP INVITE,与线上现象(约 4ms 返回、
// 不产生回放会话)一致。此测试固定「错过此断言必须 fail-closed 且不触碰调度器」的行为。
func TestCreate_LegacyOpenerWithIntentsFailsBeforeScheduling(t *testing.T) {
	now := time.Unix(1707000000, 0)
	legacy := NewZLMRTPOpener(nil, nil, nil)
	if _, ok := legacy.(IntentRTPFactory); ok {
		t.Fatal("前置条件:legacy ZLM opener 不应实现 IntentRTPFactory")
	}
	picker := &fakeNodePicker{node: NodeInfo{ID: "node-1", ServerID: "34020000002000000001", Destination: "192.0.2.20:5060", RecvIP: "192.0.2.10"}}
	service := NewService(NewRegistry(RegistryConfig{Now: func() time.Time { return now }}), picker, legacy,
		&fakeInvite{}, &fakeMedia{ready: MediaReady{}}, ServiceConfig{Intents: new(playauth.DeviceOperationIntentStore)})

	_, err := service.Create(context.Background(), validCreate(now))
	if !errors.Is(err, ErrRTPUnavailable) {
		t.Fatalf("err=%v, want ErrRTPUnavailable", err)
	}
	if picker.calls.Load() != 0 {
		t.Fatalf("失败发生在调度之前,不应调用节点调度器, got %d 次", picker.calls.Load())
	}
}

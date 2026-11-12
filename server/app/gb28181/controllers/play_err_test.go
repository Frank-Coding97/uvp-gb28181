package controllers

import (
	"errors"
	"fmt"
	"testing"

	"uvplatform.com/uvp-gb28181/app/gb28181/play"
	"uvplatform.com/uvp-gb28181/app/gb28181/uac"
)

// TestMapPlayErrPassesThroughPeerRejection 回归 2026-10-25 级联验收缺陷:
// 对端回 400 并写明原因时,页面必须看到"对端拒绝点播: 400 <reason>",
// 而不是一句没有排查方向的"点播失败"。
func TestMapPlayErrPassesThroughPeerRejection(t *testing.T) {
	const reason = "ack sms[34020000002020000001] failed, res[400] none rtp data receive on udp://192.168.10.220:30050"
	err := fmt.Errorf("发 INVITE 失败: %w", &uac.InviteRejectedError{Status: 400, Reason: reason})

	if !errors.Is(err, play.ErrInviteRejected) {
		t.Fatalf("err=%v 必须能 errors.Is 到 play.ErrInviteRejected", err)
	}
	want := "对端拒绝点播: 400 " + reason
	if got := mapPlayErr(err); got != want {
		t.Fatalf("mapPlayErr=%q, want %q", got, want)
	}
}

// TestMapPlayErrKeepsKnownBranches 既有文案不能被新分支抢走或串味。
func TestMapPlayErrKeepsKnownBranches(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"设备不存在", play.ErrDeviceNotFound, "设备不存在"},
		{"设备离线", play.ErrDeviceOffline, "设备离线,无法点播"},
		{"通道不存在", play.ErrChannelNotFound, "通道不存在"},
		{"流未就绪", play.ErrStreamNotReady, "流就绪超时,设备未推流"},
		{"总超时", play.ErrPlayTimeout, "点播超时"},
		{"无原因的对端拒绝", &uac.InviteRejectedError{Status: 403}, "对端拒绝点播: 403"},
		{"未知错误", errors.New("boom"), "点播失败"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := mapPlayErr(c.err); got != c.want {
				t.Fatalf("mapPlayErr=%q, want %q", got, c.want)
			}
		})
	}
}

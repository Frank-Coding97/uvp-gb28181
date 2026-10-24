package probe_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"uvplatform.com/uvp-gb28181/app/gb28181/zlm/node"
	"uvplatform.com/uvp-gb28181/app/gb28181/zlm/probe"
)

// fakeRangeReconciler 记录探活触发的收流范围对账入参。
type fakeRangeReconciler struct {
	mu      sync.Mutex
	calls   []rangeCall
	err     error
	changed bool
}

type rangeCall struct {
	id         int64
	start, end int
}

func (f *fakeRangeReconciler) ReconcileRTPPortRange(_ context.Context, id int64, start, end int) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, rangeCall{id: id, start: start, end: end})
	return f.changed, f.err
}

func (f *fakeRangeReconciler) snapshot() []rangeCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]rangeCall, len(f.calls))
	copy(out, f.calls)
	return out
}

// 探活成功且身份吻合 ⇒ 用端点自报的 rtp_proxy.port_range 校正登记值。
// 这是"登记值只是镜像、真源在 ZLM"这条契约的唯一自愈点（每次重启生效一次）。
func TestProber_ReconcilesRTPRangeFromEndpoint(t *testing.T) {
	n := &node.Node{ID: 7, Name: "zlm-a", Host: "10.0.0.1", MediaServerUUID: "u1", State: node.StateActive}
	reg := newFakeRegistry(n)
	factory := func(_ *node.Node) probe.Client {
		return &fakeClient{cfg: map[string]string{
			"general.mediaServerId": "u1",
			"rtp_proxy.port_range":  "51014-51063",
		}}
	}
	rec := &fakeRangeReconciler{changed: true}

	p := probe.New(reg, factory, 500*time.Millisecond, nil)
	p.SetRangeReconciler(rec)
	results := p.Run(context.Background())

	require.Len(t, results, 1)
	require.True(t, results[0].Pass)
	require.True(t, results[0].RTPRangeReconciled)
	require.Equal(t, []rangeCall{{id: 7, start: 51014, end: 51063}}, rec.snapshot())
}

// 身份不吻合（影子行/端口被别的 ZLM 占了）⇒ 只跳过激活，**不许**顺手改登记值：
// 那台端点根本不是这个节点，它的范围不是本节点的真源。
func TestProber_DoesNotReconcileWhenIdentityMismatches(t *testing.T) {
	n := &node.Node{ID: 7, Name: "zlm-a", Host: "10.0.0.1", MediaServerUUID: "u1", State: node.StateOffline}
	reg := newFakeRegistry(n)
	factory := func(_ *node.Node) probe.Client {
		return &fakeClient{cfg: map[string]string{
			"general.mediaServerId": "someone-else",
			"rtp_proxy.port_range":  "51014-51063",
		}}
	}
	rec := &fakeRangeReconciler{changed: true}

	p := probe.New(reg, factory, 500*time.Millisecond, nil)
	p.SetRangeReconciler(rec)
	results := p.Run(context.Background())

	require.Len(t, results, 1)
	require.True(t, results[0].Skipped)
	require.False(t, results[0].RTPRangeReconciled)
	require.Empty(t, rec.snapshot())
}

// 端点活着但没给可用的范围（旧版 ZLM / 手改坏）⇒ 保留原登记值，不动。
func TestProber_ReconcileSkippedWhenRangeUnusable(t *testing.T) {
	for _, value := range []string{"", "0-0", "51063-51014", "abc"} {
		t.Run(value, func(t *testing.T) {
			n := &node.Node{ID: 7, Name: "zlm-a", MediaServerUUID: "u1", State: node.StateActive}
			reg := newFakeRegistry(n)
			factory := func(_ *node.Node) probe.Client {
				return &fakeClient{cfg: map[string]string{
					"general.mediaServerId": "u1",
					"rtp_proxy.port_range":  value,
				}}
			}
			rec := &fakeRangeReconciler{changed: true}

			p := probe.New(reg, factory, 500*time.Millisecond, nil)
			p.SetRangeReconciler(rec)
			results := p.Run(context.Background())

			require.True(t, results[0].Pass)
			require.False(t, results[0].RTPRangeReconciled)
			require.Empty(t, rec.snapshot())
		})
	}
}

// 对账失败只降级成"这次没对上"，不改变探活结论 ——
// 节点确实是活的，把它判 fail 会连带把它从调度池里踢掉。
func TestProber_ReconcileFailureKeepsProbePassing(t *testing.T) {
	n := &node.Node{ID: 7, Name: "zlm-a", MediaServerUUID: "u1", State: node.StateActive}
	reg := newFakeRegistry(n)
	factory := func(_ *node.Node) probe.Client {
		return &fakeClient{cfg: map[string]string{
			"general.mediaServerId": "u1",
			"rtp_proxy.port_range":  "51014-51063",
		}}
	}
	rec := &fakeRangeReconciler{err: errors.New("node revision conflict")}

	p := probe.New(reg, factory, 500*time.Millisecond, nil)
	p.SetRangeReconciler(rec)
	results := p.Run(context.Background())

	require.Len(t, results, 1)
	require.True(t, results[0].Pass)
	require.False(t, results[0].RTPRangeReconciled)
	require.Len(t, rec.snapshot(), 1)
	require.Equal(t, 1, reg.markCalls[7])
}

// 未装配对账器（历史装配路径）⇒ 行为与改动前一致：不碰登记值，也不报错。
func TestProber_WithoutReconcilerKeepsLegacyBehavior(t *testing.T) {
	n := &node.Node{ID: 7, Name: "zlm-a", MediaServerUUID: "u1", State: node.StateActive}
	reg := newFakeRegistry(n)
	factory := func(_ *node.Node) probe.Client {
		return &fakeClient{cfg: map[string]string{
			"general.mediaServerId": "u1",
			"rtp_proxy.port_range":  "51014-51063",
		}}
	}

	p := probe.New(reg, factory, 500*time.Millisecond, nil)
	results := p.Run(context.Background())

	require.Len(t, results, 1)
	require.True(t, results[0].Pass)
	require.False(t, results[0].RTPRangeReconciled)
}

package service_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/service"
)

// unprovisionStub 记录每次解约请求，并可切换成功 / 失败。
type unprovisionStub struct {
	mu      sync.Mutex
	errs    []error
	calls   int
	targets []node.Node
}

func (s *unprovisionStub) UnprovisionHooks(_ context.Context, n *node.Node) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	if n != nil {
		s.targets = append(s.targets, *n)
	}
	if len(s.errs) == 0 {
		return nil
	}
	err := s.errs[0]
	s.errs = s.errs[1:]
	return err
}

func (s *unprovisionStub) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

// retiringNode 造一个"刚被删除"的节点快照（Delete 之后内存里剩下的那份）。
func retiringNode() *node.Node {
	return &node.Node{
		ID: 7, Name: "zlm-ghost", Host: "192.168.10.220", APIPort: 21080, APISecret: "secret-x",
		MediaServerUUID: "56e37a2a-41ff-4346-832b-58c24ae93911",
	}
}

// 手摇时钟：退避是分钟级，不注入时钟测试就只能 sleep。
type handClock struct {
	mu  sync.Mutex
	now time.Time
}

func newHandClock() *handClock {
	return &handClock{now: time.Date(2026, 9, 21, 16, 0, 0, 0, time.Local)}
}

func (c *handClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *handClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// newRetireCoordinator 装配一个"同步派发 + 手摇时钟"的协调器：断言不必等 goroutine，
// 也不必真的睡分钟。
func newRetireCoordinator(t *testing.T, stub *unprovisionStub) (*service.RetiredNodeCoordinator, *node.RetirementIndex, *handClock) {
	t.Helper()
	index := node.NewRetirementIndex(nil)
	coordinator := service.NewRetiredNodeCoordinator(index, stub)
	clock := newHandClock()
	coordinator.SetClock(clock.Now)
	coordinator.SetDispatcher(func(f func()) { f() })
	return coordinator, index, clock
}

// 退役的第一拍与第二拍：凭据留下来，对端 hook 被撤掉并把状态记成 done。
func TestRetireDeletedNodeRetainsCredentialsThenRevokes(t *testing.T) {
	stub := &unprovisionStub{}
	coordinator, index, _ := newRetireCoordinator(t, stub)
	target := retiringNode()

	require.NoError(t, coordinator.RetainCredentials(context.Background(), target, "operator_delete"))
	cred, ok := index.Lookup(target.MediaServerUUID)
	require.True(t, ok, "删行前必须留下可撤销的凭据")
	require.Equal(t, node.RetireUnprovisionPending, cred.UnprovisionState)
	require.Equal(t, "operator_delete", cred.RetireReason)
	require.Equal(t, 0, stub.count(), "只留凭据阶段不该发请求")

	coordinator.RevokeAfterRetire(target.MediaServerUUID)

	require.Equal(t, 1, stub.count())
	require.Equal(t, target.Host, stub.targets[0].Host)
	require.Equal(t, target.APIPort, stub.targets[0].APIPort)
	require.Equal(t, target.APISecret, stub.targets[0].APISecret)
	require.Equal(t, target.MediaServerUUID, stub.targets[0].MediaServerUUID)
	// 还原出来的只是"够发一次请求"的定位信息，不是把节点复活。
	require.Zero(t, stub.targets[0].ID)

	cred, ok = index.Lookup(target.MediaServerUUID)
	require.True(t, ok)
	require.Equal(t, node.RetireUnprovisionDone, cred.UnprovisionState)
	require.Equal(t, 1, cred.UnprovisionAttempts)
	require.True(t, cred.Resolved())
}

// 撤不掉时必须留成显式失配记录，并且**退避**——不能每次回调都打一次对端。
func TestRevokeFailureIsRecordedAndBackedOff(t *testing.T) {
	stub := &unprovisionStub{errs: []error{
		errors.New("dial tcp 192.168.10.220:21080: connect: connection refused"),
		errors.New("still down"),
		nil,
	}}
	coordinator, index, clock := newRetireCoordinator(t, stub)
	target := retiringNode()
	require.NoError(t, coordinator.RetainCredentials(context.Background(), target, "purge_unreachable"))

	coordinator.RevokeAfterRetire(target.MediaServerUUID)
	cred, _ := index.Lookup(target.MediaServerUUID)
	require.Equal(t, node.RetireUnprovisionUnreachable, cred.UnprovisionState, "撤不掉不能被当成成功")
	require.Equal(t, 1, stub.count())

	// 退避窗口内重复触发（对端每 10s 一条心跳）不再打对端。
	coordinator.RevokeAfterRetire(target.MediaServerUUID)
	coordinator.ObserveRetiredHook(target.MediaServerUUID, "192.168.10.220")
	require.Equal(t, 1, stub.count(), "退避窗口内不得重试")

	// 首次退避是 1 分钟：过界后重试；这一次仍失败 ⇒ 退避翻倍到 2 分钟。
	clock.advance(time.Minute + time.Second)
	coordinator.RevokeAfterRetire(target.MediaServerUUID)
	require.Equal(t, 2, stub.count())

	// 距上一次尝试只过了 90 秒 < 2 分钟 ⇒ 仍然按住。
	clock.advance(90 * time.Second)
	coordinator.RevokeAfterRetire(target.MediaServerUUID)
	require.Equal(t, 2, stub.count(), "退避应已翻倍到 2 分钟")

	// 再走 1 分钟即越过 2 分钟边界 ⇒ 第三次，这次成功。
	clock.advance(time.Minute)
	coordinator.RevokeAfterRetire(target.MediaServerUUID)
	require.Equal(t, 3, stub.count())
	cred, _ = index.Lookup(target.MediaServerUUID)
	require.Equal(t, node.RetireUnprovisionDone, cred.UnprovisionState, "第三次成功应收口")
	require.Equal(t, 3, cred.UnprovisionAttempts)

	// 结清之后不再重试：对端已经不回调了，也没有触发源了。
	clock.advance(time.Hour)
	coordinator.RevokeAfterRetire(target.MediaServerUUID)
	require.Equal(t, 3, stub.count(), "已结清的凭据不该再发请求")
}

// 退避上限：一直失败时不能无限翻倍（30 分钟封顶，与折叠窗口对齐）。
func TestRevokeBackoffIsCappedAtThirtyMinutes(t *testing.T) {
	stub := &unprovisionStub{errs: []error{
		errors.New("down"), errors.New("down"), errors.New("down"),
		errors.New("down"), errors.New("down"), errors.New("down"), errors.New("down"),
		errors.New("down"), errors.New("down"),
	}}
	coordinator, index, clock := newRetireCoordinator(t, stub)
	target := retiringNode()
	require.NoError(t, coordinator.RetainCredentials(context.Background(), target, "purge_unreachable"))

	// 连续失败 6 次：1 → 2 → 4 → 8 → 16 → 30(封顶)。
	wait := time.Minute
	for round := 0; round < 6; round++ {
		clock.advance(wait + time.Second)
		coordinator.RevokeAfterRetire(target.MediaServerUUID)
		require.Equal(t, round+1, stub.count(), "第 %d 轮应已越过退避边界", round+1)
		wait *= 2
		if wait > 30*time.Minute {
			wait = 30 * time.Minute
		}
	}

	// 封顶之后再等 31 分钟才有下一次；等 29 分钟没有。
	clock.advance(29 * time.Minute)
	coordinator.RevokeAfterRetire(target.MediaServerUUID)
	require.Equal(t, 6, stub.count(), "退避不得随失败次数无限增长")

	clock.advance(2 * time.Minute)
	coordinator.RevokeAfterRetire(target.MediaServerUUID)
	require.Equal(t, 7, stub.count())
	require.Equal(t, node.RetireUnprovisionUnreachable,
		mustCredential(t, index, target.MediaServerUUID).UnprovisionState)
}

// 热路径：只有**退休过**的 uuid 才被认领，陌生人一个都不能误判。
func TestObserveRetiredHookOnlyClaimsKnownRetiredUUIDs(t *testing.T) {
	stub := &unprovisionStub{}
	coordinator, index, _ := newRetireCoordinator(t, stub)
	target := retiringNode()
	require.NoError(t, coordinator.RetainCredentials(context.Background(), target, "operator_delete"))

	require.False(t, coordinator.ObserveRetiredHook("never-seen-uuid", "10.0.0.1"),
		"没退休过的 uuid 不能被认领成 node_retired")
	require.Equal(t, 0, stub.count())

	require.True(t, coordinator.ObserveRetiredHook(target.MediaServerUUID, "192.168.10.220"))
	require.Equal(t, 1, stub.count(), "认领的同时安排一次解约重试")
	require.Equal(t, node.RetireUnprovisionDone,
		mustCredential(t, index, target.MediaServerUUID).UnprovisionState)

	// 结清后仍认得它（凭据没删），但不再重试。
	require.True(t, coordinator.ObserveRetiredHook(target.MediaServerUUID, "192.168.10.220"))
	require.Equal(t, 1, stub.count())
}

// 没有 uuid 就没有归属判据 —— 保留凭据必须失败得明明白白，而不是静默存一条没法用的记录。
func TestRetainCredentialsRequiresUUID(t *testing.T) {
	stub := &unprovisionStub{}
	coordinator, index, _ := newRetireCoordinator(t, stub)

	err := coordinator.RetainCredentials(context.Background(), &node.Node{Name: "no-uuid"}, "operator_delete")
	require.ErrorIs(t, err, service.ErrRetiredNodeUUIDMissing)
	require.Equal(t, 0, index.Len())

	// nil 节点是"没什么可退休的"，不是"缺 uuid"：前者是空操作，后者是
	// "拿到了一个节点却认不出它"，两者不能用同一个错误。
	require.NoError(t, coordinator.RetainCredentials(context.Background(), nil, "operator_delete"))
	require.Equal(t, 0, index.Len())
}

// 未装配解约能力时：凭据照留、状态记成 unreachable，但不 panic、不假装成功。
func TestRevokeWithoutUnprovisionerIsRecordedAsUnreachable(t *testing.T) {
	index := node.NewRetirementIndex(nil)
	coordinator := service.NewRetiredNodeCoordinator(index, nil)
	coordinator.SetDispatcher(func(f func()) { f() })
	target := retiringNode()
	require.NoError(t, coordinator.RetainCredentials(context.Background(), target, "operator_delete"))

	coordinator.RevokeAfterRetire(target.MediaServerUUID)
	require.Equal(t, node.RetireUnprovisionUnreachable,
		mustCredential(t, index, target.MediaServerUUID).UnprovisionState)
}

func mustCredential(t *testing.T, index *node.RetirementIndex, uuid string) node.RetiredCredential {
	t.Helper()
	cred, ok := index.Lookup(uuid)
	require.True(t, ok)
	return cred
}

// 退役凭据只在**真的删掉了**之后才落 —— 被拒的删除是常态（影响预检冲突、
// 或"强制移除"时发现节点其实可达），它绝不能在退休表里留下一条"其实还活着"的记录。
func TestNodeServiceRetiresOnlyAfterASuccessfulDelete(t *testing.T) {
	stub := &unprovisionStub{}
	build := func(t *testing.T) (*service.NodeService, *node.Registry, *node.Node, *node.RetirementIndex) {
		t.Helper()
		reg := node.NewRegistry(newT13Repo())
		created := t13Node(t, reg)
		svc := service.NewNodeService(reg, &t13Probe{}, service.MediaTuning{})
		index := node.NewRetirementIndex(nil)
		coordinator := service.NewRetiredNodeCoordinator(index, stub)
		coordinator.SetDispatcher(func(f func()) { f() })
		svc.SetRetiredNodeCoordinator(coordinator)
		return svc, reg, created, index
	}

	t.Run("普通删除成功", func(t *testing.T) {
		stub.calls = 0
		svc, _, created, index := build(t)
		require.NoError(t, svc.Delete(context.Background(), created.ID))

		cred, ok := index.Lookup(created.MediaServerUUID)
		require.True(t, ok, "删成功之后必须留下撤销凭据")
		require.Equal(t, "operator_delete", cred.RetireReason)
		require.Equal(t, created.Host, cred.Host)
		require.Equal(t, created.APISecret, cred.APISecret)
		require.Equal(t, 1, stub.count(), "删成功之后要异步撤销对端 hook")
	})

	t.Run("还有会话时被拒", func(t *testing.T) {
		stub.calls = 0
		svc, reg, created, index := build(t)
		reg.UpdateStats(created.MediaServerUUID, node.Stats{SessionCount: 2, MediaSourceCount: 1})

		err := svc.Delete(context.Background(), created.ID)
		require.ErrorIs(t, err, service.ErrNodeImpactConflict)
		require.Equal(t, 0, index.Len(), "被拒的删除不得留下退休凭据")
		require.Equal(t, 0, stub.count(), "被拒的删除更不该去打对端")
	})

	t.Run("强制移除时节点其实可达", func(t *testing.T) {
		stub.calls = 0
		svc, _, created, index := build(t)
		svc.SetNodeImpactProvider(reachableImpactProvider())
		svc.SetNodeReferenceCleaner(func(context.Context, int64) (int64, error) { return 0, nil })

		_, err := svc.PurgeUnreachable(context.Background(), created.ID)
		require.ErrorIs(t, err, service.ErrNodeReachable)
		require.Equal(t, 0, index.Len())
		require.Equal(t, 0, stub.count())
	})

	t.Run("强制移除不可达节点", func(t *testing.T) {
		stub.calls = 0
		svc, _, created, index := build(t)
		svc.SetNodeImpactProvider(unreachableImpactProvider())
		svc.SetNodeReferenceCleaner(func(context.Context, int64) (int64, error) { return 3, nil })

		result, err := svc.PurgeUnreachable(context.Background(), created.ID)
		require.NoError(t, err)
		require.Equal(t, created.ID, result.NodeID)

		cred, ok := index.Lookup(created.MediaServerUUID)
		require.True(t, ok, "不可达节点恰恰最需要留下凭据：对端一恢复回调就能自愈")
		require.Equal(t, "purge_unreachable", cred.RetireReason)
		require.Equal(t, 1, stub.count())
	})
}

// 新建失败回滚走同一类收尾：收敛可能在"hook 已经写进对端、回读对不上"那一步才失败，
// 此时回滚删掉了本地行，但**对端已经知道这个 uuid** —— 不清掉就是幽灵节点的缩影。
func TestNodeServiceRetiresWhenCreateRollsBack(t *testing.T) {
	stub := &unprovisionStub{}
	repo := newMemoryRepo()
	probe := &mockProbe{setServerConfigErr: errors.New("ZLM 配置回读不一致: hook.on_server_keepalive")}
	svc := newSvc(repo, probe)
	index := node.NewRetirementIndex(nil)
	coordinator := service.NewRetiredNodeCoordinator(index, stub)
	coordinator.SetDispatcher(func(f func()) { f() })
	svc.SetRetiredNodeCoordinator(coordinator)

	_, err := svc.Create(context.Background(), service.CreateNodeReq{
		Name: "ghost-attempt", Host: "192.168.10.220", APIPort: 21080, APISecret: "s3cr3t",
	})
	require.Error(t, err, "收敛失败必须冒泡给调用方")
	require.Empty(t, repo.rows, "回滚应删掉本地行")

	require.Equal(t, 1, index.Len(), "回滚删行之后必须留下撤销凭据")
	cred := index.List()[0]
	require.Equal(t, "failed_create_rollback", cred.RetireReason)
	require.Equal(t, "192.168.10.220", cred.Host)
	require.Equal(t, 21080, cred.APIPort)
	require.Equal(t, "s3cr3t", cred.APISecret)
	require.NotEmpty(t, cred.MediaServerUUID, "凭据里的 uuid 要能对上对端自称的那个")
	require.Equal(t, 1, stub.count(), "回滚之后要撤销可能已经写进对端的 hook")
}

// 反面：探活阶段就失败（压根没写对端配置）时不必留凭据 —— 但留了也无害，
// 这里锁的是"注册表里确实没有残留"，避免回滚路径漏删。
func TestNodeServiceCreateProbeFailureLeavesNothing(t *testing.T) {
	stub := &unprovisionStub{}
	repo := newMemoryRepo()
	probe := &mockProbe{getServerConfigErr: errors.New("connection refused")}
	svc := newSvc(repo, probe)
	index := node.NewRetirementIndex(nil)
	coordinator := service.NewRetiredNodeCoordinator(index, stub)
	coordinator.SetDispatcher(func(f func()) { f() })
	svc.SetRetiredNodeCoordinator(coordinator)

	_, err := svc.Create(context.Background(), service.CreateNodeReq{
		Name: "n1", Host: "1.2.3.4", APIPort: 18080, APISecret: "s",
	})
	require.Error(t, err)
	require.Empty(t, repo.rows)
	require.Equal(t, 0, index.Len(), "没进注册表的节点没什么可退休的")
	require.Equal(t, 0, stub.count())
}

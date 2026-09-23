package node_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
)

// fakeRetirementRepo 记录落库调用，并可切换失败。
type fakeRetirementRepo struct {
	rows        []node.RetiredCredential
	loadErr     error
	saveErr     error
	markErr     error
	saves       int
	marks       []string
	lastMarked  node.RetiredCredential
	markedState string
}

func (r *fakeRetirementRepo) ListRetired(context.Context) ([]node.RetiredCredential, error) {
	if r.loadErr != nil {
		return nil, r.loadErr
	}
	return append([]node.RetiredCredential(nil), r.rows...), nil
}

func (r *fakeRetirementRepo) SaveRetired(_ context.Context, c node.RetiredCredential) error {
	r.saves++
	if r.saveErr != nil {
		return r.saveErr
	}
	r.rows = append(r.rows, c)
	return nil
}

func (r *fakeRetirementRepo) UpdateRetiredUnprovision(_ context.Context, uuid, state string, _ int, _ time.Time) error {
	r.marks = append(r.marks, uuid)
	r.markedState = state
	return r.markErr
}

func TestRetirementIndexLoadAllSkipsRowsWithoutUUID(t *testing.T) {
	repo := &fakeRetirementRepo{rows: []node.RetiredCredential{
		{MediaServerUUID: "b-uuid", UnprovisionState: node.RetireUnprovisionDone},
		{MediaServerUUID: ""}, // 脏行：没有 uuid 就无法被 hook 反查命中
		{MediaServerUUID: "a-uuid", UnprovisionState: node.RetireUnprovisionUnreachable},
	}}
	index := node.NewRetirementIndex(repo)
	require.NoError(t, index.LoadAll(context.Background()))
	require.Equal(t, 2, index.Len())

	cred, ok := index.Lookup("a-uuid")
	require.True(t, ok)
	require.Equal(t, node.RetireUnprovisionUnreachable, cred.UnprovisionState)
	// 结清的凭据不该被当成"还没处理"。
	done, ok := index.Lookup("b-uuid")
	require.True(t, ok)
	require.True(t, done.Resolved())
	require.False(t, cred.Resolved())

	// List 按 uuid 稳定排序，便于对账。
	list := index.List()
	require.Len(t, list, 2)
	require.Equal(t, "a-uuid", list[0].MediaServerUUID)
}

// ⛔ 关键降级语义：删行之后 `meta_node` 的那一行已经没了，此刻"记住这是谁"比
// "落库成功"更重要 —— 落库失败也必须写进内存索引，否则这个进程就彻底失去了
// 认出对端的能力，只能退回"陌生人 + 靠折叠忍"。
func TestRetirementIndexRemembersEvenWhenPersistenceFails(t *testing.T) {
	repo := &fakeRetirementRepo{saveErr: errors.New("db is down")}
	index := node.NewRetirementIndex(repo)

	err := index.Remember(context.Background(), node.RetiredCredential{
		MediaServerUUID: "ghost", Host: "192.168.10.220", APIPort: 21080,
	})
	require.Error(t, err, "落库失败必须冒泡给调用方去记审计")
	require.Equal(t, 1, repo.saves)
	cred, ok := index.Lookup("ghost")
	require.True(t, ok, "落库失败也不能丢掉内存里的认人能力")
	require.Equal(t, node.RetireUnprovisionPending, cred.UnprovisionState, "状态缺省为 pending")
	require.False(t, cred.RetiredAt.IsZero(), "退役时间要补上，否则凭据没法做时效判断")
}

func TestRetirementIndexRequiresUUID(t *testing.T) {
	index := node.NewRetirementIndex(&fakeRetirementRepo{})
	require.Error(t, index.Remember(context.Background(), node.RetiredCredential{}))
	_, ok := index.Lookup("")
	require.False(t, ok, "空 uuid 不是一次有效查询")
}

func TestRetirementIndexMarkUnprovisioned(t *testing.T) {
	repo := &fakeRetirementRepo{}
	index := node.NewRetirementIndex(repo)
	require.NoError(t, index.Remember(context.Background(), node.RetiredCredential{MediaServerUUID: "ghost"}))

	at := time.Date(2026, 9, 21, 17, 0, 0, 0, time.Local)
	require.NoError(t, index.MarkUnprovisioned(context.Background(), "ghost", node.RetireUnprovisionDone, 3, at))

	cred, _ := index.Lookup("ghost")
	require.Equal(t, node.RetireUnprovisionDone, cred.UnprovisionState)
	require.Equal(t, 3, cred.UnprovisionAttempts)
	require.NotNil(t, cred.LastAttemptAt)
	require.Equal(t, at, *cred.LastAttemptAt)
	require.Equal(t, []string{"ghost"}, repo.marks)
	require.Equal(t, node.RetireUnprovisionDone, repo.markedState, "状态要同步落库，重启后才不会重试")

	// 没有的 uuid：不落库、也不 panic（并发下凭据可能刚被别的路径处理过）。
	require.NoError(t, index.MarkUnprovisioned(context.Background(), "unknown", node.RetireUnprovisionDone, 1, at))
	require.Len(t, repo.marks, 1)
	require.Error(t, index.MarkUnprovisioned(context.Background(), "", node.RetireUnprovisionDone, 1, at))
}

// 未装配持久化（repo 为 nil）时索引仍要能工作：这是"降级但不崩"的底线。
func TestRetirementIndexWorksWithoutRepo(t *testing.T) {
	index := node.NewRetirementIndex(nil)
	require.NoError(t, index.LoadAll(context.Background()))
	require.NoError(t, index.Remember(context.Background(), node.RetiredCredential{MediaServerUUID: "ghost"}))
	require.NoError(t, index.MarkUnprovisioned(context.Background(), "ghost", node.RetireUnprovisionDone, 1, time.Now()))
	require.Equal(t, 1, index.Len())
}

// 凭据还原出的 Node 只带定位与鉴权信息，绝不携带"我还活着"的状态。
func TestRetiredCredentialNodeCarriesNoLiveState(t *testing.T) {
	cred := node.RetiredCredential{
		MediaServerUUID: "u", Name: "n", Host: "h", APIPort: 21080, APISecret: "s",
	}
	rebuilt := cred.Node()
	require.Equal(t, int64(0), rebuilt.ID, "退休凭据还原的 Node 不是注册表里的实体")
	require.Equal(t, "h", rebuilt.Host)
	require.Equal(t, 21080, rebuilt.APIPort)
	require.Equal(t, "s", rebuilt.APISecret)
	require.Equal(t, "u", rebuilt.MediaServerUUID)
	require.Equal(t, node.Node{}.State, rebuilt.State, "不得带任何状态，它进不了调度池")
	require.Equal(t, "http://h:21080/index/api", rebuilt.HTTPEndpoint())
}

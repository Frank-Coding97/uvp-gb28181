package gb28181

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	gbconfig "uvplatform.com/uvp-gb28181/app/gb28181/config"
	"uvplatform.com/uvp-gb28181/app/gb28181/zlm/node"
)

// seedRepo 只为 seed 链路准备的内存 Repo。
//
// ⛔ 刻意走**真 Registry**而不是打桩 Registry:本次改动的价值就在于
// "登记进库的那对端口到底是不是从端点实际配置来的"。若测试直接调
// resolveSeedRTPRangeWith，那它只证明了解析函数会返回探测值，证明不了
// seedDefaultNode 真的把探测值**用**上了 —— 把 seed 改回写死 30000/35000
// 时这种测试照样全绿（这正是补齐前实测到的漏洞）。
type seedRepo struct {
	mu     sync.Mutex
	nextID int64
	rows   map[int64]node.Node
}

func newSeedRepo() *seedRepo { return &seedRepo{rows: map[int64]node.Node{}} }

func (r *seedRepo) List(context.Context) ([]node.Node, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]node.Node, 0, len(r.rows))
	for _, n := range r.rows {
		out = append(out, n)
	}
	return out, nil
}

func (r *seedRepo) Get(_ context.Context, id int64) (*node.Node, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	n, ok := r.rows[id]
	if !ok {
		return nil, node.ErrNotFound
	}
	return &n, nil
}

func (r *seedRepo) Create(_ context.Context, n node.Node) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.nextID++
	n.ID = r.nextID
	r.rows[n.ID] = n
	return n.ID, nil
}

func (r *seedRepo) Update(_ context.Context, n node.Node) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.rows[n.ID]; !ok {
		return node.ErrNotFound
	}
	r.rows[n.ID] = n
	return nil
}

func (r *seedRepo) Delete(_ context.Context, id int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.rows, id)
	return nil
}

func (r *seedRepo) row(id int64) (node.Node, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	n, ok := r.rows[id]
	return n, ok
}

// seedTestConfig 固定一份 yaml 配置：含固定身份标识，模拟绿色包部署。
func seedTestConfig() gbconfig.Config {
	var cfg gbconfig.Config
	cfg.ZLM.Host = "127.0.0.1"
	cfg.ZLM.HTTPPort = 51004
	cfg.ZLM.Secret = "s3cret"
	cfg.ZLM.MediaServerID = "uvp-media-server-0001"
	return cfg
}

func readerOf(config map[string]string, err error) zlmConfigReader {
	return func(context.Context, *node.Node) (map[string]string, error) { return config, err }
}

// 首启 seed 必须**把端点实际配置登记进库**，而不是写死出厂默认：
// 写死 30000-35000 会让平台按 5000 个端口算容量，而绿色包实际只开 49 个
// （51014-51063）—— 调度从此永不判"接近满"，直到 ZLM 端口真的耗光、推流开始失败。
func TestSeedDefaultNode_UsesProbedRange(t *testing.T) {
	repo := newSeedRepo()
	reg := node.NewRegistry(repo)

	out, err := seedDefaultNode(context.Background(), reg, seedTestConfig(),
		readerOf(map[string]string{"rtp_proxy.port_range": "51014-51063"}, nil))
	require.NoError(t, err)

	require.Equal(t, "probe", out.RangeSource)
	require.Equal(t, 51014, out.RTPPortStart)
	require.Equal(t, 51063, out.RTPPortEnd)
	// 身份标识取配置固定值（绿色包契约），不能被探测流程弄丢。
	require.Equal(t, "uvp-media-server-0001", out.Node.MediaServerUUID)

	// 关键断言：**落到库里的**那对端口也是探测值（这条才会在把 seed 改回
	// 写死 30000/35000 时变红）。
	stored, ok := repo.row(out.Node.ID)
	require.True(t, ok, "seed 后库中应有该节点")
	require.Equal(t, 51014, stored.RTPPortStart)
	require.Equal(t, 51063, stored.RTPPortEnd)

	// 内存表也要一致（容量计算读的是内存）。
	require.Len(t, reg.List(), 1)
	require.Equal(t, 51014, reg.List()[0].RTPPortStart)
	require.Equal(t, 51063, reg.List()[0].RTPPortEnd)
}

// ZLM 还没起来 / 读失败 ⇒ 回退出厂默认，且 **绝不能让 seed 失败**。
// 平台比 ZLM 先启动是常态，seed 必须 best-effort。
func TestSeedDefaultNode_FallsBackWithoutFailing(t *testing.T) {
	repo := newSeedRepo()
	reg := node.NewRegistry(repo)

	out, err := seedDefaultNode(context.Background(), reg, seedTestConfig(),
		readerOf(nil, errors.New("connection refused")))
	require.NoError(t, err, "端点不可达不是 seed 的失败条件")

	require.Equal(t, "default", out.RangeSource)
	require.Equal(t, 30000, out.RTPPortStart)
	require.Equal(t, 35000, out.RTPPortEnd)

	stored, ok := repo.row(out.Node.ID)
	require.True(t, ok)
	require.Equal(t, 30000, stored.RTPPortStart)
	require.Equal(t, 35000, stored.RTPPortEnd)
	require.Len(t, reg.List(), 1)
}

// 探测时问的必须是配置里那台 ZLM（含 secret，否则会 401 而静默回退成默认）。
func TestResolveSeedRTPRange_ProbesConfiguredEndpoint(t *testing.T) {
	var probed *node.Node
	start, end, source := resolveSeedRTPRangeWith(context.Background(), seedTestConfig(),
		func(_ context.Context, target *node.Node) (map[string]string, error) {
			probed = target
			return map[string]string{"rtp_proxy.port_range": "51014-51063"}, nil
		})

	require.Equal(t, 51014, start)
	require.Equal(t, 51063, end)
	require.Equal(t, "probe", source)
	require.NotNil(t, probed)
	require.Equal(t, "127.0.0.1", probed.Host)
	require.Equal(t, 51004, probed.APIPort)
	require.Equal(t, "s3cret", probed.APISecret)
}

// 端点答了但没给可用的范围（旧版 ZLM / 手改坏 / 值非法）⇒ 同样回退，不写脏数据。
func TestResolveSeedRTPRange_FallsBackWhenEndpointValueUnusable(t *testing.T) {
	for name, config := range map[string]map[string]string{
		"键缺失":      {"http.port": "51004"},
		"值为空":      {"rtp_proxy.port_range": ""},
		"全零":       {"rtp_proxy.port_range": "0-0"},
		"起止颠倒":     {"rtp_proxy.port_range": "51063-51014"},
		"超出 65535": {"rtp_proxy.port_range": "1-65536"},
	} {
		t.Run(name, func(t *testing.T) {
			start, end, source := resolveSeedRTPRangeWith(context.Background(), seedTestConfig(),
				readerOf(config, nil))
			require.Equal(t, 30000, start)
			require.Equal(t, 35000, end)
			require.Equal(t, "default", source)
		})
	}
}

// 探测超时必须短：ZLM 比平台晚起是常态，seed 不能把它变成启动阻塞点。
func TestSeedRTPRangeProbeTimeout_StaysShort(t *testing.T) {
	require.LessOrEqual(t, seedRTPRangeProbeTimeout, 5*time.Second)
	start, end := seedRTPRangeFallback()
	require.Equal(t, 30000, start)
	require.Equal(t, 35000, end, "回退值必须与历史行为一致，不许写 0")
}

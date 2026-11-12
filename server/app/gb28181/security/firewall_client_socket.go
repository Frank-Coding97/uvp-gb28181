package security

import (
	"fmt"
	"os"
	"time"
)

// SocketPresenceClient 把「特权防火墙代理根本没部署/没在跑」与「代理在跑但操作失败」
// 区分开。
//
// ⛔⛔ 为什么需要它（2026-10-20 老板报障：绿色包里开启保护模式直接报
//
//	  `dial unix /run/uvp/firewall-agent.sock: connect: no such file or directory`）：
//
//		UnixFirewallClient.Capability() 在 Linux 上**恒为 supported**（那是刻意的：
//		部署方声称自己有代理，只是暂时连不上，必须让操作员看见"同步失败"而不是静默降级）。
//		但绿色包（单机包，解压即用、非 root 启动）**根本不带这个代理**，于是：
//		切到保护/严格模式 → UpdatePolicy → agent.Reconcile → 拨号失败 → 400 原始英文错误，
//		操作员看到的是"模式开不了"，而真实情况是"内核层没有代理，应用层拦截一直有效"。
//
// ⇒ 判据用 **socket 文件是否存在**：
//   - 存在：交给底层客户端，普通错误照旧向上抛（不吞、不静默）；
//   - 不存在：明确按 unsupported 处理（与 Windows/macOS 同一条已验证路径）——
//     保护模式能开、封禁标 agent_state=unsupported、应用层准入继续丢弃报文，
//     前端显示"主机防火墙 降级"，而不是一句让人无从下手的英文 dial 错误。
//
// ⛔ 刻意**不缓存**探测结果：每次问一遍文件系统（两次 stat），这样代理随后被
//
//	systemd/ctl.sh 拉起来时**无需重启后端**就自动恢复内核级封禁 —— 反过来缓存
//	会让"代理晚启动 10 秒"变成"这台机器永远只做应用层"。
type SocketPresenceClient struct {
	base   FirewallAgentClient
	socket string
	stat   func(string) (os.FileInfo, error)
}

// NewSocketPresenceClient 包一层 socket 存在性探测。socket 为空或 base 为空时
// 退化成 unsupported 客户端，避免调用方拿到 nil 恐慌。
func NewSocketPresenceClient(base FirewallAgentClient, socketPath string) *SocketPresenceClient {
	return &SocketPresenceClient{base: base, socket: socketPath, stat: os.Stat}
}

func (c *SocketPresenceClient) present() bool {
	if c.base == nil || c.socket == "" {
		return false
	}
	info, err := c.stat(c.socket)
	return err == nil && !info.IsDir()
}

// absentError 必须 %w 包住 ErrFirewallUnsupported：runtime 用 errors.Is 判定
// 「代理不支持」并据此把封禁标成 unsupported，而不是标成同步失败。
func (c *SocketPresenceClient) absentError() error {
	return fmt.Errorf("%w: firewall agent socket %q is absent", ErrFirewallUnsupported, c.socket)
}

// AbsentReason 是给前端看的一句话（会被拼在"应用层拦截仍生效"前面）。
const AbsentReason = "主机防火墙代理未运行（未部署或以非 root 启动）"

func (c *SocketPresenceClient) Capability() AgentCapabilityState {
	if !c.present() {
		return AgentCapabilityUnsupported
	}
	if provider, ok := c.base.(FirewallAgentCapabilityProvider); ok {
		return provider.Capability()
	}
	return AgentCapabilityUnknown
}

func (c *SocketPresenceClient) Ban(decision BanDecision) error {
	if !c.present() {
		return c.absentError()
	}
	return c.base.Ban(decision)
}

func (c *SocketPresenceClient) Unban(sourceIP string) error {
	if !c.present() {
		return c.absentError()
	}
	return c.base.Unban(sourceIP)
}

func (c *SocketPresenceClient) Reconcile(decisions []BanDecision) error {
	if !c.present() {
		return c.absentError()
	}
	if reconciler, ok := c.base.(ReconcileCapable); ok {
		return reconciler.Reconcile(decisions)
	}
	// 底层不支持整表下发时退回逐条封禁（与 runtime 自己的兜底一致）。
	for _, decision := range decisions {
		if err := c.base.Ban(decision); err != nil {
			return err
		}
	}
	return nil
}

func (c *SocketPresenceClient) Status() AgentStatus {
	if !c.present() {
		return AgentStatus{
			Connected:  false,
			Capability: AgentCapabilityUnsupported,
			LastError:  AbsentReason,
			CheckedAt:  time.Now(),
		}
	}
	return c.base.Status()
}

var _ FirewallAgentClient = (*SocketPresenceClient)(nil)
var _ FirewallAgentCapabilityProvider = (*SocketPresenceClient)(nil)
var _ ReconcileCapable = (*SocketPresenceClient)(nil)

// ReconcileCapable 固定住 runtime.reconcileAgent 依赖的「整表下发」能力，
// 避免以后有人给这层包装漏掉 Reconcile（漏了会退化成逐条 Ban，语义不同）。
type ReconcileCapable interface {
	Reconcile([]BanDecision) error
}

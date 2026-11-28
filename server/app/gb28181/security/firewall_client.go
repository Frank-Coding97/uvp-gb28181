package security

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net"
	"strings"
	"time"
)

type UnixFirewallClient struct {
	socketPath string
	timeout    time.Duration
	// sipPort 随每次请求下发，代理据此把 nft 规则里的 dport 对齐到平台**当前**
	// 生效的 SIP 端口。
	//
	// ⛔⛔ 为什么不能只在代理启动时用命令行 `--sip-port` 定死（2026-10-20 定）：
	//   绿色包的 SIP 端口是**引导页写进 gb_sip_config 的**，而引导页在
	//   `ctl.sh start` **之后**才跑 ⇒ 代理启动那一刻的端口必然可能是旧的。
	//   端口不一致时 nft 规则匹配不到真实报文 ⇒ 界面显示"内核封禁已生效"、
	//   内核却一条都没拦（本仓最忌讳的静默错配）。
	//   ⇒ 端口由平台随请求下发，代理发现变化就重建规则表，几秒内自愈。
	sipPort uint16
}

func NewUnixFirewallClient(socketPath string, timeout time.Duration) *UnixFirewallClient {
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	return &UnixFirewallClient{socketPath: socketPath, timeout: timeout}
}

// SetSIPPort 设置下发给代理的 SIP 端口；0 表示"平台还不知道"，代理保持原样。
func (c *UnixFirewallClient) SetSIPPort(port uint16) { c.sipPort = port }

func (*UnixFirewallClient) Capability() AgentCapabilityState {
	return AgentCapabilitySupported
}

func (c *UnixFirewallClient) Ban(decision BanDecision) error {
	_, err := c.call(unixFirewallRequest{Action: "ban", Decision: decision})
	return err
}

func (c *UnixFirewallClient) Unban(sourceIP string) error {
	_, err := c.call(unixFirewallRequest{Action: "unban", SourceIP: sourceIP})
	return err
}

func (c *UnixFirewallClient) Status() AgentStatus {
	response, err := c.call(unixFirewallRequest{Action: "status"})
	if err != nil {
		return AgentStatus{Connected: false, Capability: AgentCapabilitySupported, LastError: err.Error(), CheckedAt: time.Now()}
	}
	if response.Status.Capability == "" || response.Status.Capability == AgentCapabilityUnknown {
		response.Status.Capability = AgentCapabilitySupported
	}
	return response.Status
}

func (c *UnixFirewallClient) Reconcile(decisions []BanDecision) error {
	_, err := c.call(unixFirewallRequest{Action: "reconcile", Decisions: decisions})
	return err
}

type unixFirewallRequest struct {
	Action    string        `json:"action"`
	Decision  BanDecision   `json:"decision"`
	SourceIP  string        `json:"sourceIp"`
	Decisions []BanDecision `json:"decisions,omitempty"`
	SIPPort   uint16        `json:"sipPort,omitempty"`
}

type unixFirewallResponse struct {
	OK     bool        `json:"ok"`
	Error  string      `json:"error,omitempty"`
	Status AgentStatus `json:"status"`
}

func (c *UnixFirewallClient) call(req unixFirewallRequest) (unixFirewallResponse, error) {
	var response unixFirewallResponse
	if strings.TrimSpace(c.socketPath) == "" {
		return response, errors.New("firewall socket path is empty")
	}
	// 端口随每条请求走：代理侧比对不一致就重建规则表（见 sipPort 字段处的注释）。
	req.SIPPort = c.sipPort
	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	defer cancel()
	dialer := net.Dialer{}
	conn, err := dialer.DialContext(ctx, "unix", c.socketPath)
	if err != nil {
		return response, err
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(c.timeout))
	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return response, err
	}
	if err := json.NewDecoder(bufio.NewReader(conn)).Decode(&response); err != nil {
		return response, err
	}
	if !response.OK {
		if response.Error == "" {
			response.Error = "firewall agent rejected request"
		}
		return response, errors.New(response.Error)
	}
	return response, nil
}

var _ FirewallAgentClient = (*UnixFirewallClient)(nil)
var _ FirewallAgentCapabilityProvider = (*UnixFirewallClient)(nil)

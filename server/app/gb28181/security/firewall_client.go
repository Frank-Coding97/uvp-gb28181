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
}

func NewUnixFirewallClient(socketPath string, timeout time.Duration) *UnixFirewallClient {
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	return &UnixFirewallClient{socketPath: socketPath, timeout: timeout}
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
		return AgentStatus{Connected: false, LastError: err.Error(), CheckedAt: time.Now()}
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
	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	defer cancel()
	dialer := net.Dialer{}
	conn, err := dialer.DialContext(ctx, "unix", c.socketPath)
	if err != nil {
		return response, err
	}
	defer conn.Close()
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

package firewall

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os/exec"
	"strings"
	"time"

	"uvplatform.cn/uvp-gb28181/app/gb28181/security"
)

type CommandRunner interface {
	Run(context.Context, string, []string, []byte) ([]byte, error)
}

type execCommandRunner struct{}

func (execCommandRunner) Run(ctx context.Context, name string, args []string, input []byte) ([]byte, error) {
	command := exec.CommandContext(ctx, name, args...)
	if len(input) > 0 {
		command.Stdin = strings.NewReader(string(input))
	}
	output, err := command.CombinedOutput()
	if err != nil {
		return output, fmt.Errorf("%w: %s", err, strings.TrimSpace(string(output)))
	}
	return output, nil
}

type NftConfig struct {
	Interface   string
	Destination net.IP
	Port        uint16
	Allowlist   []net.IPNet
}

type NftBackend struct {
	runner CommandRunner
	cfg    NftConfig
}

func NewNftBackend(ctx context.Context, runner CommandRunner, cfg NftConfig) (*NftBackend, error) {
	if runner == nil {
		runner = execCommandRunner{}
	}
	if err := validateNftConfig(cfg); err != nil {
		return nil, err
	}
	b := &NftBackend{runner: runner, cfg: cfg}
	if err := b.ensure(ctx); err != nil {
		return nil, err
	}
	return b, nil
}

func DetectNftConfig(ctx context.Context, runner CommandRunner, port uint16, allowlist []net.IPNet) (NftConfig, error) {
	if runner == nil {
		runner = execCommandRunner{}
	}
	output, err := runner.Run(ctx, "ip", []string{"-j", "route", "get", "1.1.1.1"}, nil)
	if err != nil {
		return NftConfig{}, fmt.Errorf("detect default route: %w", err)
	}
	var routes []struct {
		Dev     string `json:"dev"`
		Prefsrc string `json:"prefsrc"`
	}
	if err := json.Unmarshal(output, &routes); err != nil || len(routes) == 0 {
		return NftConfig{}, errors.New("default route response is invalid")
	}
	destination := net.ParseIP(routes[0].Prefsrc)
	if destination == nil {
		return NftConfig{}, errors.New("default route has no local destination")
	}
	return NftConfig{Interface: routes[0].Dev, Destination: destination, Port: port, Allowlist: allowlist}, nil
}

func (b *NftBackend) ensure(ctx context.Context) error {
	_, err := b.runner.Run(ctx, "nft", []string{"-j", "list", "table", "inet", "uvp_sip_guard"}, nil)
	script := b.tableScript()
	if err == nil {
		script = "delete table inet uvp_sip_guard\n" + script
	}
	_, err = b.runner.Run(ctx, "nft", []string{"-f", "-"}, []byte(script))
	if err != nil {
		return fmt.Errorf("create uvp_sip_guard table: %w", err)
	}
	return nil
}

func (b *NftBackend) Add(sourceIP string, expiresAt time.Time) error {
	ip, err := security.ValidateSource(sourceIP)
	if err != nil {
		return err
	}
	if b.isAllowlisted(ip) {
		return ErrAllowlisted
	}
	if expiresAt.IsZero() {
		return b.addElement(ip, "")
	}
	remaining := time.Until(expiresAt)
	if remaining <= 0 {
		return errors.New("firewall rule already expired")
	}
	return b.addElement(ip, fmt.Sprintf(" timeout %ds", max(1, int(remaining/time.Second))))
}

func (b *NftBackend) addElement(ip net.IP, timeout string) error {
	set, err := b.sourceSet(ip)
	if err != nil {
		return err
	}
	existing, err := b.List()
	if err != nil {
		return err
	}
	input := fmt.Sprintf("add element inet uvp_sip_guard %s { %s%s }\n", set, ip.String(), timeout)
	for _, current := range existing {
		if current == ip.String() {
			input = fmt.Sprintf("delete element inet uvp_sip_guard %s { %s }\nadd element inet uvp_sip_guard %s { %s%s }\n", set, ip.String(), set, ip.String(), timeout)
			break
		}
	}
	if _, err := b.runner.Run(context.Background(), "nft", []string{"-f", "-"}, []byte(input)); err != nil {
		return err
	}
	return nil
}

func (b *NftBackend) Remove(sourceIP string) error {
	ip, err := security.ValidateSource(sourceIP)
	if err != nil {
		return err
	}
	set, err := b.sourceSet(ip)
	if err != nil {
		return err
	}
	input := fmt.Sprintf("delete element inet uvp_sip_guard %s { %s }\n", set, ip.String())
	if _, err := b.runner.Run(context.Background(), "nft", []string{"-f", "-"}, []byte(input)); err != nil {
		// nft reports a missing element as an error; removal is intentionally idempotent.
		if strings.Contains(strings.ToLower(err.Error()), "no such file") || strings.Contains(strings.ToLower(err.Error()), "not found") {
			return nil
		}
		return err
	}
	return nil
}

func (b *NftBackend) List() ([]string, error) {
	set := "blocked_v4"
	if b.cfg.Destination.To4() == nil {
		set = "blocked_v6"
	}
	output, err := b.runner.Run(context.Background(), "nft", []string{"-j", "list", "set", "inet", "uvp_sip_guard", set}, nil)
	if err != nil {
		return nil, err
	}
	var document interface{}
	if err := json.Unmarshal(output, &document); err != nil {
		return nil, err
	}
	values := make([]string, 0)
	collectNftValues(document, &values)
	return values, nil
}

func (b *NftBackend) isAllowlisted(ip net.IP) bool {
	for _, network := range b.cfg.Allowlist {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

func (b *NftBackend) sourceSet(ip net.IP) (string, error) {
	if (ip.To4() != nil) != (b.cfg.Destination.To4() != nil) {
		return "", errors.New("source address family does not match configured destination")
	}
	if ip.To4() != nil {
		return "blocked_v4", nil
	}
	return "blocked_v6", nil
}

func (b *NftBackend) tableScript() string {
	destination := b.cfg.Destination.String()
	family := "ipv4_addr"
	blocked := "blocked_v4"
	protocol := "ip"
	if b.cfg.Destination.To4() == nil {
		family = "ipv6_addr"
		blocked = "blocked_v6"
		protocol = "ip6"
	}
	var builder strings.Builder
	fmt.Fprintf(&builder, "add table inet uvp_sip_guard\nadd set inet uvp_sip_guard %s { type %s; flags timeout; }\n", blocked, family)
	fmt.Fprintf(&builder, "add chain inet uvp_sip_guard guard_prerouting { type filter hook prerouting priority raw; policy accept; }\n")
	fmt.Fprintf(&builder, "add rule inet uvp_sip_guard guard_prerouting iifname %q %s daddr %s %s saddr @%s udp dport %d counter drop\n", b.cfg.Interface, protocol, destination, protocol, blocked, b.cfg.Port)
	fmt.Fprintf(&builder, "add rule inet uvp_sip_guard guard_prerouting iifname %q %s daddr %s %s saddr @%s tcp dport %d counter drop\n", b.cfg.Interface, protocol, destination, protocol, blocked, b.cfg.Port)
	return builder.String()
}

func validateNftConfig(cfg NftConfig) error {
	if cfg.Interface == "" || strings.ContainsAny(cfg.Interface, " \t\n\"'") {
		return errors.New("invalid firewall interface")
	}
	if cfg.Destination == nil || cfg.Destination.IsUnspecified() || cfg.Destination.IsMulticast() {
		return errors.New("invalid firewall destination")
	}
	if cfg.Port == 0 {
		return errors.New("invalid firewall port")
	}
	return nil
}

func collectNftValues(value interface{}, values *[]string) {
	switch current := value.(type) {
	case map[string]interface{}:
		for key, child := range current {
			if key == "val" {
				if text, ok := child.(string); ok && net.ParseIP(text) != nil {
					*values = append(*values, text)
				}
			}
			collectNftValues(child, values)
		}
	case []interface{}:
		for _, child := range current {
			collectNftValues(child, values)
		}
	}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

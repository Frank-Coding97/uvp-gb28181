package firewall

import (
	"crypto/sha256"
	"encoding/hex"
	"net"
	"net/netip"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"github.com/google/uuid"
)

// ValidateIdentity checks the immutable package identity used to name owned
// rules. Removal uses this smaller contract so a stale network or SIP config
// cannot make cleanup impossible.
func ValidateIdentity(inputs Inputs) error {
	if strings.TrimSpace(inputs.CanonicalRoot) == "" || !filepath.IsAbs(inputs.CanonicalRoot) {
		return errorFor(ReasonInvalidInput)
	}
	if !inputs.ReleaseVerified {
		return errorFor(ReasonReleaseUnverified)
	}
	if !validAbsoluteProgram(inputs.BackendExe) || !validAbsoluteProgram(inputs.MediaExe) {
		return errorFor(ReasonInvalidExecutable)
	}
	return nil
}

// ValidateInputs checks the complete trust boundary supplied by the verified
// launcher loader. It does not inspect the network or mutate the firewall.
func ValidateInputs(inputs Inputs) error {
	if err := ValidateIdentity(inputs); err != nil {
		return err
	}
	if _, ok := validSIPAddr(inputs.SIP.ListenIP); !ok || inputs.SIP.Port < 1 || inputs.SIP.Port > 65535 {
		return errorFor(ReasonInvalidSIP)
	}
	if len(inputs.MediaListeners) == 0 {
		return errorFor(ReasonMediaInvalid)
	}
	for _, listener := range inputs.MediaListeners {
		if err := validateMediaListener(listener.Network, listener.Address); err != nil {
			return err
		}
	}
	return nil
}

func validAbsoluteProgram(path string) bool {
	path = strings.TrimSpace(path)
	return path != "" && filepath.IsAbs(path) && filepath.Clean(path) == path
}

func validSIPAddr(raw string) (netip.Addr, bool) {
	raw = strings.TrimSpace(raw)
	addr, err := netip.ParseAddr(raw)
	if err != nil || !addr.Is4() || raw != addr.String() {
		return netip.Addr{}, false
	}
	if addr.IsLoopback() || addr.IsMulticast() || addr == netip.MustParseAddr("255.255.255.255") {
		return netip.Addr{}, false
	}
	return addr, true
}

func validateMediaListener(network, address string) error {
	network = strings.ToLower(strings.TrimSpace(network))
	if network != "tcp" && network != "udp" {
		return errorFor(ReasonMediaInvalid)
	}
	host, portText, err := net.SplitHostPort(strings.TrimSpace(address))
	if err != nil || strings.TrimSpace(host) == "" {
		return errorFor(ReasonMediaInvalid)
	}
	if port, err := strconv.Atoi(portText); err != nil || port < 1 || port > 65535 {
		return errorFor(ReasonMediaInvalid)
	}
	host = strings.Trim(host, "[]")
	if addr, err := netip.ParseAddr(host); err != nil || !addr.Is4() {
		return errorFor(ReasonMediaInvalid)
	}
	return nil
}

// SelectInterface revalidates one adapter returned by the platform adapter.
// It accepts only the exact requested GUID and a concrete SIP address owned by
// that adapter.
func SelectInterface(sip SIPConfig, interfaces []InterfaceInfo, requestedID string) (InterfaceInfo, error) {
	requested, ok := normalizeGUID(requestedID)
	if !ok {
		return InterfaceInfo{}, errorFor(ReasonInvalidInput)
	}
	sipAddr, ok := validSIPAddr(sip.ListenIP)
	if !ok || sip.Port < 1 || sip.Port > 65535 {
		return InterfaceInfo{}, errorFor(ReasonInvalidSIP)
	}

	var chosen InterfaceInfo
	matches := 0
	for _, candidate := range interfaces {
		id, valid := normalizeGUID(candidate.ID)
		if !valid || id != requested {
			continue
		}
		matches++
		chosen = candidate
	}
	switch matches {
	case 0:
		return InterfaceInfo{}, errorFor(ReasonInterfaceMissing)
	case 1:
	default:
		return InterfaceInfo{}, errorFor(ReasonInterfaceAmbiguous)
	}
	if strings.TrimSpace(chosen.Alias) == "" {
		return InterfaceInfo{}, errorFor(ReasonInterfaceMissing)
	}
	if !chosen.Up {
		return InterfaceInfo{}, errorFor(ReasonInterfaceDown)
	}
	if !chosen.Private {
		return InterfaceInfo{}, errorFor(ReasonInterfaceNotPrivate)
	}
	if chosen.Loopback {
		return InterfaceInfo{}, errorFor(ReasonInterfaceLoopback)
	}

	seenIPv4 := false
	wildcard := sipAddr.IsUnspecified()
	for _, raw := range chosen.IPv4 {
		addr, valid := parseIPv4(strings.TrimSpace(raw))
		if !valid || addr.IsLoopback() || addr.IsUnspecified() || addr.IsMulticast() || addr == netip.MustParseAddr("255.255.255.255") {
			continue
		}
		seenIPv4 = true
		if wildcard || addr == sipAddr {
			return chosen, nil
		}
	}
	if !seenIPv4 {
		return InterfaceInfo{}, errorFor(ReasonInterfaceNoIPv4)
	}
	return InterfaceInfo{}, errorFor(ReasonSIPNotOnInterface)
}

func normalizeGUID(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "{")
	raw = strings.TrimSuffix(raw, "}")
	parsed, err := uuid.Parse(raw)
	if err != nil {
		return "", false
	}
	return strings.ToLower(parsed.String()), true
}

// BuildRules creates exactly four instance-owned rules. The media rule port
// sets include every configured listener for that protocol and always include
// the RTP allocation range for both TCP and UDP.
func BuildRules(inputs Inputs, selected InterfaceInfo) ([]Rule, error) {
	if err := ValidateInputs(inputs); err != nil {
		return nil, err
	}
	checked, err := SelectInterface(inputs.SIP, []InterfaceInfo{selected}, selected.ID)
	if err != nil {
		return nil, err
	}
	names := RuleNames(inputs.CanonicalRoot)
	group := RuleGroup(inputs.CanonicalRoot)
	if len(names) != 4 || group == "" {
		return nil, errorFor(ReasonInvalidInput)
	}

	ports := map[string]map[int]struct{}{"tcp": {}, "udp": {}}
	for _, listener := range inputs.MediaListeners {
		network := strings.ToLower(strings.TrimSpace(listener.Network))
		_, portText, _ := net.SplitHostPort(strings.TrimSpace(listener.Address))
		port, _ := strconv.Atoi(portText)
		ports[network][port] = struct{}{}
	}
	tcpPorts := joinPorts(ports["tcp"])
	udpPorts := joinPorts(ports["udp"])
	common := func(name, role, protocol, program, localPort string) Rule {
		return Rule{
			Name:                name,
			Group:               group,
			Role:                role,
			Protocol:            protocol,
			Program:             program,
			LocalPort:           localPort,
			Direction:           "Inbound",
			Action:              "Allow",
			Profile:             "Private",
			RemoteAddress:       "LocalSubnet",
			InterfaceAlias:      checked.Alias,
			EdgeTraversalPolicy: "Block",
			Enabled:             true,
		}
	}
	return []Rule{
		common(names[0], "backend", "TCP", inputs.BackendExe, strconv.Itoa(inputs.SIP.Port)),
		common(names[1], "backend", "UDP", inputs.BackendExe, strconv.Itoa(inputs.SIP.Port)),
		common(names[2], "media", "TCP", inputs.MediaExe, appendRTPRange(tcpPorts)),
		common(names[3], "media", "UDP", inputs.MediaExe, appendRTPRange(udpPorts)),
	}, nil
}

func appendRTPRange(ports string) string {
	if ports == "" {
		return "30000-35000"
	}
	return ports + ",30000-35000"
}

func joinPorts(values map[int]struct{}) string {
	ports := make([]int, 0, len(values))
	for port := range values {
		ports = append(ports, port)
	}
	sort.Ints(ports)
	result := make([]string, 0, len(ports))
	for _, port := range ports {
		result = append(result, strconv.Itoa(port))
	}
	return strings.Join(result, ",")
}

// RuleNames returns deterministic owned names for an absolute instance root.
// Invalid roots return nil; callers handling user input must validate first.
func RuleNames(root string) []string {
	hash, ok := instanceHash(root)
	if !ok {
		return nil
	}
	prefix := "UVP-" + hash + "-"
	return []string{
		prefix + "backend-tcp",
		prefix + "backend-udp",
		prefix + "media-tcp",
		prefix + "media-udp",
	}
}

// RuleGroup returns the only group accepted for this installation's rules.
// Keeping the product marker and the instance hash together lets the adapter
// distinguish an owned rule from an unrelated rule with the same name.
func RuleGroup(root string) string {
	hash, ok := instanceHash(root)
	if !ok {
		return ""
	}
	return "UVP-GB28181-Firewall-" + hash
}

func ruleGroupForName(name string) string {
	const prefix = "UVP-"
	if !strings.HasPrefix(name, prefix) {
		return ""
	}
	rest := strings.TrimPrefix(name, prefix)
	parts := strings.SplitN(rest, "-", 2)
	if len(parts) != 2 || len(parts[0]) != 32 {
		return ""
	}
	switch parts[1] {
	case "backend-tcp", "backend-udp", "media-tcp", "media-udp":
	default:
		return ""
	}
	for _, character := range parts[0] {
		if !((character >= '0' && character <= '9') || (character >= 'a' && character <= 'f')) {
			return ""
		}
	}
	return "UVP-GB28181-Firewall-" + parts[0]
}

func instanceHash(root string) (string, bool) {
	root = strings.TrimSpace(root)
	if root == "" || !filepath.IsAbs(root) {
		return "", false
	}
	root, err := filepath.Abs(filepath.Clean(root))
	if err != nil {
		return "", false
	}
	if runtime.GOOS == "windows" {
		root = strings.ToLower(root)
	}
	sum := sha256.Sum256([]byte(root))
	return hex.EncodeToString(sum[:16]), true
}

package firewall

import (
	"context"
	"errors"
	"net/netip"
	"sort"
	"strings"

	"uvplatform.cn/uvp-gb28181/internal/standalone"
)

// Action identifies the read or mutation requested by the launcher.
type Action string

const (
	ActionStatus Action = "status"
	ActionApply  Action = "apply"
	ActionRemove Action = "remove"
)

// Reason is a bounded diagnostic label. It deliberately excludes command
// output, file contents, credentials, and operating-system error text.
type Reason string

const (
	ReasonInvalidInput             Reason = "invalid_input"
	ReasonReleaseUnverified        Reason = "release_unverified"
	ReasonInvalidExecutable        Reason = "invalid_executable"
	ReasonInvalidSIP               Reason = "sip_invalid"
	ReasonSIPNotOnInterface        Reason = "sip_address_not_on_interface"
	ReasonMediaInvalid             Reason = "media_invalid"
	ReasonInterfaceMissing         Reason = "interface_missing"
	ReasonInterfaceAmbiguous       Reason = "interface_ambiguous"
	ReasonInterfaceDown            Reason = "interface_down"
	ReasonInterfaceNotPrivate      Reason = "interface_not_private"
	ReasonInterfaceLoopback        Reason = "interface_loopback"
	ReasonInterfaceNoIPv4          Reason = "interface_no_ipv4"
	ReasonInstallationIncomplete   Reason = "installation_incomplete"
	ReasonConfigurationUnavailable Reason = "configuration_unavailable"
	ReasonDatabaseUnavailable      Reason = "database_unavailable"
	ReasonSIPConfigMissing         Reason = "sip_config_missing"
	ReasonAdapterUnavailable       Reason = "adapter_unavailable"
	ReasonPermissionDenied         Reason = "permission_denied"
	ReasonReadbackFailed           Reason = "readback_failed"
	ReasonOperationFailed          Reason = "operation_failed"
	ReasonUnsupportedPlatform      Reason = "unsupported_platform"
	ReasonConfirmationRequired     Reason = "confirmation_required"
	ReasonElevationCanceled        Reason = "elevation_canceled"
	ReasonElevationFailed          Reason = "elevation_failed"
	ReasonRuleDrift                Reason = "rule_drift"
	ReasonRuleConflict             Reason = "rule_conflict"
	ReasonPotentialBlock           Reason = "potential_block"
	ReasonEffectiveBlockRule       Reason = "effective_block_rule"
	ReasonDiagnosticTruncated      Reason = "diagnostic_truncated"
)

// Error is an intentionally bounded firewall operation error.
type Error struct {
	Reason Reason
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	return "firewall: " + string(e.Reason)
}

func errorFor(reason Reason) error {
	return &Error{Reason: reason}
}

// NewError creates a bounded operation error for callers outside this package.
func NewError(reason Reason) error {
	return errorFor(reason)
}

func reasonOf(err error) Reason {
	var bounded *Error
	if errors.As(err, &bounded) && bounded != nil {
		return bounded.Reason
	}
	if errors.Is(err, context.Canceled) {
		return ReasonOperationFailed
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return ReasonOperationFailed
	}
	return ReasonOperationFailed
}

// ReasonOf extracts the bounded reason without exposing an operating-system
// error or command output.
func ReasonOf(err error) Reason {
	return reasonOf(err)
}

// Inputs are the verified local package inputs used to construct rules. The
// loader must never populate secret values here.
type Inputs struct {
	CanonicalRoot   string                     `json:"-"`
	ReleaseID       string                     `json:"-"`
	BackendExe      string                     `json:"-"`
	MediaExe        string                     `json:"-"`
	ReleaseVerified bool                       `json:"-"`
	SIP             SIPConfig                  `json:"sip"`
	MediaListeners  []standalone.MediaListener `json:"media_listeners"`
}

// SIPConfig contains only the fields needed by the firewall policy.
type SIPConfig struct {
	ListenIP string `json:"listen_ip"`
	Port     int    `json:"port"`
}

// InterfaceInfo is the small, revalidated network adapter view accepted by the
// policy. IPv4 entries are textual to keep adapter JSON and tests simple.
type InterfaceInfo struct {
	ID       string   `json:"id"`
	Alias    string   `json:"alias"`
	Up       bool     `json:"up"`
	Private  bool     `json:"private"`
	Loopback bool     `json:"loopback"`
	IPv4     []string `json:"ipv4"`
}

// Rule is a complete expected Windows firewall rule. The PowerShell adapter
// treats a non-empty Name as owned only when it matches the deterministic
// instance name.
type Rule struct {
	Name                string `json:"name"`
	Group               string `json:"group"`
	Role                string `json:"role"`
	Protocol            string `json:"protocol"`
	Program             string `json:"program"`
	LocalPort           string `json:"local_port"`
	Direction           string `json:"direction"`
	Action              string `json:"action"`
	Profile             string `json:"profile"`
	RemoteAddress       string `json:"remote_address"`
	InterfaceAlias      string `json:"interface_alias"`
	EdgeTraversalPolicy string `json:"edge_traversal_policy"`
	Enabled             bool   `json:"enabled"`
}

// RuleState is safe to return to the caller and contains no OS command output.
type RuleState struct {
	Name     string `json:"name"`
	Group    string `json:"group,omitempty"`
	Present  bool   `json:"present"`
	Matches  bool   `json:"matches"`
	Conflict bool   `json:"conflict,omitempty"`

	// ExternalBlock marks a rule returned by the bounded ActiveStore scan. It
	// is diagnostic data only and is never an owned mutation target.
	ExternalBlock bool `json:"external_block,omitempty"`
	// PotentialBlock means the rule shares the package's executable/port scope
	// but has a constraint the adapter cannot prove from local state.
	PotentialBlock bool `json:"potential_block,omitempty"`
	// DiagnosticsTruncated means the bounded ActiveStore scan found more
	// package candidates than it can safely return.
	DiagnosticsTruncated bool `json:"diagnostics_truncated,omitempty"`
	// DetailTruncated means one normalized external field exceeded the response
	// bound; the caller must not treat that readback as complete.
	DetailTruncated bool `json:"detail_truncated,omitempty"`

	// The following fields are bounded, normalized details for an external
	// block. They intentionally omit command output and security identities.
	Program            string `json:"program,omitempty"`
	Protocol           string `json:"protocol,omitempty"`
	LocalPort          string `json:"local_port,omitempty"`
	RemotePort         string `json:"remote_port,omitempty"`
	Enabled            bool   `json:"enabled,omitempty"`
	Direction          string `json:"direction,omitempty"`
	Action             string `json:"action,omitempty"`
	Profile            string `json:"profile,omitempty"`
	LocalAddress       string `json:"local_address,omitempty"`
	RemoteAddress      string `json:"remote_address,omitempty"`
	InterfaceAlias     string `json:"interface_alias,omitempty"`
	InterfaceType      string `json:"interface_type,omitempty"`
	Service            string `json:"service,omitempty"`
	AppPackage         string `json:"app_package,omitempty"`
	IdentityConstraint bool   `json:"identity_constraint,omitempty"`
	UnknownConstraints bool   `json:"unknown_constraints,omitempty"`
}

// Result is the bounded result of one operation.
type Result struct {
	Action      Action      `json:"action"`
	Success     bool        `json:"success"`
	Converged   bool        `json:"converged"`
	InterfaceID string      `json:"interface_id,omitempty"`
	Rules       []RuleState `json:"rules,omitempty"`
	Warnings    []Reason    `json:"warnings,omitempty"`
	Reason      Reason      `json:"reason,omitempty"`
}

// Adapter is the platform firewall boundary. Implementations must use fixed
// commands and return bounded Error values for operational failures.
type Adapter interface {
	Interfaces(context.Context) ([]InterfaceInfo, error)
	Inspect(context.Context, []Rule) ([]RuleState, error)
	Upsert(context.Context, []Rule) error
	Remove(context.Context, []string) error
}

// ownedRemover is implemented by adapters that can atomically verify rule
// ownership before removing exact names. It extends Adapter without breaking
// test or third-party adapters that only implement the original Remove API.
type ownedRemover interface {
	RemoveOwned(context.Context, []Rule) error
}

// Loader obtains verified package/configuration state. It must not log or
// return secrets.
type Loader func(context.Context) (Inputs, error)

// Service serializes all operations for one launcher instance.
type Service struct {
	adapter Adapter
	load    Loader
	serial  chan struct{}
}

func NewService(adapter Adapter, load Loader) *Service {
	return &Service{
		adapter: adapter,
		load:    load,
		serial:  make(chan struct{}, 1),
	}
}

func (s *Service) withSerial(ctx context.Context, fn func() (Result, error)) (Result, error) {
	if s == nil || s.adapter == nil || s.load == nil {
		return Result{}, errorFor(ReasonInvalidInput)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case s.serial <- struct{}{}:
		defer func() { <-s.serial }()
	case <-ctx.Done():
		return Result{}, ctx.Err()
	}
	return fn()
}

func (s *Service) Status(ctx context.Context) (Result, error) {
	return s.withSerial(ctx, func() (Result, error) {
		inputs, err := s.load(ctx)
		if err != nil {
			return Result{Action: ActionStatus, Reason: reasonOf(err)}, err
		}
		if err := ValidateInputs(inputs); err != nil {
			return Result{Action: ActionStatus, Reason: reasonOf(err)}, err
		}
		unlock, err := lockFirewallInstance(ctx, inputs.CanonicalRoot)
		if err != nil {
			return Result{Action: ActionStatus, Reason: reasonOf(err)}, err
		}
		defer unlock()
		interfaces, err := s.adapter.Interfaces(ctx)
		if err != nil {
			err = adapterError(err)
			return Result{Action: ActionStatus, Reason: reasonOf(err)}, err
		}
		var matchingStates []RuleState
		var mismatchStates []RuleState
		var matchingInterface string
		var effectiveMismatchStates []RuleState
		var effectiveMismatchInterface string
		var effectiveMatchingStates []RuleState
		var effectiveMatchingInterface string
		var diagnosticMismatchStates []RuleState
		var diagnosticMismatchInterface string
		var diagnosticMatchingStates []RuleState
		var diagnosticMatchingInterface string
		matches := 0
		validCandidates := 0
		for _, candidate := range interfaces {
			chosen, selectErr := SelectInterface(inputs.SIP, []InterfaceInfo{candidate}, candidate.ID)
			if selectErr != nil {
				continue
			}
			rules, buildErr := BuildRules(inputs, chosen)
			if buildErr != nil {
				continue
			}
			validCandidates++
			states, inspectErr := s.adapter.Inspect(ctx, rules)
			if inspectErr != nil {
				inspectErr = adapterError(inspectErr)
				return Result{Action: ActionStatus, Rules: boundRuleStates(states), Reason: reasonOf(inspectErr)}, inspectErr
			}
			states = classifyRuleStates(states, rules, chosen)
			if allMatching(states) {
				matches++
				matchingStates = states
				matchingInterface = chosen.ID
				if hasEffectiveBlock(states) {
					effectiveMatchingStates = states
					effectiveMatchingInterface = chosen.ID
				}
				if hasDiagnosticTruncation(states) {
					diagnosticMatchingStates = states
					diagnosticMatchingInterface = chosen.ID
				}
			} else {
				mismatchStates = states
				if hasEffectiveBlock(states) {
					effectiveMismatchStates = states
					effectiveMismatchInterface = chosen.ID
				}
				if hasDiagnosticTruncation(states) {
					diagnosticMismatchStates = states
					diagnosticMismatchInterface = chosen.ID
				}
			}
		}
		if matches == 1 {
			if effectiveMatchingStates != nil {
				result := Result{Action: ActionStatus, Success: false, Converged: true, InterfaceID: effectiveMatchingInterface, Rules: boundRuleStates(effectiveMatchingStates), Warnings: blockWarnings(effectiveMatchingStates), Reason: ReasonEffectiveBlockRule}
				return result, errorFor(ReasonEffectiveBlockRule)
			}
			result := Result{Action: ActionStatus, Success: true, Converged: true, InterfaceID: matchingInterface, Rules: boundRuleStates(matchingStates), Warnings: blockWarnings(matchingStates)}
			if hasDiagnosticTruncation(matchingStates) {
				result.Success = false
				result.Reason = ReasonDiagnosticTruncated
				return result, errorFor(ReasonDiagnosticTruncated)
			}
			return result, nil
		}
		if matches > 1 {
			if diagnosticMatchingStates != nil {
				result := Result{Action: ActionStatus, Success: false, InterfaceID: diagnosticMatchingInterface, Rules: boundRuleStates(diagnosticMatchingStates), Warnings: blockWarnings(diagnosticMatchingStates), Reason: ReasonDiagnosticTruncated}
				return result, errorFor(ReasonDiagnosticTruncated)
			}
			return Result{Action: ActionStatus, Success: true, Reason: ReasonInterfaceAmbiguous}, nil
		}
		expected := make([]Rule, 0, 4)
		if validCandidates > 0 {
			if effectiveMismatchStates != nil {
				result := Result{Action: ActionStatus, Success: false, Converged: false, InterfaceID: effectiveMismatchInterface, Rules: boundRuleStates(effectiveMismatchStates), Warnings: blockWarnings(effectiveMismatchStates), Reason: ReasonEffectiveBlockRule}
				return result, errorFor(ReasonEffectiveBlockRule)
			}
			if diagnosticMismatchStates != nil {
				result := Result{Action: ActionStatus, Success: false, Converged: false, InterfaceID: diagnosticMismatchInterface, Rules: boundRuleStates(diagnosticMismatchStates), Warnings: blockWarnings(diagnosticMismatchStates), Reason: ReasonDiagnosticTruncated}
				return result, errorFor(ReasonDiagnosticTruncated)
			}
			reason := ReasonRuleDrift
			if hasConflict(mismatchStates) {
				reason = ReasonRuleConflict
			}
			return Result{Action: ActionStatus, Success: true, Converged: false, Rules: boundRuleStates(mismatchStates), Warnings: blockWarnings(mismatchStates), Reason: reason}, nil
		}
		for _, name := range RuleNames(inputs.CanonicalRoot) {
			expected = append(expected, Rule{Name: name, Group: RuleGroup(inputs.CanonicalRoot)})
		}
		states, err := s.adapter.Inspect(ctx, expected)
		if err != nil {
			err = adapterError(err)
			return Result{Action: ActionStatus, Rules: boundRuleStates(states), Reason: reasonOf(err)}, err
		}
		states = classifyRuleStates(states, expected, InterfaceInfo{})
		reason := ReasonRuleDrift
		if hasConflict(states) {
			reason = ReasonRuleConflict
		}
		return Result{Action: ActionStatus, Success: true, Converged: false, Rules: boundRuleStates(states), Warnings: blockWarnings(states), Reason: reason}, nil
	})
}

func (s *Service) Apply(ctx context.Context, interfaceID string) (Result, error) {
	return s.mutate(ctx, ActionApply, interfaceID, func(rules []Rule) error {
		return s.adapter.Upsert(ctx, rules)
	})
}

// Remove deletes only the deterministic names for the loaded instance. The
// variadic argument is retained for source compatibility with early callers;
// it is deliberately ignored and never used to select a network interface.
func (s *Service) Remove(ctx context.Context, _ ...string) (Result, error) {
	return s.withSerial(ctx, func() (Result, error) {
		result := Result{Action: ActionRemove}
		inputs, err := s.load(ctx)
		if err != nil {
			result.Reason = reasonOf(err)
			return result, err
		}
		if err := ValidateIdentity(inputs); err != nil {
			result.Reason = reasonOf(err)
			return result, err
		}
		names := RuleNames(inputs.CanonicalRoot)
		if len(names) != 4 {
			result.Reason = ReasonInvalidInput
			return result, errorFor(ReasonInvalidInput)
		}
		unlock, err := lockFirewallInstance(ctx, inputs.CanonicalRoot)
		if err != nil {
			result.Reason = reasonOf(err)
			return result, err
		}
		defer unlock()
		expected := make([]Rule, 0, len(names))
		group := RuleGroup(inputs.CanonicalRoot)
		for _, name := range names {
			expected = append(expected, Rule{Name: name, Group: group})
		}
		if remover, ok := s.adapter.(ownedRemover); ok {
			err = remover.RemoveOwned(ctx, expected)
		} else {
			states, inspectErr := s.adapter.Inspect(ctx, expected)
			if inspectErr != nil {
				err = inspectErr
			} else if hasConflict(states) {
				err = errorFor(ReasonRuleConflict)
			} else {
				err = s.adapter.Remove(ctx, names)
			}
		}
		if err != nil {
			err = adapterError(err)
			result.Reason = reasonOf(err)
			return result, err
		}
		states, err := s.adapter.Inspect(ctx, expected)
		if err != nil {
			err = adapterError(err)
			result.Rules = boundRuleStates(states)
			result.Reason = reasonOf(err)
			return result, err
		}
		states = classifyRuleStates(states, expected, InterfaceInfo{})
		result.Rules = boundRuleStates(states)
		result.Warnings = blockWarnings(states)
		result.Success = nonePresent(states)
		result.Converged = result.Success
		if !result.Success {
			if hasConflict(states) {
				result.Reason = ReasonRuleConflict
				return result, errorFor(ReasonRuleConflict)
			}
			result.Reason = ReasonReadbackFailed
			return result, errorFor(ReasonReadbackFailed)
		}
		return result, nil
	})
}

func (s *Service) mutate(ctx context.Context, action Action, interfaceID string, operation func([]Rule) error) (Result, error) {
	return s.withSerial(ctx, func() (Result, error) {
		result := Result{Action: action, InterfaceID: interfaceID}
		inputs, err := s.load(ctx)
		if err != nil {
			result.Reason = reasonOf(err)
			return result, err
		}
		if err := ValidateInputs(inputs); err != nil {
			result.Reason = reasonOf(err)
			return result, err
		}
		interfaces, err := s.adapter.Interfaces(ctx)
		if err != nil {
			err = adapterError(err)
			result.Reason = reasonOf(err)
			return result, err
		}
		chosen, err := SelectInterface(inputs.SIP, interfaces, interfaceID)
		if err != nil {
			result.Reason = reasonOf(err)
			return result, err
		}
		rules, err := BuildRules(inputs, chosen)
		if err != nil {
			result.Reason = reasonOf(err)
			return result, err
		}
		unlock, err := lockFirewallInstance(ctx, inputs.CanonicalRoot)
		if err != nil {
			result.Reason = reasonOf(err)
			return result, err
		}
		defer unlock()
		existing, err := s.adapter.Inspect(ctx, rules)
		if err != nil {
			err = adapterError(err)
			result.Reason = reasonOf(err)
			return result, err
		}
		existing = classifyRuleStates(existing, rules, chosen)
		if hasConflict(existing) {
			result.Reason = ReasonRuleConflict
			return result, errorFor(ReasonRuleConflict)
		}
		if err := operation(rules); err != nil {
			err = adapterError(err)
			result.Reason = reasonOf(err)
			return result, err
		}
		states, err := s.adapter.Inspect(ctx, rules)
		if err != nil {
			err = adapterError(err)
			result.Rules = boundRuleStates(states)
			result.Reason = reasonOf(err)
			return result, err
		}
		states = classifyRuleStates(states, rules, chosen)
		result.Rules = boundRuleStates(states)
		result.Warnings = blockWarnings(states)
		if action == ActionApply {
			result.Converged = allMatching(states)
			if hasEffectiveBlock(states) {
				result.Success = false
				result.Reason = ReasonEffectiveBlockRule
				return result, errorFor(ReasonEffectiveBlockRule)
			}
			if hasDiagnosticTruncation(states) {
				result.Success = false
				result.Reason = ReasonDiagnosticTruncated
				return result, errorFor(ReasonDiagnosticTruncated)
			}
			result.Success = result.Converged
			if !result.Success {
				if hasConflict(states) {
					result.Reason = ReasonRuleConflict
					return result, errorFor(ReasonRuleConflict)
				}
				result.Reason = ReasonReadbackFailed
				return result, errorFor(ReasonReadbackFailed)
			}
			return result, nil
		}
		result.Success = nonePresent(states)
		result.Converged = result.Success
		if !result.Success {
			result.Reason = ReasonReadbackFailed
			return result, errorFor(ReasonReadbackFailed)
		}
		return result, nil
	})
}

func hasConflict(states []RuleState) bool {
	for _, state := range states {
		if !state.ExternalBlock && state.Conflict {
			return true
		}
	}
	return false
}

func hasEffectiveBlock(states []RuleState) bool {
	for _, state := range states {
		if state.ExternalBlock && !state.PotentialBlock && !state.DiagnosticsTruncated && !state.DetailTruncated {
			return true
		}
	}
	return false
}

func hasDiagnosticTruncation(states []RuleState) bool {
	for _, state := range states {
		if state.ExternalBlock && (state.DiagnosticsTruncated || state.DetailTruncated) {
			return true
		}
	}
	return false
}

func blockWarnings(states []RuleState) []Reason {
	var warnings []Reason
	for _, state := range states {
		if !state.ExternalBlock {
			continue
		}
		if state.PotentialBlock && !containsReason(warnings, ReasonPotentialBlock) {
			warnings = append(warnings, ReasonPotentialBlock)
		}
		if (state.DiagnosticsTruncated || state.DetailTruncated) && !containsReason(warnings, ReasonDiagnosticTruncated) {
			warnings = append(warnings, ReasonDiagnosticTruncated)
		}
	}
	return warnings
}

func containsReason(reasons []Reason, want Reason) bool {
	for _, reason := range reasons {
		if reason == want {
			return true
		}
	}
	return false
}

const (
	maxFirewallRuleStates = 20
	maxFirewallDetailLen  = 256
)

func boundFirewallDetail(value string) string {
	value = strings.TrimSpace(value)
	if len([]rune(value)) <= maxFirewallDetailLen {
		return value
	}
	return string([]rune(value)[:maxFirewallDetailLen])
}

func boundRuleStates(states []RuleState) []RuleState {
	owned := make([]RuleState, 0, len(states))
	external := make([]RuleState, 0, len(states))
	seenExternal := make(map[string]struct{})
	for _, state := range states {
		if state.ExternalBlock && ruleStateDetailTruncated(state) {
			state.DetailTruncated = true
		}
		state.Name = boundFirewallDetail(state.Name)
		state.Group = boundFirewallDetail(state.Group)
		state.Program = boundFirewallDetail(state.Program)
		state.Protocol = boundFirewallDetail(state.Protocol)
		state.LocalPort = boundFirewallDetail(state.LocalPort)
		state.RemotePort = boundFirewallDetail(state.RemotePort)
		state.Direction = boundFirewallDetail(state.Direction)
		state.Action = boundFirewallDetail(state.Action)
		state.Profile = boundFirewallDetail(state.Profile)
		state.LocalAddress = boundFirewallDetail(state.LocalAddress)
		state.RemoteAddress = boundFirewallDetail(state.RemoteAddress)
		state.InterfaceAlias = boundFirewallDetail(state.InterfaceAlias)
		state.InterfaceType = boundFirewallDetail(state.InterfaceType)
		state.Service = boundFirewallDetail(state.Service)
		state.AppPackage = boundFirewallDetail(state.AppPackage)
		if !state.ExternalBlock {
			owned = append(owned, state)
			continue
		}
		key := strings.ToLower(state.Name)
		if key == "" {
			key = strings.ToLower(strings.Join([]string{state.Program, state.Protocol, state.LocalPort, state.RemoteAddress, state.InterfaceAlias, state.InterfaceType}, "|"))
		}
		if _, exists := seenExternal[key]; exists {
			continue
		}
		seenExternal[key] = struct{}{}
		external = append(external, state)
	}
	sort.SliceStable(external, func(i, j int) bool {
		if !strings.EqualFold(external[i].Name, external[j].Name) {
			return strings.ToLower(external[i].Name) < strings.ToLower(external[j].Name)
		}
		if external[i].PotentialBlock != external[j].PotentialBlock {
			return !external[i].PotentialBlock
		}
		return external[i].Program < external[j].Program
	})
	if len(owned) > maxFirewallRuleStates {
		owned = owned[:maxFirewallRuleStates]
	}
	remaining := maxFirewallRuleStates - len(owned)
	if remaining < 0 {
		remaining = 0
	}
	if len(external) > remaining {
		external = external[:remaining]
	}
	return append(owned, external...)
}

func ruleStateDetailTruncated(state RuleState) bool {
	for _, value := range []string{
		state.Name, state.Group, state.Program, state.Protocol, state.LocalPort,
		state.RemotePort, state.Direction, state.Action, state.Profile,
		state.LocalAddress, state.RemoteAddress, state.InterfaceAlias,
		state.InterfaceType, state.Service, state.AppPackage,
	} {
		if len([]rune(strings.TrimSpace(value))) > maxFirewallDetailLen {
			return true
		}
	}
	return false
}

func adapterError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	var bounded *Error
	if errors.As(err, &bounded) {
		return err
	}
	return errorFor(ReasonAdapterUnavailable)
}

func allPresent(states []RuleState) bool {
	states = ownedRuleStates(states)
	if len(states) != 4 {
		return false
	}
	for _, state := range states {
		if !state.Present {
			return false
		}
	}
	return true
}

func allMatching(states []RuleState) bool {
	states = ownedRuleStates(states)
	if len(states) != 4 {
		return false
	}
	for _, state := range states {
		if !state.Present || !state.Matches {
			return false
		}
	}
	return true
}

func nonePresent(states []RuleState) bool {
	states = ownedRuleStates(states)
	if len(states) != 4 {
		return false
	}
	for _, state := range states {
		if state.Present {
			return false
		}
	}
	return true
}

func ownedRuleStates(states []RuleState) []RuleState {
	owned := make([]RuleState, 0, len(states))
	for _, state := range states {
		if !state.ExternalBlock && !state.PotentialBlock && !state.DiagnosticsTruncated {
			owned = append(owned, state)
		}
	}
	return owned
}

// parseIPv4 is shared by policy and adapter-facing tests.
func parseIPv4(value string) (netip.Addr, bool) {
	addr, err := netip.ParseAddr(value)
	if err != nil || !addr.Is4() {
		return netip.Addr{}, false
	}
	return addr, true
}

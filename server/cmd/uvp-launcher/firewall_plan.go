package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"uvplatform.cn/uvp-gb28181/internal/standalone/firewall"
)

func firewallDigest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func validFirewallPlanHash(value string) bool {
	if len(value) != 64 {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && hex.EncodeToString(decoded) == value
}

func firewallPlanHash(inputs firewall.Inputs, action firewall.Action, rules []firewall.Rule) string {
	// Inputs intentionally excludes secrets from JSON; explicitly bind identity
	// and the verified release manifest as well as every planned rule field.
	value := struct {
		Action        firewall.Action
		Root, Release string
		Names         []string
		Rules         []firewall.Rule
	}{action, inputs.CanonicalRoot, inputs.ReleaseID, firewall.RuleNames(inputs.CanonicalRoot), rules}
	data, _ := json.Marshal(value)
	return firewallDigest(data)
}

// Service invokes mutations under its instance mutex. Re-read the authoritative
// inputs at that boundary, rather than applying the earlier public preview.
type confirmedFirewallAdapter struct {
	firewall.Adapter
	load    firewall.Loader
	command firewallCommand
	mutated bool
}

func (a *confirmedFirewallAdapter) verify(ctx context.Context, supplied []firewall.Rule) error {
	inputs, err := a.load(ctx)
	if err != nil {
		return err
	}
	var rules []firewall.Rule
	if a.command.action == firewall.ActionApply {
		interfaces, err := a.Adapter.Interfaces(ctx)
		if err != nil {
			return err
		}
		selected, err := firewall.SelectInterface(inputs.SIP, interfaces, a.command.interfaceID)
		if err != nil {
			return err
		}
		rules, err = firewall.BuildRules(inputs, selected)
		if err != nil {
			return err
		}
		if supplied != nil && firewallPlanHash(inputs, a.command.action, supplied) != firewallPlanHash(inputs, a.command.action, rules) {
			return firewall.NewError(firewall.ReasonRuleDrift)
		}
	}
	if firewallPlanHash(inputs, a.command.action, rules) != a.command.planHash {
		return firewall.NewError(firewall.ReasonRuleDrift)
	}
	return nil
}

func (a *confirmedFirewallAdapter) Upsert(ctx context.Context, rules []firewall.Rule) error {
	if err := a.verify(ctx, rules); err != nil {
		return err
	}
	a.mutated = true
	if err := a.Adapter.Upsert(ctx, rules); err != nil {
		return err
	}
	return a.verify(ctx, rules)
}

func (a *confirmedFirewallAdapter) Remove(ctx context.Context, names []string) error {
	if err := a.verify(ctx, nil); err != nil {
		return err
	}
	a.mutated = true
	if err := a.Adapter.Remove(ctx, names); err != nil {
		return err
	}
	return a.verify(ctx, nil)
}

// Stable small exit codes communicate bounded reasons across the elevated
// process without reading its output or exposing OS error text.
var firewallExitReasons = []firewall.Reason{
	"invalid_input", "release_unverified", "invalid_executable", "sip_invalid",
	"sip_address_not_on_interface", "media_invalid", "interface_missing",
	"interface_ambiguous", "interface_down", "interface_not_private",
	"interface_loopback", "interface_no_ipv4", "installation_incomplete",
	"configuration_unavailable", "database_unavailable", "sip_config_missing",
	"adapter_unavailable", "permission_denied", "readback_failed", "operation_failed",
	"unsupported_platform", "confirmation_required", "elevation_canceled", "elevation_failed",
	"rule_drift", "rule_conflict",
}

func firewallExitCode(reason firewall.Reason) int {
	for i, candidate := range firewallExitReasons {
		if candidate == reason {
			return i + 10
		}
	}
	return 1
}

func firewallReasonFromExitCode(code uint32) firewall.Reason {
	if code >= 10 && int(code-10) < len(firewallExitReasons) {
		return firewallExitReasons[code-10]
	}
	return firewall.ReasonElevationFailed
}

func (a *confirmedFirewallAdapter) Inspect(ctx context.Context, rules []firewall.Rule) ([]firewall.RuleState, error) {
	states, err := a.Adapter.Inspect(ctx, rules)
	if err != nil {
		return states, err
	}
	if a.mutated {
		if err := a.verify(ctx, nil); err != nil {
			return states, err
		}
	}
	return states, nil
}

func reportFirewallChildFailure(ctx context.Context, options firewallCommandOptions, action firewall.Action, childErr error) int {
	result, _ := firewall.NewService(options.adapter(), options.load).Status(ctx)
	result.Action, result.Success, result.Converged = action, false, false
	result.Reason = firewall.ReasonOf(childErr)
	return writeFirewallResult(options, result)
}

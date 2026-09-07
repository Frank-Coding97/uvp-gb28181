//go:build windows

package firewall

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

const maxPowerShellResponse = 64 << 10

type powerShellAdapter struct{}

type powerShellRequest struct {
	Operation string `json:"operation"`
	Rules     []Rule `json:"rules,omitempty"`
}

type powerShellResponse struct {
	OK         bool            `json:"ok"`
	Reason     Reason          `json:"reason,omitempty"`
	Interfaces json.RawMessage `json:"interfaces,omitempty"`
	RuleStates json.RawMessage `json:"rules,omitempty"`
}

// The script is fixed at build time. All runtime values arrive as JSON on
// stdin; no user-controlled value is ever interpolated into PowerShell code.
const powerShellFirewallScript = `
$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'
$utf8 = New-Object -TypeName System.Text.UTF8Encoding -ArgumentList $false
[Console]::InputEncoding = $utf8
[Console]::OutputEncoding = $utf8

function Emit($value) {
    $value | ConvertTo-Json -Compress -Depth 8
}

function StringValue($value) {
    if ($null -eq $value) { return '' }
    return (@($value) | ForEach-Object { [string]$_ }) -join ','
}

function PortValue($value) {
    if ($null -eq $value) { return '' }
    $tokens = @(@($value) | ForEach-Object { [string]$_ -split ',' | ForEach-Object { $_.Trim() } | Where-Object { $_ -ne '' } })
    $singles = @($tokens | Where-Object { $_ -match '^\d+$' } | Sort-Object { [int]$_ })
    $ranges = @($tokens | Where-Object { $_ -notmatch '^\d+$' } | Sort-Object)
    return (@($singles + $ranges) -join ',')
}

function SameValue($actual, $expected, [bool]$ports) {
    if ($ports) {
        return (PortValue $actual) -ieq (PortValue $expected)
    }
    return (StringValue $actual) -ieq (StringValue $expected)
}

function FindRule([string]$name) {
    try {
        Get-NetFirewallRule -Name $name -ErrorAction Stop
    } catch {
        if ($_.CategoryInfo.Category -eq 'ObjectNotFound' -and
            [string]$_.FullyQualifiedErrorId -like 'CmdletizationQuery_NotFound_*,Get-NetFirewallRule') {
            return
        }
        throw
    }
}

function RuleState($expected) {
    $rule = FindRule ([string]$expected.name)
    if ($null -eq $rule) {
        return [pscustomobject]@{ name = [string]$expected.name; group = ''; present = $false; matches = $false; conflict = $false }
    }
    $actualGroup = [string]$rule.Group
    $expectedGroup = [string]$expected.group
    $groupMatches = ($expectedGroup -eq '' -or $actualGroup -ieq $expectedGroup)
    $conflict = ($expectedGroup -ne '' -and -not $groupMatches)
    if ([string]$expected.program -eq '') {
        return [pscustomobject]@{ name = [string]$expected.name; group = $actualGroup; present = $true; matches = [bool]$groupMatches; conflict = [bool]$conflict }
    }
	$application = Get-NetFirewallApplicationFilter -AssociatedNetFirewallRule $rule
	$port = Get-NetFirewallPortFilter -AssociatedNetFirewallRule $rule
	$address = Get-NetFirewallAddressFilter -AssociatedNetFirewallRule $rule
	$interface = Get-NetFirewallInterfaceFilter -AssociatedNetFirewallRule $rule
    $matches = (
        $groupMatches -and
        (SameValue $rule.Enabled ([string]$expected.enabled) $false) -and
        (SameValue $rule.Direction $expected.direction $false) -and
        (SameValue $rule.Action $expected.action $false) -and
        (SameValue $rule.Profile $expected.profile $false) -and
        (SameValue $rule.EdgeTraversalPolicy $expected.edge_traversal_policy $false) -and
        (SameValue $interface.InterfaceAlias $expected.interface_alias $false) -and
        (SameValue $application.Program $expected.program $false) -and
        (SameValue $port.Protocol $expected.protocol $false) -and
        (SameValue $port.LocalPort $expected.local_port $true) -and
        (SameValue $address.RemoteAddress $expected.remote_address $false)
    )
    return [pscustomobject]@{ name = [string]$expected.name; group = $actualGroup; present = $true; matches = [bool]$matches; conflict = [bool]$conflict }
}

try {
    $utf8Reader = New-Object -TypeName System.IO.StreamReader -ArgumentList ([Console]::OpenStandardInput()), $utf8, $true
    $request = ($utf8Reader.ReadToEnd() | ConvertFrom-Json)
    switch ([string]$request.operation) {
        'interfaces' {
            $items = @(
                Get-NetAdapter -IncludeHidden | ForEach-Object {
                    $adapter = $_
                    $profile = @(Get-NetConnectionProfile -InterfaceIndex $adapter.ifIndex -ErrorAction SilentlyContinue | Select-Object -First 1)
                    $addresses = @(Get-NetIPAddress -InterfaceIndex $adapter.ifIndex -AddressFamily IPv4 -ErrorAction SilentlyContinue | ForEach-Object { [string]$_.IPAddress })
                    [pscustomobject]@{
                        id = [string]$adapter.InterfaceGuid
                        alias = [string]$adapter.Name
                        up = ([string]$adapter.Status -ieq 'Up')
                        private = ($profile.Count -eq 1 -and [string]$profile[0].NetworkCategory -ieq 'Private')
                        loopback = ([string]$adapter.Name -match '(?i)loopback' -or [int]$adapter.ifType -eq 24)
                        ipv4 = $addresses
                    }
                }
            )
            Emit ([pscustomobject]@{ ok = $true; interfaces = $items })
        }
        'inspect' {
            $states = @($request.rules | ForEach-Object { RuleState $_ })
            Emit ([pscustomobject]@{ ok = $true; rules = $states })
        }
        'upsert' {
            $items = @($request.rules)
            foreach ($item in $items) {
                $name = [string]$item.name
                $group = [string]$item.group
                if ($name -eq '' -or $group -eq '') {
                    Emit ([pscustomobject]@{ ok = $false; reason = 'invalid_input' })
                    exit 2
                }
                $existing = @(FindRule $name)
                foreach ($rule in $existing) {
                    if ([string]$rule.Group -ine $group) {
                        Emit ([pscustomobject]@{ ok = $false; reason = 'rule_conflict' })
                        exit 3
                    }
                }
            }
            foreach ($item in $items) {
                $params = @{
                    Name = [string]$item.name
                    DisplayName = [string]$item.name
                    Group = [string]$item.group
                    Direction = [string]$item.direction
                    Action = [string]$item.action
                    Profile = [string]$item.profile
                    Program = [string]$item.program
                    Protocol = [string]$item.protocol
                    LocalPort = @([string]$item.local_port -split ',')
                    RemoteAddress = [string]$item.remote_address
                    InterfaceAlias = [string]$item.interface_alias
                    EdgeTraversalPolicy = [string]$item.edge_traversal_policy
                    Enabled = ([bool]$item.enabled)
                    ErrorAction = 'Stop'
                }
                $existing = @(FindRule $params.Name)
                if ($existing.Count -gt 0) {
                    Remove-NetFirewallRule -Name $params.Name -ErrorAction Stop
                }
                New-NetFirewallRule @params | Out-Null
            }
            Emit ([pscustomobject]@{ ok = $true })
        }
        'remove' {
            $items = @($request.rules)
            foreach ($item in $items) {
                $name = [string]$item.name
                $group = [string]$item.group
                if ($name -eq '' -or $group -eq '') {
                    Emit ([pscustomobject]@{ ok = $false; reason = 'invalid_input' })
                    exit 2
                }
                $existing = @(FindRule $name)
                foreach ($rule in $existing) {
                    if ([string]$rule.Group -ine $group) {
                        Emit ([pscustomobject]@{ ok = $false; reason = 'rule_conflict' })
                        exit 3
                    }
                }
            }
            foreach ($item in $items) {
                $existing = @(FindRule ([string]$item.name))
                if ($existing.Count -gt 0) {
                    Remove-NetFirewallRule -Name ([string]$item.name) -ErrorAction Stop
                }
            }
            Emit ([pscustomobject]@{ ok = $true })
        }
        default {
            Emit ([pscustomobject]@{ ok = $false; reason = 'invalid_input' })
            exit 2
        }
    }
}
catch {
    $reason = 'operation_failed'
    if ([string]$_.Exception.Message -match '(?i)access is denied|unauthorized|permission') {
        $reason = 'permission_denied'
    } elseif ([string]$_.Exception.Message -match '(?i)not found|cannot find|no such') {
        $reason = 'adapter_unavailable'
    }
    Emit ([pscustomobject]@{ ok = $false; reason = $reason })
    exit 1
}
`

func NewSystemAdapter() Adapter {
	return powerShellAdapter{}
}

func (powerShellAdapter) Interfaces(ctx context.Context) ([]InterfaceInfo, error) {
	response, err := runPowerShell(ctx, powerShellRequest{Operation: "interfaces"})
	if err != nil {
		return nil, err
	}
	var values []InterfaceInfo
	if err := decodeJSONArray(response.Interfaces, &values); err != nil {
		return nil, errorFor(ReasonAdapterUnavailable)
	}
	return values, nil
}

func (powerShellAdapter) Inspect(ctx context.Context, rules []Rule) ([]RuleState, error) {
	response, err := runPowerShell(ctx, powerShellRequest{Operation: "inspect", Rules: rules})
	if err != nil {
		return nil, err
	}
	var values []RuleState
	if err := decodeJSONArray(response.RuleStates, &values); err != nil {
		return nil, errorFor(ReasonAdapterUnavailable)
	}
	return values, nil
}

func (powerShellAdapter) Upsert(ctx context.Context, rules []Rule) error {
	_, err := runPowerShell(ctx, powerShellRequest{Operation: "upsert", Rules: rules})
	return err
}

func (powerShellAdapter) Remove(ctx context.Context, names []string) error {
	rules := make([]Rule, 0, len(names))
	for _, name := range names {
		rules = append(rules, Rule{Name: name, Group: ruleGroupForName(name)})
	}
	_, err := runPowerShell(ctx, powerShellRequest{Operation: "remove", Rules: rules})
	return err
}

func (powerShellAdapter) RemoveOwned(ctx context.Context, rules []Rule) error {
	_, err := runPowerShell(ctx, powerShellRequest{Operation: "remove", Rules: rules})
	return err
}

func powerShellPath() (string, error) {
	systemDir, err := windows.GetSystemDirectory()
	if err != nil || strings.TrimSpace(systemDir) == "" {
		return "", errorFor(ReasonAdapterUnavailable)
	}
	path := filepath.Join(systemDir, "WindowsPowerShell", "v1.0", "powershell.exe")
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return "", errorFor(ReasonAdapterUnavailable)
	}
	return path, nil
}

func runPowerShell(ctx context.Context, request powerShellRequest) (powerShellResponse, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	payload, err := json.Marshal(request)
	if err != nil {
		return powerShellResponse{}, errorFor(ReasonInvalidInput)
	}
	powershell, err := powerShellPath()
	if err != nil {
		return powerShellResponse{}, err
	}
	cmd := exec.CommandContext(ctx, powershell, "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", powerShellFirewallScript)
	cmd.Stdin = bytes.NewReader(payload)
	cmd.Stderr = io.Discard
	var output boundedBuffer
	cmd.Stdout = &output
	runErr := cmd.Run()
	if ctx.Err() != nil {
		return powerShellResponse{}, ctx.Err()
	}
	if output.Overflow {
		return powerShellResponse{}, errorFor(ReasonAdapterUnavailable)
	}
	var response powerShellResponse
	if err := json.Unmarshal(output.Bytes(), &response); err != nil {
		return powerShellResponse{}, errorFor(ReasonAdapterUnavailable)
	}
	if !response.OK {
		if response.Reason == "" {
			response.Reason = ReasonOperationFailed
		}
		return response, errorFor(response.Reason)
	}
	if runErr != nil {
		return response, errorFor(ReasonOperationFailed)
	}
	return response, nil
}

type boundedBuffer struct {
	buf      bytes.Buffer
	Overflow bool
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	if b.buf.Len()+len(p) > maxPowerShellResponse {
		b.Overflow = true
		return len(p), nil
	}
	return b.buf.Write(p)
}

func (b *boundedBuffer) Bytes() []byte {
	return b.buf.Bytes()
}

func decodeJSONArray(raw json.RawMessage, target any) error {
	value := bytes.TrimSpace(raw)
	if len(value) == 0 || bytes.Equal(value, []byte("null")) {
		return nil
	}
	if value[0] == '{' {
		value = append([]byte{'['}, append(value, ']')...)
	}
	decoder := json.NewDecoder(strings.NewReader(string(value)))
	decoder.DisallowUnknownFields()
	return decoder.Decode(target)
}

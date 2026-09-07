// environment-probe inventories a Windows build/runtime host without installing anything.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

type inventory struct {
	Caption           string   `json:"caption"`
	Build             int      `json:"build"`
	ProductType       int      `json:"product_type"`
	Architecture      int      `json:"architecture"`
	BuildTools        []string `json:"build_tools"`
	RunningComponents []string `json:"running_components"`
}

// This is a preflight filter, not proof that no dependencies are installed.
func validate(role string, i inventory) error {
	if role != "build" && role != "runtime" {
		return errors.New("role must be build or runtime")
	}
	if i.Caption == "" || i.Build <= 0 || i.Architecture != 9 {
		return errors.New("requires a detected native x64 Windows host")
	}
	if role == "runtime" {
		if i.ProductType != 1 || i.Build < 19045 || i.Build >= 22000 || !strings.Contains(i.Caption, "Windows 10") {
			return errors.New("runtime must be Windows 10 22H2 x64 (build 19045)")
		}
		if len(i.BuildTools) != 0 || len(i.RunningComponents) != 0 {
			return errors.New("runtime is not isolated: build tools or existing components detected")
		}
	}
	return nil
}

// Only query OS metadata, tool names and relevant process names. No secrets or command lines.
const query = `$ErrorActionPreference = 'Stop'
[Console]::OutputEncoding = New-Object System.Text.UTF8Encoding $false
$os = Get-CimInstance Win32_OperatingSystem
$cpu = @(Get-CimInstance Win32_Processor)[0]
$tools = @('git','go','node','cmake','msbuild','cl','gcc','clang','docker' | Where-Object { Get-Command $_ -ErrorAction SilentlyContinue })
$processes = @(Get-Process | Where-Object { $_.ProcessName -match '^(redis-server|MediaServer|uvp-server|mysqld|postgres|sqlservr)$' } | Select-Object -ExpandProperty ProcessName -Unique)
[ordered]@{caption=$os.Caption; build=[int]$os.BuildNumber; product_type=[int]$os.ProductType; architecture=[int]$cpu.Architecture; build_tools=$tools; running_components=$processes} | ConvertTo-Json -Compress`

func main() {
	role := flag.String("role", "runtime", "build or runtime")
	flag.Parse()
	if runtime.GOOS != "windows" {
		fmt.Fprintln(os.Stderr, "BLOCKED: run this probe on Windows, not an emulated inventory")
		os.Exit(1)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "powershell.exe", "-NoLogo", "-NoProfile", "-NonInteractive", "-Command", query).Output()
	if err != nil {
		fmt.Fprintln(os.Stderr, "BLOCKED: Windows inventory query failed")
		os.Exit(1)
	}
	var i inventory
	if err := json.Unmarshal(out, &i); err != nil {
		fmt.Fprintln(os.Stderr, "BLOCKED: invalid Windows inventory")
		os.Exit(1)
	}
	validation := validate(*role, i)
	report := struct {
		Role                   string    `json:"role"`
		Inventory              inventory `json:"inventory"`
		PreflightPassed        bool      `json:"preflight_passed"`
		WindowsRuntimeVerified bool      `json:"windows_runtime_verified"`
		Note                   string    `json:"note"`
	}{*role, i, validation == nil, false, "Inventory only. T01-C needs clean OS provenance; P0 additionally needs native component execution."}
	if err := json.NewEncoder(os.Stdout).Encode(report); err != nil {
		os.Exit(1)
	}
	if validation != nil {
		fmt.Fprintln(os.Stderr, "BLOCKED:", validation)
		os.Exit(1)
	}
}

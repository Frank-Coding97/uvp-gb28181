// standalone-path-probe exercises the server's real explicit path contract.
// The CGO-free executable can be copied to a clean Windows host for T05.
package main

import (
	"encoding/json"
	"fmt"
	"os"

	"uvplatform.cn/uvp-gb28181/internal/standalone"
)

type probeResult struct {
	Passed   bool              `json:"passed"`
	CWD      string            `json:"cwd,omitempty"`
	Paths    *standalone.Paths `json:"paths,omitempty"`
	Error    string            `json:"error,omitempty"`
	Expected bool              `json:"expected_rejection,omitempty"`
}

func main() {
	expectError, args := splitExpectError(os.Args[1:])
	paths, err := standalone.ResolveStartupPaths(args, os.Getenv)
	if err == nil && !paths.Explicit {
		err = standalone.ErrExplicitPathRequired
	}
	if err == nil {
		err = paths.Validate()
	}
	if err != nil {
		writeResult(probeResult{Passed: expectError, Error: err.Error(), Expected: expectError})
		if !expectError {
			os.Exit(1)
		}
		return
	}
	if expectError {
		writeResult(probeResult{Error: "expected rejection but all paths were accepted"})
		os.Exit(1)
	}
	cwd, _ := os.Getwd()
	writeResult(probeResult{Passed: true, CWD: cwd, Paths: &paths})
}

func splitExpectError(args []string) (bool, []string) {
	filtered := make([]string, 0, len(args))
	expectError := false
	for _, arg := range args {
		if arg == "-expect-error" || arg == "--expect-error" || arg == "-expect-error=true" || arg == "--expect-error=true" {
			expectError = true
			continue
		}
		filtered = append(filtered, arg)
	}
	return expectError, filtered
}

func writeResult(value probeResult) {
	if err := json.NewEncoder(os.Stdout).Encode(value); err != nil {
		fmt.Fprintln(os.Stderr, err)
	}
}

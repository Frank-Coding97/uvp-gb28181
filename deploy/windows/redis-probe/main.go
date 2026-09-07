package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"
)

type options struct {
	redisServer     string
	expectedVersion string
	sourceSHA       string
	licenseFile     string
	diskFullRoot    string
	keepTemp        bool
}

type report struct {
	Schema          int           `json:"schema"`
	StartedAt       string        `json:"started_at"`
	FinishedAt      string        `json:"finished_at"`
	Status          string        `json:"status"`
	RedisServer     string        `json:"redis_server"`
	BinarySHA256    string        `json:"binary_sha256,omitempty"`
	ExpectedVersion string        `json:"expected_version,omitempty"`
	SourceSHA       string        `json:"source_sha,omitempty"`
	Workspace       string        `json:"workspace,omitempty"`
	WorkspaceKept   bool          `json:"workspace_kept"`
	Checks          []checkResult `json:"checks"`
	Errors          []string      `json:"errors,omitempty"`
}

type checkResult struct {
	Name    string         `json:"name"`
	Status  string         `json:"status"`
	Details map[string]any `json:"details,omitempty"`
	Error   string         `json:"error,omitempty"`
}

func main() {
	var opts options
	flag.StringVar(&opts.redisServer, "redis-server", "", "path to the Redis server executable")
	flag.StringVar(&opts.expectedVersion, "expected-version", "", "exact Redis server version to require")
	flag.StringVar(&opts.sourceSHA, "source-sha", "", "locked Redis source commit SHA to record")
	flag.StringVar(&opts.licenseFile, "license-file", "", "Redis license/COPYING file to verify")
	flag.StringVar(&opts.diskFullRoot, "disk-full-root", "", "marked small Windows volume root for the disk-full test")
	flag.BoolVar(&opts.keepTemp, "keep-temp", false, "keep the temporary probe workspace after completion")
	flag.Parse()

	result := runProbe(opts)
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(result); err != nil {
		fmt.Fprintf(os.Stderr, "encode probe report: %v\n", err)
		os.Exit(1)
	}
	if result.Status != "passed" {
		os.Exit(1)
	}
}

func newReport(opts options) report {
	return report{
		Schema:          1,
		StartedAt:       time.Now().UTC().Format(time.RFC3339Nano),
		Status:          "failed",
		RedisServer:     opts.redisServer,
		ExpectedVersion: opts.expectedVersion,
		SourceSHA:       opts.sourceSHA,
		WorkspaceKept:   opts.keepTemp,
		Checks:          make([]checkResult, 0, 3),
	}
}

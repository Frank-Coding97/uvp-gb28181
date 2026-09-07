// zlm-probe exercises the Windows runtime contract of the packaged ZLMediaKit.
// It copies an input release tree to an isolated Chinese/space path before
// starting MediaServer.exe, so the probe never mutates the build output.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"
)

func main() {
	var opts probeOptions
	flag.StringVar(&opts.root, "root", "", "directory containing MediaServer.exe and its complete runtime resources")
	flag.StringVar(&opts.executable, "exe", "", "path to MediaServer.exe; it must be inside -root")
	flag.StringVar(&opts.fixture, "fixture", "", "small H264 MP4 fixture used by the media and recording checks")
	flag.StringVar(&opts.expectedCommit, "expected-commit", "", "locked ZLMediaKit commit; /index/api/version must report its prefix")
	flag.BoolVar(&opts.keepTemp, "keep-temp", false, "keep the isolated workspace and process logs after the probe")
	flag.Parse()

	report := runProbe(opts)
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(report); err != nil {
		fmt.Fprintln(os.Stderr, "encode probe report failed")
		os.Exit(1)
	}
	if !report.Passed {
		os.Exit(1)
	}
}

func newReport(opts probeOptions) probeReport {
	return probeReport{
		Schema:        1,
		Probe:         "windows-zlm-probe",
		StartedAt:     time.Now().UTC().Format(time.RFC3339Nano),
		Status:        "failed",
		WorkspaceKept: opts.keepTemp,
		Checks:        make([]checkResult, 0, 8),
	}
}

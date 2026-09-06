// Command logging-inventory emits the bounded T01 source inventory as JSON.
// It is useful in review scripts; it never imports bootstrap or starts the
// application.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"uvplatform.cn/uvp-gb28181/internal/loggingcontract"
)

func main() {
	root := flag.String("root", ".", "Go source root to scan")
	includeTests := flag.Bool("include-tests", false, "include *_test.go files")
	failOnIssues := flag.Bool("fail-on-issues", false, "exit 1 when policy findings are present")
	policy := flag.Bool("policy", false, "run the type-aware logging policy gate")
	failOnFindings := flag.Bool("fail-on-findings", false, "exit 1 when type-aware policy findings are present")
	flag.Parse()
	if *policy || *failOnFindings {
		report, err := loggingcontract.CheckPolicy(*root, loggingcontract.PolicyOptions{IncludeTests: *includeTests})
		if err != nil {
			_, _ = fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(report); err != nil {
			_, _ = fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		if *failOnFindings && report.HasFindings() {
			os.Exit(1)
		}
		return
	}

	report, err := loggingcontract.Scan(*root, loggingcontract.Options{IncludeTests: *includeTests})
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(report); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if *failOnIssues && len(report.Issues) != 0 {
		os.Exit(1)
	}
}

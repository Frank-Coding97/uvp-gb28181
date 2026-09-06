package loggingcontract

import (
	"path/filepath"
	"testing"
)

func TestLoggingPolicy(t *testing.T) {
	t.Run("businessErrorsOutsideLoggerArgumentsAreIgnored", func(t *testing.T) {
		report := mustCheckPolicyFixture(t, "policy_clean")
		if report.HasFindings() {
			t.Fatalf("clean fixture findings = %#v", report.Findings)
		}
	})

	t.Run("zapNewNopIsClassifiedAsNoOutputFallback", func(t *testing.T) {
		report := mustCheckPolicyFixture(t, "policy_noop")
		if hasPolicyFinding(report, PolicyLoggerConstructor) {
			t.Fatalf("zap.NewNop must not be treated as an output bypass: %#v", report.Findings)
		}
	})

	t.Run("unresolvedLoggerInterfaceIsExplicit", func(t *testing.T) {
		report := mustCheckPolicyFixture(t, "policy_unresolved")
		if !hasPolicyFinding(report, PolicyUnresolvedLogger) {
			t.Fatalf("unresolved interface logger must be reviewed: %#v", report.Findings)
		}
	})

	t.Run("typedLoggerFindingsAreRejected", func(t *testing.T) {
		report := mustCheckPolicyFixture(t, "policy_bad")
		want := map[PolicyFindingKind]bool{
			PolicyLoggerConstructor:  false,
			PolicyDynamicMessage:     false,
			PolicyUnknownField:       false,
			PolicyDynamicErrorString: false,
			PolicyMissingEvent:       false,
			PolicyStdoutBypass:       false,
			PolicyStdlibLog:          false,
		}
		for _, finding := range report.Findings {
			want[finding.Kind] = true
		}
		for kind, found := range want {
			if !found {
				t.Errorf("finding kind %q missing from %#v", kind, report.Findings)
			}
		}
		if len(report.Inventory.Sites) == 0 {
			t.Fatal("policy report must retain non-empty inventory")
		}
	})

	t.Run("typedChainedLoggerSemanticsAreChecked", func(t *testing.T) {
		report := mustCheckPolicyFixture(t, "policy_chains")
		if countPolicyFindings(report, PolicyLoggerConstructor) != 2 {
			t.Fatalf("only zap.NewExample and Config.Build should be independent constructors: %#v", report.Findings)
		}
		if countPolicyFindings(report, PolicyMissingEvent) != 1 {
			t.Fatalf("only the unstructured sugared Infof lacks a structured event: %#v", report.Findings)
		}
		if countPolicyFindings(report, PolicyDynamicMessage) != 0 {
			t.Fatalf("zap.Logger.Log must treat level as the first argument and message as the second: %#v", report.Findings)
		}
		if countPolicyFindings(report, PolicyUnknownField) < 3 {
			t.Fatalf("zap With, local fields, and slog raw values must be checked: %#v", report.Findings)
		}
		if countPolicyFindings(report, PolicyDynamicErrorString) != 2 {
			t.Fatalf("sugared Infof and standard Printf format errors must be checked: %#v", report.Findings)
		}
	})

	t.Run("exactLegacyExceptionIsAccepted", func(t *testing.T) {
		root := filepath.Join("testdata", "policy_legacy")
		report := mustCheckPolicyFixture(t, "policy_legacy", PolicyOptions{LegacyExceptions: []LegacyException{{
			PackagePath: "uvplatform.cn/uvp-gb28181/internal/loggingcontract/testdata/policy_legacy",
			File:        "legacy.go",
			Function:    "Historical",
			Method:      "Info",
			Message:     "historic message",
			Count:       1,
			Reason:      "fixture historical compatibility",
		}}})
		if report.HasFindings() {
			t.Fatalf("exact exception findings = %#v", report.Findings)
		}
		_ = root
	})

	t.Run("legacyExceptionDoesNotMatchChangedMessage", func(t *testing.T) {
		root := filepath.Join("testdata", "policy_legacy_changed")
		report, err := CheckPolicy(root, PolicyOptions{LegacyExceptions: []LegacyException{{
			PackagePath: "uvplatform.cn/uvp-gb28181/internal/loggingcontract/testdata/policy_legacy_changed",
			File:        "legacy.go",
			Function:    "Historical",
			Method:      "Info",
			Message:     "historic message",
			Count:       1,
			Reason:      "fixture historical compatibility",
		}}})
		if err != nil {
			t.Fatal(err)
		}
		if !hasPolicyFinding(report, PolicyMissingEvent) {
			t.Fatalf("changed message must fail exact exception, findings = %#v", report.Findings)
		}
	})

	t.Run("legacyExceptionCountRejectsNewDuplicate", func(t *testing.T) {
		report, err := CheckPolicy(filepath.Join("testdata", "policy_legacy_duplicate"), PolicyOptions{LegacyExceptions: []LegacyException{{
			PackagePath: "uvplatform.cn/uvp-gb28181/internal/loggingcontract/testdata/policy_legacy_duplicate",
			File:        "legacy.go",
			Function:    "Historical",
			Method:      "Info",
			Message:     "historic message",
			Count:       1,
			Reason:      "fixture historical compatibility",
		}}})
		if err != nil {
			t.Fatal(err)
		}
		if !hasPolicyFinding(report, PolicyMissingEvent) {
			t.Fatalf("new duplicate must fail exact exception count, findings = %#v", report.Findings)
		}
	})

	t.Run("buildIgnoredSourcesAreReportedWithExactReason", func(t *testing.T) {
		report, err := CheckPolicy(serverRoot(t), PolicyOptions{Patterns: []string{"./internal/loggingcontract"}})
		if err != nil {
			t.Fatal(err)
		}
		found := 0
		for _, excluded := range report.Excluded {
			if excluded.Path == "resource/database/gb28181/migrations/run-merge-sip-log.go" || excluded.Path == "resource/database/gb28181/migrations/remove_sip_log_new_menu.go" {
				found++
				if excluded.Reason == "" {
					t.Errorf("build-ignored file %s has no reason", excluded.Path)
				}
			}
		}
		if found != 2 {
			t.Fatalf("exact build-ignored exceptions = %d, want 2", found)
		}
		if hasPolicyFinding(report, PolicyUnreviewedExcluded) {
			t.Fatalf("reviewed build-ignored files must not create unreviewed findings: %#v", report.Findings)
		}
	})
}

func mustCheckPolicyFixture(t *testing.T, name string, options ...PolicyOptions) PolicyReport {
	t.Helper()
	root := filepath.Join("testdata", name)
	var opts PolicyOptions
	if len(options) != 0 {
		opts = options[0]
	}
	report, err := CheckPolicy(root, opts)
	if err != nil {
		t.Fatalf("CheckPolicy(%s): %v", root, err)
	}
	return report
}

func hasPolicyFinding(report PolicyReport, kind PolicyFindingKind) bool {
	for _, finding := range report.Findings {
		if finding.Kind == kind {
			return true
		}
	}
	return false
}

func countPolicyFindings(report PolicyReport, kind PolicyFindingKind) int {
	count := 0
	for _, finding := range report.Findings {
		if finding.Kind == kind {
			count++
		}
	}
	return count
}

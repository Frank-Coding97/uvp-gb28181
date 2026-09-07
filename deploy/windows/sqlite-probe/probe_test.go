package main

import (
	"strings"
	"testing"
)

func TestSQLiteVersionAtLeastRepairBaseline(t *testing.T) {
	for _, tc := range []struct {
		version string
		want    bool
	}{
		{version: "3.51.2", want: false},
		{version: "3.51.3", want: true},
		{version: "3.53.4", want: true},
		{version: "3.9.10", want: false},
		{version: "", want: false},
	} {
		if got := versionAtLeast(tc.version, minimumSQLiteVersion); got != tc.want {
			t.Errorf("versionAtLeast(%q) = %v, want %v", tc.version, got, tc.want)
		}
	}
}

func TestProbeCoversRequiredSQLiteContracts(t *testing.T) {
	report, err := runProbe()
	if err != nil {
		t.Fatal(err)
	}
	if !report.Passed {
		t.Fatalf("probe failed: %s", report.Error)
	}
	if !versionAtLeast(report.SQLiteVersion, minimumSQLiteVersion) {
		t.Fatalf("SQLite runtime %q is below %s", report.SQLiteVersion, minimumSQLiteVersion)
	}
	if report.JournalMode != "wal" || report.Synchronous != 2 || report.ForeignKeys != 1 || report.BusyTimeoutMS != busyTimeoutMS {
		t.Fatalf("unexpected connection settings: %+v", report)
	}
	if report.MaxOpenConnections != 1 || report.MaxIdleConnections != 1 {
		t.Fatalf("database/sql pool is not single-connection: %+v", report)
	}
	for _, required := range []string{
		"crud_transactions",
		"reopen_integrity",
		"concurrent_writes",
		"bounded_context_wait",
	} {
		caseResult, ok := report.Cases[required]
		if !ok || !caseResult.Passed {
			t.Fatalf("required case %q did not pass: %+v", required, caseResult)
		}
	}
	if !strings.Contains(report.DatabasePath, " ") || !strings.Contains(report.DatabasePath, "中文") {
		t.Fatalf("database path does not exercise Chinese/space path: %q", report.DatabasePath)
	}
}

package loggingcontract

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestLoggingInventory(t *testing.T) {
	t.Run("aliasesAndInjectedConstructors", func(t *testing.T) {
		report := mustScanFixture(t, "aliases")

		if got := countSites(report, KindZapLog); got != 3 {
			t.Fatalf("ZapLog/injected logger calls = %d, want 3", got)
		}
		if got := countSites(report, KindStandardLog); got != 1 {
			t.Fatalf("stdlib log calls = %d, want 1", got)
		}
		if got := countSites(report, KindSlog); got != 1 {
			t.Fatalf("slog calls = %d, want 1", got)
		}
		if got := countSites(report, KindZapConstructor); got < 3 {
			t.Fatalf("zap constructors = %d, want at least 3", got)
		}
		if got := countSites(report, KindZapField); got != 2 {
			t.Fatalf("zap field calls = %d, want 2", got)
		}
		if got := countSites(report, KindStdoutBypass); got != 1 {
			t.Fatalf("stdout bypasses = %d, want 1", got)
		}

		var injected, global bool
		for _, site := range report.Sites {
			if site.Kind != KindZapLog {
				continue
			}
			injected = injected || site.LoggerSource == LoggerInjected
			global = global || site.LoggerSource == LoggerGlobal
		}
		if !injected || !global {
			t.Fatalf("logger sources = global:%v injected:%v, want both", global, injected)
		}
		if len(report.Excluded) == 0 {
			t.Fatal("expected explicit testdata exclusion records")
		}
	})

	t.Run("constantAndDynamicMessages", func(t *testing.T) {
		report := mustScanFixture(t, "messages")
		var static, dynamic int
		for _, site := range report.Sites {
			if site.Kind != KindZapLog && site.Kind != KindStandardLog && site.Kind != KindStdoutBypass {
				continue
			}
			switch site.MessageClass {
			case MessageStatic:
				static++
			case MessageDynamic:
				dynamic++
			}
		}
		if static != 3 {
			t.Fatalf("static messages = %d, want 3", static)
		}
		if dynamic != 3 {
			t.Fatalf("dynamic messages = %d, want 3", dynamic)
		}
		for _, site := range report.Sites {
			if site.MessageClass == MessageDynamic && site.SourceText != "" {
				t.Fatalf("dynamic site %s:%d exposes source text", site.File, site.Line)
			}
		}
	})

	t.Run("negativePolicySamples", func(t *testing.T) {
		bad := mustScanFixture(t, filepath.Join("negative"))
		assertIssueKinds(t, bad.Issues, IssueStdoutBypass, IssueBareAny, IssueDynamicError)

		clean := mustScanFixture(t, "negative_clean")
		if len(clean.Issues) != 0 {
			t.Fatalf("clean fixture issues = %#v", clean.Issues)
		}
	})

	t.Run("realSourceOwnershipAndBoundaries", func(t *testing.T) {
		root := serverRoot(t)
		report, err := Scan(root, Options{})
		if err != nil {
			t.Fatal(err)
		}
		if len(report.Sites) == 0 {
			t.Fatal("real source inventory is empty")
		}
		if len(report.Routes) < 10 {
			t.Fatalf("registered routes = %d, want at least 10", len(report.Routes))
		}
		if len(report.DBBoundaries) == 0 {
			t.Fatal("real source DB boundary inventory is empty")
		}
		if len(report.CallEdges) == 0 {
			t.Fatal("real source call inventory is empty")
		}
		if len(report.Unassigned) != 0 {
			t.Fatalf("unassigned candidates = %#v", report.Unassigned[:min(5, len(report.Unassigned))])
		}

		for _, site := range report.Sites {
			want := ""
			if strings.HasPrefix(site.File, "app/controllers/") {
				want = "T08"
			}
			if strings.HasPrefix(site.File, "app/gb28181/controllers/") || strings.HasPrefix(site.File, "app/gb28181/cascade/controller/") {
				want = "T09"
			}
			if want != "" && site.OwnerTask != want {
				t.Errorf("HTTP controller %s:%d assigned %s, want %s", site.File, site.Line, site.OwnerTask, want)
			}
		}
		allowed := map[string]bool{
			"T05": true, "T08": true, "T09": true, "T10": true,
			"T11": true, "T12": true, "T14": true,
		}
		for _, site := range report.Sites {
			if !allowed[site.OwnerTask] {
				t.Fatalf("site %s:%d has owner %q", site.File, site.Line, site.OwnerTask)
			}
		}
		for _, route := range report.Routes {
			if route.OwnerTask == "" || route.ResolutionNote == "" {
				t.Fatalf("route %s:%d lacks ownership or resolution note: %#v", route.File, route.Line, route)
			}
		}
	})
}

func mustScanFixture(t *testing.T, name string) Report {
	t.Helper()
	root := filepath.Join("testdata", name)
	report, err := Scan(root, Options{})
	if err != nil {
		t.Fatalf("Scan(%s): %v", root, err)
	}
	return report
}

func serverRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime caller unavailable")
	}
	root, err := filepath.Abs(filepath.Join(filepath.Dir(file), "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func countSites(report Report, kind SiteKind) int {
	count := 0
	for _, site := range report.Sites {
		if site.Kind == kind {
			count++
		}
	}
	return count
}

func assertIssueKinds(t *testing.T, issues []Issue, expected ...IssueKind) {
	t.Helper()
	got := make(map[IssueKind]bool, len(issues))
	for _, issue := range issues {
		got[issue.Kind] = true
	}
	for _, kind := range expected {
		if !got[kind] {
			t.Errorf("issue kind %q not found in %#v", kind, issues)
		}
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

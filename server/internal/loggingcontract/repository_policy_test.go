package loggingcontract

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestLoggingPolicyRepository(t *testing.T) {
	report, err := CheckRepositoryPolicy(serverRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Packages) == 0 || len(report.Inventory.Sites) == 0 {
		t.Fatal("repository gate must inspect actual production sources")
	}
	for _, finding := range report.Findings {
		t.Errorf("%s:%d %s %s: %s", finding.File, finding.Line, finding.Function, finding.Kind, finding.Description)
	}
}

func TestLoggingPolicyAdapterReviewRejectsChanges(t *testing.T) {
	root := t.TempDir()
	const source = "package fixture\n"
	if err := os.WriteFile(filepath.Join(root, "adapter.go"), []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	finding := PolicyFinding{File: "adapter.go", Function: "emit", Receiver: "*Adapter", Method: "Info", Kind: PolicyDynamicEvent}
	review := adapterReview{File: "adapter.go", SHA256: fmt.Sprintf("%x", sha256.Sum256([]byte(source))), Reason: "test adapter contract", Calls: []adapterCall{{Function: "emit", Receiver: "*Adapter", Method: "Info", Kind: PolicyDynamicEvent, Count: 1}}}
	for _, count := range []int{0, 1, 2} {
		report := PolicyReport{Root: root, Findings: make([]PolicyFinding, count)}
		for i := range report.Findings {
			report.Findings[i] = finding
		}
		applyAdapterReviews(&report, []adapterReview{review})
		if report.HasFindings() != (count != 1) {
			t.Fatalf("count %d findings = %v", count, report.Findings)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "adapter.go"), []byte(source+"// changed forwarding contract\n"), 0600); err != nil {
		t.Fatal(err)
	}
	report := PolicyReport{Root: root, Findings: []PolicyFinding{finding}}
	applyAdapterReviews(&report, []adapterReview{review})
	if len(report.Findings) != 2 || !hasPolicyFinding(report, PolicyExceptionMismatch) {
		t.Fatalf("changed source must retain its finding and require review: %v", report.Findings)
	}
}

package loggingcontract

import (
	"crypto/sha256"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

//go:embed reviewed_legacy.json
var reviewedLegacyJSON []byte

// RepositoryPolicyOptions applies only the reviewed historical call identities
// in this application. Fixtures and other source trees use explicit options.
func RepositoryPolicyOptions() (PolicyOptions, error) {
	var legacy []LegacyException
	if err := json.Unmarshal(reviewedLegacyJSON, &legacy); err != nil {
		return PolicyOptions{}, err
	}
	return PolicyOptions{LegacyExceptions: legacy}, nil
}

//go:embed reviewed_adapters.json
var reviewedAdaptersJSON []byte

type adapterReview struct {
	File   string        `json:"file"`
	SHA256 string        `json:"sha256"`
	Reason string        `json:"reason"`
	Calls  []adapterCall `json:"calls"`
}

type adapterCall struct {
	Function string            `json:"function"`
	Receiver string            `json:"receiver"`
	Method   string            `json:"method"`
	Kind     PolicyFindingKind `json:"kind"`
	Count    int               `json:"count"`
}

type RepositoryPolicyReport struct {
	PolicyReport
	Adapters []adapterReview `json:"reviewed_adapters"`
}

// CheckRepositoryPolicy keeps generic findings visible unless an exact reviewed
// adapter call matches. Its source digest also seals dynamic adapter behavior:
// editing even an existing forwarding call requires reviewing the adapter again.
func CheckRepositoryPolicy(root string) (RepositoryPolicyReport, error) {
	opts, err := RepositoryPolicyOptions()
	if err != nil {
		return RepositoryPolicyReport{}, err
	}
	report, err := CheckPolicy(root, opts)
	if err != nil {
		return RepositoryPolicyReport{}, err
	}
	var reviews []adapterReview
	if err := json.Unmarshal(reviewedAdaptersJSON, &reviews); err != nil {
		return RepositoryPolicyReport{}, err
	}
	applyAdapterReviews(&report, reviews)
	return RepositoryPolicyReport{PolicyReport: report, Adapters: reviews}, nil
}

func applyAdapterReviews(report *PolicyReport, reviews []adapterReview) {
	for _, review := range reviews {
		data, err := os.ReadFile(filepath.Join(report.Root, review.File))
		digest := sha256.Sum256(data)
		if err != nil || review.Reason == "" || fmt.Sprintf("%x", digest) != review.SHA256 {
			report.Findings = append(report.Findings, PolicyFinding{Kind: PolicyExceptionMismatch, File: review.File, Description: "reviewed adapter source changed; re-review its forwarding and ownership contract"})
			continue
		}
		for _, call := range review.Calls {
			count := 0
			remaining := make([]PolicyFinding, 0, len(report.Findings))
			for _, finding := range report.Findings {
				if finding.File == review.File && finding.Function == call.Function && finding.Receiver == call.Receiver && finding.Method == call.Method && finding.Kind == call.Kind {
					count++
					if count <= call.Count {
						continue
					}
				}
				remaining = append(remaining, finding)
			}
			report.Findings = remaining
			if count != call.Count || call.Count < 1 {
				report.Findings = append(report.Findings, PolicyFinding{Kind: PolicyExceptionMismatch, File: review.File, Function: call.Function, Receiver: call.Receiver, Method: call.Method, Description: fmt.Sprintf("reviewed adapter calls = %d, want %d: %s", count, call.Count, review.Reason)})
			}
		}
	}
	sortPolicyFindings(report)
}

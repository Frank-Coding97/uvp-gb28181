package schedulerhelper

import (
	"context"
	"strings"
	"sync"
)

type summaryReporterKey struct{}

type summaryReporter struct {
	mu        sync.Mutex
	summaries []string
}

func withSummaryReporter(ctx context.Context, reporter *summaryReporter) context.Context {
	return context.WithValue(ctx, summaryReporterKey{}, reporter)
}

func (r *summaryReporter) report(summary string) {
	summary = strings.TrimSpace(summary)
	if summary == "" {
		return
	}
	r.mu.Lock()
	r.summaries = append(r.summaries, summary)
	r.mu.Unlock()
}

func (r *summaryReporter) snapshot() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return strings.Join(r.summaries, "\n")
}

// ReportSummary adds a human-readable summary to the current job execution.
// Multiple reports are joined by newlines and remain isolated between executions.
func ReportSummary(ctx context.Context, summary string) {
	if ctx == nil {
		return
	}
	if reporter, ok := ctx.Value(summaryReporterKey{}).(*summaryReporter); ok && reporter != nil {
		reporter.report(summary)
	}
}

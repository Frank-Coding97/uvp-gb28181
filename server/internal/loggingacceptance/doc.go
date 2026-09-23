// Package loggingacceptance verifies logging through an isolated backend process.
// Acceptance-only HTTP routes require the logging_acceptance build tag.
//
// The component half of the fixtures - the scheduler executor, its parameter
// validation, and the two static events it emits - carries no build tag on
// purpose, so `go test ./...` (what CI runs) exercises it. See
// acceptance_executor.go and C09.5 in docs/logging-governance/content-plan.md.
package loggingacceptance

// Package loggingcontract contains a read-only source inventory used while
// migrating the application's logging boundaries. T01 intentionally uses a
// lightweight Go AST pass; complete type and interface policy gates belong to
// T14 after the logging contracts have been migrated.
package loggingcontract

import "encoding/json"

// Options controls the source tree walked by Scan.
type Options struct {
	// IncludeTests includes *_test.go files. Tests are excluded by default so
	// the migration inventory describes production code only.
	IncludeTests bool
	// ExcludeDirs adds directory basenames to the built-in exclusions.
	ExcludeDirs []string
}

// SiteKind identifies a source-level logging or output boundary.
type SiteKind string

const (
	KindZapLog         SiteKind = "zap_log"
	KindZapField       SiteKind = "zap_field"
	KindZapConstructor SiteKind = "zap_constructor"
	KindStandardLog    SiteKind = "stdlib_log"
	KindSlog           SiteKind = "slog"
	KindFmtOutput      SiteKind = "fmt_output"
	KindStdoutBypass   SiteKind = "stdout_bypass"
	KindDBBoundary     SiteKind = "db_boundary"
)

// LoggerSource describes how a logger value reaches a call site.
type LoggerSource string

const (
	LoggerGlobal   LoggerSource = "global"
	LoggerInjected LoggerSource = "injected"
	LoggerLocal    LoggerSource = "local"
	LoggerBridge   LoggerSource = "bridge"
	LoggerUnknown  LoggerSource = "unknown"
)

// MessageClass is intentionally a conservative syntactic classification. A
// dynamic message is a migration candidate; it is not proof that a secret is
// present, and a static message is not proof that its fields are safe.
type MessageClass string

const (
	MessageStatic  MessageClass = "static"
	MessageDynamic MessageClass = "dynamic"
	MessageNone    MessageClass = "none"
)

// ContextSource records whether a site is reachable from a registered HTTP
// handler using the scanner's bounded name-based call graph.
type ContextSource string

const (
	ContextHTTP       ContextSource = "http_route"
	ContextBackground ContextSource = "background_or_unresolved"
)

// Site is one source-level candidate. SourceText is kept for in-process
// diagnostics only and is never populated or encoded by the scanner; this
// prevents a report from accidentally becoming a source/value leak.
type Site struct {
	File           string        `json:"file"`
	Package        string        `json:"package"`
	Function       string        `json:"function"`
	Receiver       string        `json:"receiver,omitempty"`
	Line           int           `json:"line"`
	Column         int           `json:"column"`
	Kind           SiteKind      `json:"kind"`
	Operation      string        `json:"operation,omitempty"`
	LoggerSource   LoggerSource  `json:"logger_source,omitempty"`
	MessageClass   MessageClass  `json:"message_class,omitempty"`
	ContextSource  ContextSource `json:"context_source,omitempty"`
	OwnerTask      string        `json:"owner_task"`
	OwnerReason    string        `json:"owner_reason"`
	RouteNames     []string      `json:"route_names,omitempty"`
	ResolutionNote string        `json:"resolution_note,omitempty"`
	SourceText     string        `json:"-"`

	functionID string
}

// Route records a statically recognized HTTP registration. Dynamic path and
// handler expressions remain visible with a non-empty ResolutionNote.
type Route struct {
	File           string   `json:"file"`
	Package        string   `json:"package"`
	Function       string   `json:"function"`
	Line           int      `json:"line"`
	Column         int      `json:"column"`
	Method         string   `json:"method"`
	Path           string   `json:"path,omitempty"`
	PathLiteral    bool     `json:"path_literal"`
	Handlers       []string `json:"handlers,omitempty"`
	OwnerTask      string   `json:"owner_task"`
	OwnerReason    string   `json:"owner_reason"`
	Resolved       bool     `json:"resolved"`
	ResolutionNote string   `json:"resolution_note"`
}

// CallEdge is a bounded, name-based call graph edge. Resolved means a unique
// local declaration matched the name; it does not claim full Go type
// resolution across packages or interfaces.
type CallEdge struct {
	File           string `json:"file"`
	Line           int    `json:"line"`
	Caller         string `json:"caller"`
	Callee         string `json:"callee"`
	Resolved       bool   `json:"resolved"`
	ResolutionNote string `json:"resolution_note"`
}

// DBBoundary identifies a direct global DB call or a conservative ORM call.
type DBBoundary struct {
	File           string        `json:"file"`
	Package        string        `json:"package"`
	Function       string        `json:"function"`
	Line           int           `json:"line"`
	Column         int           `json:"column"`
	Operation      string        `json:"operation"`
	ContextSource  ContextSource `json:"context_source"`
	OwnerTask      string        `json:"owner_task"`
	OwnerReason    string        `json:"owner_reason"`
	RouteNames     []string      `json:"route_names,omitempty"`
	ResolutionNote string        `json:"resolution_note"`
}

// IssueKind identifies an actionable syntactic policy finding.
type IssueKind string

const (
	IssueStdoutBypass   IssueKind = "stdout_bypass"
	IssueBareAny        IssueKind = "bare_zap_any"
	IssueDynamicMessage IssueKind = "dynamic_message"
	IssueDynamicError   IssueKind = "dynamic_error_string"
)

// Issue never contains source snippets or evaluated argument values.
type Issue struct {
	Kind        IssueKind `json:"kind"`
	File        string    `json:"file"`
	Function    string    `json:"function"`
	Line        int       `json:"line"`
	Column      int       `json:"column"`
	OwnerTask   string    `json:"owner_task"`
	Description string    `json:"description"`
}

// ExcludedFile explains why a path was intentionally omitted from the
// production inventory.
type ExcludedFile struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

// Report is the complete inventory produced by Scan.
type Report struct {
	Root         string         `json:"root"`
	Sites        []Site         `json:"sites"`
	Routes       []Route        `json:"routes"`
	CallEdges    []CallEdge     `json:"call_edges"`
	DBBoundaries []DBBoundary   `json:"db_boundaries"`
	Issues       []Issue        `json:"issues"`
	Excluded     []ExcludedFile `json:"excluded"`
	Unassigned   []Site         `json:"unassigned"`
}

// MarshalJSON is provided as a named API so callers can persist a report
// without knowing its internal graph bookkeeping fields.
func (r Report) MarshalJSON() ([]byte, error) {
	type plain Report
	return json.Marshal(plain(r))
}

func (r Report) SitesByTask(task string) []Site {
	result := make([]Site, 0)
	for _, site := range r.Sites {
		if site.OwnerTask == task {
			result = append(result, site)
		}
	}
	return result
}

func (r Report) HasIssues(kind IssueKind) bool {
	for _, issue := range r.Issues {
		if issue.Kind == kind {
			return true
		}
	}
	return false
}

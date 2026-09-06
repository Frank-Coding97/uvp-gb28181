package loggingcontract

import (
	"context"
	"fmt"
	"go/ast"
	"go/constant"
	"go/parser"
	"go/token"
	"go/types"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/tools/go/packages"
)

const (
	zapPackagePath     = "go.uber.org/zap"
	logPackagePath     = "log"
	slogPackagePath    = "log/slog"
	fmtPackagePath     = "fmt"
	osPackagePath      = "os"
	ioPackagePath      = "io"
	loggingPackagePath = "uvplatform.cn/uvp-gb28181/app/utils/logging"
)

// PolicyOptions controls the type-aware logging policy check.
type PolicyOptions struct {
	// IncludeTests includes test packages in the typed check. Production checks
	// leave this false so tests cannot mask runtime policy findings.
	IncludeTests bool
	// Patterns are go/packages patterns evaluated relative to Root. Empty uses
	// ./... for the module rooted at Root.
	Patterns []string
	// ExcludeDirs extends the source inventory's directory exclusions. It is
	// only for tooling fixtures or an explicitly reviewed scan boundary.
	ExcludeDirs []string
	// LegacyExceptions permits an exact historical static-message call to use
	// the runtime legacy.log event. Every exception must identify its package,
	// file, function, method and message; Count limits matching call sites.
	LegacyExceptions []LegacyException
}

// LegacyException is an exact, reviewable compatibility exception. It is
// intentionally narrower than a package or directory allow-list.
type LegacyException struct {
	PackagePath string `json:"package_path"`
	File        string `json:"file"`
	Function    string `json:"function"`
	Receiver    string `json:"receiver,omitempty"`
	Method      string `json:"method"`
	Message     string `json:"message"`
	Count       int    `json:"count"`
	Reason      string `json:"reason"`
}

// PolicyFindingKind identifies one type-aware policy finding.
type PolicyFindingKind string

const (
	PolicyPackageError       PolicyFindingKind = "package_error"
	PolicyUnresolvedLogger   PolicyFindingKind = "unresolved_logger"
	PolicyLoggerConstructor  PolicyFindingKind = "logger_constructor"
	PolicyDynamicMessage     PolicyFindingKind = "dynamic_message"
	PolicyDynamicErrorString PolicyFindingKind = "dynamic_error_string"
	PolicyUnknownField       PolicyFindingKind = "unknown_field"
	PolicyMissingEvent       PolicyFindingKind = "missing_event"
	PolicyDynamicEvent       PolicyFindingKind = "dynamic_event"
	PolicyStdoutBypass       PolicyFindingKind = "stdout_bypass"
	PolicyStdlibLog          PolicyFindingKind = "stdlib_log"
	PolicyUnreviewedExcluded PolicyFindingKind = "unreviewed_excluded_source"
	PolicyExceptionMismatch  PolicyFindingKind = "legacy_exception_mismatch"
	PolicyInvalidException   PolicyFindingKind = "invalid_exception"
)

// PolicyFinding is a source-only diagnostic. It never includes evaluated
// argument values or source snippets.
type PolicyFinding struct {
	Kind        PolicyFindingKind `json:"kind"`
	PackagePath string            `json:"package_path,omitempty"`
	File        string            `json:"file"`
	Package     string            `json:"package,omitempty"`
	Function    string            `json:"function,omitempty"`
	Receiver    string            `json:"receiver,omitempty"`
	Method      string            `json:"method,omitempty"`
	Line        int               `json:"line"`
	Column      int               `json:"column"`
	Argument    string            `json:"argument,omitempty"`
	Message     string            `json:"message,omitempty"`
	Description string            `json:"description"`
}

// PolicyPackage records type-checking status for one loaded package.
type PolicyPackage struct {
	ID          string `json:"id"`
	PackagePath string `json:"package_path"`
	ErrorCount  int    `json:"error_count,omitempty"`
	TypeErrors  int    `json:"type_errors,omitempty"`
}

// PolicyReport is the typed policy result and the original inventory. The
// inventory remains available so callers can compare bounded T01 ownership
// with the stronger type-aware findings without changing Scan semantics.
type PolicyReport struct {
	Root       string            `json:"root"`
	Inventory  Report            `json:"inventory"`
	Packages   []PolicyPackage   `json:"packages"`
	Findings   []PolicyFinding   `json:"findings"`
	Excluded   []ExcludedFile    `json:"excluded"`
	Exceptions []LegacyException `json:"exceptions,omitempty"`
}

// HasFindings reports whether the policy found any unprocessed condition.
func (r PolicyReport) HasFindings() bool { return len(r.Findings) != 0 }

func CheckPolicy(root string, opts PolicyOptions) (PolicyReport, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return PolicyReport{}, fmt.Errorf("logging policy root: %w", err)
	}
	stat, err := os.Stat(absRoot)
	if err != nil {
		return PolicyReport{}, fmt.Errorf("logging policy root %q: %w", root, err)
	}
	if !stat.IsDir() {
		return PolicyReport{}, fmt.Errorf("logging policy root %q is not a directory", root)
	}
	inventory, err := Scan(absRoot, Options{IncludeTests: opts.IncludeTests, ExcludeDirs: opts.ExcludeDirs})
	if err != nil {
		return PolicyReport{}, err
	}
	report := PolicyReport{
		Root:       absRoot,
		Inventory:  inventory,
		Packages:   make([]PolicyPackage, 0),
		Findings:   make([]PolicyFinding, 0),
		Excluded:   append([]ExcludedFile(nil), inventory.Excluded...),
		Exceptions: append([]LegacyException(nil), opts.LegacyExceptions...),
	}
	for _, file := range buildIgnoredFiles(absRoot) {
		reason, reviewed := reviewedBuildIgnoredFile(file)
		report.Excluded = append(report.Excluded, ExcludedFile{Path: file, Reason: reason})
		if !reviewed {
			report.Findings = append(report.Findings, PolicyFinding{
				Kind:        PolicyUnreviewedExcluded,
				File:        file,
				Description: "build-ignored Go source was not covered by an exact reviewed exception",
			})
		}
	}

	patterns := append([]string(nil), opts.Patterns...)
	if len(patterns) == 0 {
		patterns = []string{"./..."}
	}
	packagesFound, loadErr := packages.Load(&packages.Config{
		Context: context.Background(),
		Dir:     absRoot,
		Mode:    packages.LoadAllSyntax,
		Tests:   opts.IncludeTests,
	}, patterns...)
	if loadErr != nil {
		return report, fmt.Errorf("load typed logging packages: %w", loadErr)
	}
	if len(packagesFound) == 0 {
		return report, fmt.Errorf("load typed logging packages: no packages matched %v", patterns)
	}

	usage := make([]int, len(opts.LegacyExceptions))
	for _, pkg := range packagesFound {
		status := PolicyPackage{ID: pkg.ID, PackagePath: pkg.PkgPath, ErrorCount: len(pkg.Errors), TypeErrors: len(pkg.TypeErrors)}
		report.Packages = append(report.Packages, status)
		analyzePackage(absRoot, pkg, opts.LegacyExceptions, usage, &report)
	}
	for index, exception := range opts.LegacyExceptions {
		expected := maxExceptionCount(exception.Count)
		if exception.PackagePath == "" || exception.File == "" || exception.Function == "" || exception.Method == "" || exception.Message == "" || exception.Reason == "" {
			report.Findings = append(report.Findings, PolicyFinding{
				Kind:        PolicyInvalidException,
				File:        exception.File,
				Function:    exception.Function,
				Method:      exception.Method,
				Description: "legacy exception must identify package, file, function, method, static message and reason",
			})
			continue
		}
		if usage[index] != expected {
			report.Findings = append(report.Findings, PolicyFinding{
				Kind:        PolicyExceptionMismatch,
				PackagePath: exception.PackagePath,
				File:        exception.File,
				Function:    exception.Function,
				Receiver:    exception.Receiver,
				Method:      exception.Method,
				Description: fmt.Sprintf("legacy exception matched %d call(s), want exactly %d: %s", usage[index], expected, exception.Reason),
			})
		}
	}
	sortPolicyFindings(&report)
	sort.Slice(report.Packages, func(i, j int) bool {
		if report.Packages[i].PackagePath != report.Packages[j].PackagePath {
			return report.Packages[i].PackagePath < report.Packages[j].PackagePath
		}
		return report.Packages[i].ID < report.Packages[j].ID
	})
	return report, nil
}

type policyLoggerKind uint8

const (
	policyZapLogger policyLoggerKind = iota + 1
	policySlogLogger
	policyStdLogger
)

type policyLoggerCall struct {
	kind         policyLoggerKind
	method       string
	messageIndex int
	fieldStart   int
	inspectStart int
	fieldMode    policyFieldMode
}

type policyFieldMode uint8

const (
	policyNoFields policyFieldMode = iota
	policyZapFields
	policyKeyValues
	policySlogAttrs
	policyPlainArgs
)

type policyLoggerFieldPart struct {
	mode policyFieldMode
	args []ast.Expr
}

type policyAnalyzer struct {
	pkg        *packages.Package
	exceptions []LegacyException
	usage      []int
	report     *PolicyReport
	origins    map[types.Object][]ast.Expr
}

func analyzePackage(root string, pkg *packages.Package, exceptions []LegacyException, usage []int, report *PolicyReport) {
	for _, packageError := range pkg.Errors {
		report.Findings = append(report.Findings, PolicyFinding{
			Kind:        PolicyPackageError,
			PackagePath: pkg.PkgPath,
			Description: packageError.Error(),
		})
	}
	for _, typeError := range pkg.TypeErrors {
		report.Findings = append(report.Findings, PolicyFinding{
			Kind:        PolicyPackageError,
			PackagePath: pkg.PkgPath,
			Description: typeError.Error(),
		})
	}
	if pkg.IllTyped && len(pkg.Errors) == 0 && len(pkg.TypeErrors) == 0 {
		report.Findings = append(report.Findings, PolicyFinding{
			Kind:        PolicyPackageError,
			PackagePath: pkg.PkgPath,
			Description: "package was reported ill-typed without a detailed loader error",
		})
	}
	analyzer := policyAnalyzer{pkg: pkg, exceptions: exceptions, usage: usage, report: report, origins: make(map[types.Object][]ast.Expr)}
	for _, syntaxFile := range pkg.Syntax {
		path := analyzer.filePath(syntaxFile)
		if path == "" || !analyzer.compiledFile(path) {
			continue
		}
		analyzer.collectOrigins(syntaxFile, pkg.TypesInfo)
		rel, err := filepath.Rel(root, path)
		if err != nil {
			continue
		}
		rel = filepath.ToSlash(rel)
		for _, declaration := range syntaxFile.Decls {
			switch declaration := declaration.(type) {
			case *ast.FuncDecl:
				if declaration.Body != nil {
					analyzer.walkBody(syntaxFile, rel, declaration.Body, declaration.Name.Name, receiverName(declaration), pkg.TypesInfo)
				}
			case *ast.GenDecl:
				analyzer.walkNode(syntaxFile, rel, declaration, "<package>", "", pkg.TypesInfo)
			}
		}
	}
}

func (a policyAnalyzer) filePath(file *ast.File) string {
	if a.pkg == nil || a.pkg.Fset == nil || file == nil {
		return ""
	}
	return filepath.Clean(a.pkg.Fset.PositionFor(file.Pos(), false).Filename)
}

func (a policyAnalyzer) compiledFile(path string) bool {
	clean := filepath.Clean(path)
	for _, candidate := range a.pkg.CompiledGoFiles {
		if filepath.Clean(candidate) == clean {
			return true
		}
	}
	return false
}

// collectOrigins records the small amount of local value flow needed to keep
// logger field slices from becoming an unchecked escape hatch. It is
// deliberately object keyed, so same-named variables in different scopes do
// not get conflated. Multiple assignments are retained and are reported as
// unresolved when the value is later expanded into a logger call.
func (a policyAnalyzer) collectOrigins(file *ast.File, info *types.Info) {
	if file == nil || info == nil || a.origins == nil {
		return
	}
	ast.Inspect(file, func(node ast.Node) bool {
		switch statement := node.(type) {
		case *ast.ValueSpec:
			for index, name := range statement.Names {
				if index >= len(statement.Values) {
					continue
				}
				if object := info.Defs[name]; object != nil {
					a.origins[object] = append(a.origins[object], statement.Values[index])
				}
			}
		case *ast.AssignStmt:
			for index, left := range statement.Lhs {
				if index >= len(statement.Rhs) {
					continue
				}
				ident, ok := left.(*ast.Ident)
				if !ok {
					continue
				}
				if object := info.ObjectOf(ident); object != nil {
					a.origins[object] = append(a.origins[object], statement.Rhs[index])
				}
			}
		}
		return true
	})
}

func (a policyAnalyzer) walkBody(file *ast.File, rel string, body *ast.BlockStmt, function, receiver string, info *types.Info) {
	a.walkNode(file, rel, body, function, receiver, info)
}

func (a policyAnalyzer) walkNode(file *ast.File, rel string, node ast.Node, function, receiver string, info *types.Info) {
	if node == nil {
		return
	}
	ast.Inspect(node, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		position := a.pkg.Fset.PositionFor(call.Pos(), false)
		if isStdoutBypassCall(call, info) {
			if !a.allowed(rel, function, receiver, PolicyStdoutBypass) {
				a.addFinding(PolicyFinding{Kind: PolicyStdoutBypass, File: rel, PackagePath: a.pkg.PkgPath, Package: a.pkg.Name, Function: function, Receiver: receiver, Line: position.Line, Column: position.Column, Description: "stdout/stderr write bypasses the unified logger"})
			}
		}
		if isLoggerConstructor(call, info) {
			if !a.allowed(rel, function, receiver, PolicyLoggerConstructor) {
				a.addFinding(PolicyFinding{Kind: PolicyLoggerConstructor, File: rel, PackagePath: a.pkg.PkgPath, Package: a.pkg.Name, Function: function, Receiver: receiver, Line: position.Line, Column: position.Column, Description: "logger is constructed at a business call site instead of being injected or using the shared bridge"})
			}
		}
		if loggerCall, ok := identifyLoggerCall(call, info); ok {
			a.inspectLoggerCall(call, loggerCall, rel, function, receiver, position, info)
		} else if likelyUnresolvedLoggerCall(call, info) {
			a.addFinding(PolicyFinding{Kind: PolicyUnresolvedLogger, File: rel, PackagePath: a.pkg.PkgPath, Package: a.pkg.Name, Function: function, Receiver: receiver, Method: selectorMethod(call), Line: position.Line, Column: position.Column, Description: "suspected logger call could not be resolved by go/types"})
		}
		return true
	})
}

func (a policyAnalyzer) inspectLoggerCall(call *ast.CallExpr, loggerCall policyLoggerCall, rel, function, receiver string, position token.Position, info *types.Info) {
	message := ""
	if loggerCall.messageIndex >= 0 && loggerCall.messageIndex < len(call.Args) {
		message, _ = stringConstantOK(info, call.Args[loggerCall.messageIndex])
		if !isStringConstant(info, call.Args[loggerCall.messageIndex]) && !a.allowed(rel, function, receiver, PolicyDynamicMessage) {
			a.addFinding(PolicyFinding{Kind: PolicyDynamicMessage, File: rel, PackagePath: a.pkg.PkgPath, Package: a.pkg.Name, Function: function, Receiver: receiver, Method: loggerCall.method, Argument: "message", Line: position.Line, Column: position.Column, Description: "logger message is not a compile-time string constant"})
		}
	}
	for index := loggerCall.inspectStart; index < len(call.Args); index++ {
		if containsErrorStringFlow(call.Args[index], info, a.origins, make(map[types.Object]bool)) {
			a.addFinding(PolicyFinding{Kind: PolicyDynamicErrorString, File: rel, PackagePath: a.pkg.PkgPath, Package: a.pkg.Name, Function: function, Receiver: receiver, Method: loggerCall.method, Argument: "argument", Line: position.Line, Column: position.Column, Description: "error text is rendered inside a logger argument; use a typed safe error field"})
		}
	}
	for _, part := range loggerChainFieldParts(call, info) {
		for _, argument := range part.args {
			if containsErrorStringFlow(argument, info, a.origins, make(map[types.Object]bool)) {
				a.addFinding(PolicyFinding{Kind: PolicyDynamicErrorString, File: rel, PackagePath: a.pkg.PkgPath, Package: a.pkg.Name, Function: function, Receiver: receiver, Method: loggerCall.method, Argument: "argument", Line: position.Line, Column: position.Column, Description: "error text is rendered inside a logger argument; use a typed safe error field"})
			}
		}
	}
	fieldStart := loggerCall.fieldStart
	if fieldStart < 0 {
		fieldStart = 0
	}
	if fieldStart > len(call.Args) {
		fieldStart = len(call.Args)
	}
	parts := append([]policyLoggerFieldPart{{mode: loggerCall.fieldMode, args: call.Args[fieldStart:]}}, loggerChainFieldParts(call, info)...)

	if loggerCall.kind == policyStdLogger {
		if !a.allowed(rel, function, receiver, PolicyStdlibLog) && !a.matchLegacy(rel, function, receiver, loggerCall.method, message) {
			a.addFinding(PolicyFinding{Kind: PolicyStdlibLog, File: rel, PackagePath: a.pkg.PkgPath, Package: a.pkg.Name, Function: function, Receiver: receiver, Method: loggerCall.method, Message: message, Line: position.Line, Column: position.Column, Description: "direct standard-library log output must use the reviewed one-way bridge"})
		}
	}

	if loggerCall.kind == policyZapLogger || loggerCall.kind == policySlogLogger {
		eventState := loggerEventState(parts, info, a.origins)
		switch eventState {
		case eventMissing:
			if !a.matchLegacy(rel, function, receiver, loggerCall.method, message) && !a.allowed(rel, function, receiver, PolicyMissingEvent) {
				a.addFinding(PolicyFinding{Kind: PolicyMissingEvent, File: rel, PackagePath: a.pkg.PkgPath, Package: a.pkg.Name, Function: function, Receiver: receiver, Method: loggerCall.method, Argument: "event", Message: message, Line: position.Line, Column: position.Column, Description: "migration-range logger call has no static event field"})
			}
		case eventDynamic:
			a.addFinding(PolicyFinding{Kind: PolicyDynamicEvent, File: rel, PackagePath: a.pkg.PkgPath, Package: a.pkg.Name, Function: function, Receiver: receiver, Method: loggerCall.method, Argument: "event", Line: position.Line, Column: position.Column, Description: "logger event must be a compile-time string constant"})
		}
	}

	for _, part := range parts {
		a.inspectLoggerFields(part, rel, function, receiver, loggerCall.method, position, info)
	}
}

func (a policyAnalyzer) inspectLoggerFields(part policyLoggerFieldPart, rel, function, receiver, method string, position token.Position, info *types.Info) {
	if part.mode == policyNoFields || len(part.args) == 0 {
		return
	}
	if part.mode == policyKeyValues {
		for index, argument := range part.args {
			if isLoggingFieldExpression(argument, info) {
				a.inspectFieldExpression(argument, policyZapFields, rel, function, receiver, method, position, info, make(map[types.Object]bool))
				continue
			}
			if index%2 == 0 {
				if _, ok := stringConstantOK(info, argument); !ok {
					a.addFinding(PolicyFinding{Kind: PolicyUnresolvedLogger, File: rel, PackagePath: a.pkg.PkgPath, Package: a.pkg.Name, Function: function, Receiver: receiver, Method: method, Argument: "key", Line: position.Line, Column: position.Column, Description: "logger key/value arguments have a non-static key"})
				}
				continue
			}
			if !isSafeLogValue(argument, info, a.origins, make(map[types.Object]bool)) {
				a.addFinding(PolicyFinding{Kind: PolicyUnknownField, File: rel, PackagePath: a.pkg.PkgPath, Package: a.pkg.Name, Function: function, Receiver: receiver, Method: method, Argument: "value", Line: position.Line, Column: position.Column, Description: "logger key/value argument contains an object whose representation is not statically safe"})
			}
		}
		if len(part.args)%2 != 0 {
			a.addFinding(PolicyFinding{Kind: PolicyUnresolvedLogger, File: rel, PackagePath: a.pkg.PkgPath, Package: a.pkg.Name, Function: function, Receiver: receiver, Method: method, Argument: "key/value", Line: position.Line, Column: position.Column, Description: "logger key/value arguments have an unmatched value"})
		}
		return
	}
	for _, argument := range part.args {
		if part.mode == policyPlainArgs {
			if !isSafeLogValue(argument, info, a.origins, make(map[types.Object]bool)) {
				a.addFinding(PolicyFinding{Kind: PolicyUnknownField, File: rel, PackagePath: a.pkg.PkgPath, Package: a.pkg.Name, Function: function, Receiver: receiver, Method: method, Argument: "argument", Line: position.Line, Column: position.Column, Description: "unstructured logger argument contains an object whose representation is not statically safe"})
			}
			continue
		}
		a.inspectFieldExpression(argument, part.mode, rel, function, receiver, method, position, info, make(map[types.Object]bool))
	}
}

func (a policyAnalyzer) inspectFieldExpression(expression ast.Expr, mode policyFieldMode, rel, function, receiver, method string, position token.Position, info *types.Info, seen map[types.Object]bool) {
	if expression == nil {
		return
	}
	if isUnknownLogField(expression, info) {
		a.addFinding(PolicyFinding{Kind: PolicyUnknownField, File: rel, PackagePath: a.pkg.PkgPath, Package: a.pkg.Name, Function: function, Receiver: receiver, Method: method, Argument: "field", Line: position.Line, Column: position.Column, Description: "unknown logger field requires an explicit safe representation"})
	}
	if info == nil {
		return
	}
	switch value := expression.(type) {
	case *ast.Ident:
		object := info.ObjectOf(value)
		origins := a.origins[object]
		if len(origins) == 0 {
			if isLoggingFieldType(info.TypeOf(expression)) || isLoggingContainerType(info.TypeOf(expression), mode) {
				a.addFinding(PolicyFinding{Kind: PolicyUnresolvedLogger, File: rel, PackagePath: a.pkg.PkgPath, Package: a.pkg.Name, Function: function, Receiver: receiver, Method: method, Argument: "field", Line: position.Line, Column: position.Column, Description: "logger field value is carried by an unresolved variable"})
			}
			return
		}
		if seen[object] {
			return
		}
		seen[object] = true
		for _, origin := range origins {
			a.inspectFieldExpression(origin, mode, rel, function, receiver, method, position, info, seen)
		}
	case *ast.Ellipsis:
		a.inspectFieldExpression(value.Elt, mode, rel, function, receiver, method, position, info, seen)
	case *ast.CompositeLit:
		for _, element := range value.Elts {
			if pair, ok := element.(*ast.KeyValueExpr); ok {
				a.inspectFieldExpression(pair.Value, mode, rel, function, receiver, method, position, info, seen)
				continue
			}
			a.inspectFieldExpression(element, mode, rel, function, receiver, method, position, info, seen)
		}
	case *ast.CallExpr:
		if isAppendCall(value, info) {
			for _, argument := range value.Args {
				a.inspectFieldExpression(argument, mode, rel, function, receiver, method, position, info, seen)
			}
		}
	}
}

func loggerChainFieldParts(call *ast.CallExpr, info *types.Info) []policyLoggerFieldPart {
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return nil
	}
	return collectLoggerChainFieldParts(selector.X, info)
}

func collectLoggerChainFieldParts(expression ast.Expr, info *types.Info) []policyLoggerFieldPart {
	var parts []policyLoggerFieldPart
	switch value := expression.(type) {
	case *ast.CallExpr:
		selector, ok := value.Fun.(*ast.SelectorExpr)
		if !ok {
			return nil
		}
		if mode, ok := loggerDerivationMode(value, info); ok {
			parts = append(parts, policyLoggerFieldPart{mode: mode, args: value.Args})
		}
		parts = append(parts, collectLoggerChainFieldParts(selector.X, info)...)
	case *ast.SelectorExpr:
		parts = append(parts, collectLoggerChainFieldParts(value.X, info)...)
	}
	return parts
}

func (a policyAnalyzer) addFinding(finding PolicyFinding) {
	a.report.Findings = append(a.report.Findings, finding)
}

func (a policyAnalyzer) allowed(file, function, receiver string, kind PolicyFindingKind) bool {
	if file == "internal/loggingcontract/cmd/main.go" && function == "main" && kind == PolicyStdoutBypass {
		return true
	}
	if isLoggingInfrastructureSymbol(file, function, receiver) {
		return kind == PolicyLoggerConstructor || kind == PolicyStdlibLog || kind == PolicyStdoutBypass
	}
	if isFileJobLoggerSymbol(file, function, receiver) {
		return kind == PolicyLoggerConstructor || kind == PolicyStdlibLog || kind == PolicyStdoutBypass || kind == PolicyDynamicMessage
	}
	return false
}

func isLoggingInfrastructureSymbol(file, function, receiver string) bool {
	switch filepath.ToSlash(file) {
	case "app/utils/logging/bootstrap.go":
		return receiver == "" && function == "ReportStartupFailure"
	case "app/utils/logging/bridge.go":
		if receiver == "" {
			switch function {
			case "StandardWriter", "InstallStandardBridge", "NewSlogHandler":
				return true
			}
		}
		if receiver == "standardWriter" || receiver == "*standardWriter" {
			return function == "Write"
		}
		if receiver == "slogHandler" || receiver == "*slogHandler" {
			switch function {
			case "Enabled", "Handle", "WithAttrs", "WithGroup":
				return true
			}
		}
	case "app/utils/logging/context.go":
		return receiver == "" && (function == "WithContext" || function == "FromContext")
	case "app/utils/logging/logger.go":
		return receiver == "" && (function == "NewRuntime" || function == "WithIdentity")
	case "app/utils/logging/sink.go":
		return receiver == "" && (function == "OpenRuntime" || function == "newEmergencyWriter")
	}
	return false
}

func isFileJobLoggerSymbol(file, function, receiver string) bool {
	if filepath.ToSlash(file) != "app/utils/schedulerhelper/logger.go" {
		return false
	}
	if receiver == "" {
		return function == "NewFileJobLogger" || function == "rotateLogFileUnsafe"
	}
	if receiver != "*FileJobLogger" {
		return false
	}
	switch function {
	case "NewFileJobLogger", "rotateLogFileUnsafe", "log", "Fatal":
		return true
	default:
		return false
	}
}

func identifyLoggerCall(call *ast.CallExpr, info *types.Info) (policyLoggerCall, bool) {
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return policyLoggerCall{}, false
	}
	object := selectorObject(info, selector)
	function, ok := object.(*types.Func)
	if !ok || function.Pkg() == nil {
		return policyLoggerCall{}, false
	}
	method := function.Name()
	switch function.Pkg().Path() {
	case zapPackagePath:
		if !zapLoggerMethods[method] || !isZapLoggerMethod(function) {
			return policyLoggerCall{}, false
		}
		return zapLoggerCall(function, method, len(call.Args))
	case slogPackagePath:
		if !slogLoggerMethods[method] {
			return policyLoggerCall{}, false
		}
		messageIndex := 0
		if strings.HasSuffix(method, "Context") {
			messageIndex = 1
		} else if method == "Log" || method == "LogAttrs" {
			messageIndex = 2
		}
		mode := policyKeyValues
		if method == "LogAttrs" {
			mode = policySlogAttrs
		}
		return policyLoggerCall{kind: policySlogLogger, method: method, messageIndex: messageIndex, fieldStart: messageIndex + 1, inspectStart: messageIndex, fieldMode: mode}, true
	case logPackagePath:
		if !standardLoggerMethods[method] {
			return policyLoggerCall{}, false
		}
		messageIndex := 0
		if method == "Output" {
			messageIndex = 1
		}
		return policyLoggerCall{kind: policyStdLogger, method: method, messageIndex: messageIndex, fieldStart: len(call.Args), inspectStart: messageIndex, fieldMode: policyNoFields}, true
	default:
		return policyLoggerCall{}, false
	}
}

func zapLoggerCall(function *types.Func, method string, argumentCount int) (policyLoggerCall, bool) {
	receiver := zapLoggerReceiver(function)
	if receiver == "Logger" {
		if method == "Log" {
			return policyLoggerCall{kind: policyZapLogger, method: method, messageIndex: 1, fieldStart: 2, inspectStart: 1, fieldMode: policyZapFields}, true
		}
		return policyLoggerCall{kind: policyZapLogger, method: method, messageIndex: 0, fieldStart: 1, inspectStart: 0, fieldMode: policyZapFields}, true
	}
	if receiver != "SugaredLogger" {
		return policyLoggerCall{}, false
	}
	call := policyLoggerCall{kind: policyZapLogger, method: method, messageIndex: 0, fieldStart: argumentCount, inspectStart: 0, fieldMode: policyNoFields}
	switch {
	case method == "Log":
		call.messageIndex, call.fieldStart, call.inspectStart, call.fieldMode = 1, argumentCount, 1, policyPlainArgs
	case method == "Logf":
		call.messageIndex, call.fieldStart, call.inspectStart = 1, argumentCount, 1
	case method == "Logw":
		call.messageIndex, call.fieldStart, call.inspectStart, call.fieldMode = 1, 2, 1, policyKeyValues
	case strings.HasSuffix(method, "f"):
		call.fieldStart, call.inspectStart = argumentCount, 0
	case strings.HasSuffix(method, "w"):
		call.fieldStart, call.inspectStart, call.fieldMode = 1, 0, policyKeyValues
	default:
		call.fieldStart, call.inspectStart, call.fieldMode = 1, 0, policyPlainArgs
	}
	return call, true
}

func zapLoggerReceiver(function *types.Func) string {
	if function == nil {
		return ""
	}
	signature, ok := function.Type().(*types.Signature)
	if !ok || signature.Recv() == nil {
		return ""
	}
	typ := signature.Recv().Type()
	if pointer, ok := typ.(*types.Pointer); ok {
		typ = pointer.Elem()
	}
	named, ok := typ.(*types.Named)
	if !ok || named.Obj() == nil || named.Obj().Pkg() == nil || named.Obj().Pkg().Path() != zapPackagePath {
		return ""
	}
	return named.Obj().Name()
}

func isZapLoggerMethod(function *types.Func) bool {
	signature, ok := function.Type().(*types.Signature)
	return ok && signature.Recv() != nil && isLoggerType(signature.Recv().Type(), zapPackagePath)
}

var zapLoggerMethods = map[string]bool{
	"Debug": true, "Info": true, "Warn": true, "Error": true, "DPanic": true, "Panic": true, "Fatal": true,
	"Debugf": true, "Infof": true, "Warnf": true, "Errorf": true, "DPanicf": true, "Panicf": true, "Fatalf": true,
	"Debugw": true, "Infow": true, "Warnw": true, "Errorw": true, "DPanicw": true, "Panicw": true, "Fatalw": true,
	"Logf": true, "Logw": true,
	"Log": true,
}

var standardLoggerMethods = map[string]bool{
	"Print": true, "Printf": true, "Println": true, "Fatal": true, "Fatalf": true, "Fatalln": true,
	"Panic": true, "Panicf": true, "Panicln": true, "Output": true,
}

var slogLoggerMethods = map[string]bool{
	"Debug": true, "Info": true, "Warn": true, "Error": true, "Log": true, "LogAttrs": true,
	"DebugContext": true, "InfoContext": true, "WarnContext": true, "ErrorContext": true,
}

func selectorObject(info *types.Info, selector *ast.SelectorExpr) types.Object {
	if info == nil || selector == nil {
		return nil
	}
	if info.Selections != nil {
		if selection := info.Selections[selector]; selection != nil {
			return selection.Obj()
		}
	}
	if info.Uses != nil {
		return info.Uses[selector.Sel]
	}
	return nil
}

func isLoggerConstructor(call *ast.CallExpr, info *types.Info) bool {
	object := callObject(info, call)
	function, ok := object.(*types.Func)
	if !ok || function.Pkg() == nil {
		return false
	}
	path := function.Pkg().Path()
	if path != zapPackagePath && path != slogPackagePath && path != logPackagePath {
		return false
	}
	if isReviewedBridgeConstructor(call, info, path, function.Name()) {
		return false
	}
	switch path {
	case zapPackagePath:
		// NewNop is an explicit no-output fallback. It cannot leak data and is
		// therefore classified as safe construction rather than a sink bypass.
		if function.Name() == "NewNop" {
			return false
		}
		if function.Name() == "Build" {
			return isZapConfigBuild(function)
		}
		switch function.Name() {
		case "New", "NewProduction", "NewDevelopment", "NewExample":
			return isLoggerType(infoTypeOf(info, call), path)
		default:
			return false
		}
	case slogPackagePath, logPackagePath:
		return function.Name() == "New" && isLoggerType(infoTypeOf(info, call), path)
	default:
		return false
	}
}

func isZapConfigBuild(function *types.Func) bool {
	if function == nil {
		return false
	}
	signature, ok := function.Type().(*types.Signature)
	if !ok || signature.Recv() == nil {
		return false
	}
	typ := signature.Recv().Type()
	if pointer, ok := typ.(*types.Pointer); ok {
		typ = pointer.Elem()
	}
	named, ok := typ.(*types.Named)
	return ok && named.Obj() != nil && named.Obj().Pkg() != nil && named.Obj().Pkg().Path() == zapPackagePath && named.Obj().Name() == "Config"
}

func isReviewedBridgeConstructor(call *ast.CallExpr, info *types.Info, packagePath, functionName string) bool {
	if (packagePath != slogPackagePath && packagePath != logPackagePath) || functionName != "New" || len(call.Args) == 0 {
		return false
	}
	bridge, ok := call.Args[0].(*ast.CallExpr)
	if !ok {
		return false
	}
	object := callObject(info, bridge)
	function, ok := object.(*types.Func)
	return ok && function.Pkg() != nil && function.Pkg().Path() == loggingPackagePath &&
		((packagePath == slogPackagePath && function.Name() == "NewSlogHandler") || (packagePath == logPackagePath && function.Name() == "StandardWriter"))
}

func isLoggerType(typ types.Type, packagePath string) bool {
	if typ == nil {
		return false
	}
	if tuple, ok := typ.(*types.Tuple); ok {
		if tuple.Len() == 0 {
			return false
		}
		typ = tuple.At(0).Type()
	}
	if pointer, ok := typ.(*types.Pointer); ok {
		typ = pointer.Elem()
	}
	named, ok := typ.(*types.Named)
	if !ok || named.Obj() == nil || named.Obj().Pkg() == nil || named.Obj().Pkg().Path() != packagePath {
		return false
	}
	switch packagePath {
	case zapPackagePath:
		return named.Obj().Name() == "Logger" || named.Obj().Name() == "SugaredLogger"
	case slogPackagePath, logPackagePath:
		return named.Obj().Name() == "Logger"
	default:
		return false
	}
}

func callObject(info *types.Info, call *ast.CallExpr) types.Object {
	switch function := call.Fun.(type) {
	case *ast.SelectorExpr:
		return selectorObject(info, function)
	case *ast.Ident:
		if info != nil && info.Uses != nil {
			return info.Uses[function]
		}
	}
	return nil
}

func infoTypeOf(info *types.Info, expression ast.Expr) types.Type {
	if info == nil || expression == nil {
		return nil
	}
	return info.TypeOf(expression)
}

func likelyUnresolvedLoggerCall(call *ast.CallExpr, info *types.Info) bool {
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || !loggerMethodName(selector.Sel.Name) {
		return false
	}
	if syntacticAppZapLog(selector) {
		return true
	}
	if info == nil || info.TypeOf(selector.X) == nil {
		return true
	}
	_, isInterface := info.TypeOf(selector.X).Underlying().(*types.Interface)
	if !isInterface {
		return selectorObject(info, selector) == nil
	}
	object := selectorObject(info, selector)
	if object == nil {
		return true
	}
	function, ok := object.(*types.Func)
	if !ok || function.Pkg() == nil {
		return !isErrorStringMethod(function)
	}
	path := function.Pkg().Path()
	return path != zapPackagePath && path != slogPackagePath && path != logPackagePath
}

func isErrorStringMethod(function *types.Func) bool {
	if function == nil || function.Name() != "Error" {
		return false
	}
	signature, ok := function.Type().(*types.Signature)
	if !ok || signature.Results() == nil || signature.Results().Len() != 1 {
		return false
	}
	basic, ok := signature.Results().At(0).Type().Underlying().(*types.Basic)
	return ok && basic.Info()&types.IsString != 0
}

func loggerMethodName(name string) bool {
	return zapLoggerMethods[name] || slogLoggerMethods[name] || standardLoggerMethods[name]
}

func syntacticAppZapLog(selector *ast.SelectorExpr) bool {
	value, ok := selector.X.(*ast.SelectorExpr)
	if !ok || value.Sel.Name != "ZapLog" {
		return false
	}
	_, ok = value.X.(*ast.Ident)
	return ok
}

type eventState uint8

const (
	eventMissing eventState = iota
	eventStatic
	eventDynamic
)

func loggerEventState(parts []policyLoggerFieldPart, info *types.Info, origins map[types.Object][]ast.Expr) eventState {
	state := eventMissing
	for _, part := range parts {
		partState := eventStateForPart(part, info, origins, make(map[types.Object]bool))
		if partState == eventDynamic {
			return eventDynamic
		}
		if partState == eventStatic {
			state = eventStatic
		}
	}
	return state
}

func eventStateForPart(part policyLoggerFieldPart, info *types.Info, origins map[types.Object][]ast.Expr, seen map[types.Object]bool) eventState {
	if len(part.args) == 0 {
		return eventMissing
	}
	if part.mode == policyKeyValues {
		state := eventMissing
		for index := 0; index < len(part.args); {
			argument := part.args[index]
			if isLoggingFieldExpression(argument, info) {
				fieldState, ok := loggingFieldEventState(argument, info, origins, seen)
				if ok {
					if fieldState == eventDynamic {
						return eventDynamic
					}
					if fieldState == eventStatic {
						state = eventStatic
					}
				}
				index++
				continue
			}
			key, ok := stringConstantOK(info, argument)
			if !ok || key != "event" {
				index += 2
				continue
			}
			if index+1 >= len(part.args) {
				return eventDynamic
			}
			value, ok := stringConstantOK(info, part.args[index+1])
			if !ok || value == "" {
				return eventDynamic
			}
			state = eventStatic
			index += 2
		}
		return state
	}
	state := eventMissing
	for _, argument := range part.args {
		fieldState, ok := loggingFieldEventState(argument, info, origins, seen)
		if !ok {
			continue
		}
		if fieldState == eventDynamic {
			return eventDynamic
		}
		if fieldState == eventStatic {
			state = eventStatic
		}
	}
	return state
}

func loggingFieldEventState(expression ast.Expr, info *types.Info, origins map[types.Object][]ast.Expr, seen map[types.Object]bool) (eventState, bool) {
	if expression == nil || info == nil {
		return eventMissing, false
	}
	if ident, ok := expression.(*ast.Ident); ok {
		object := info.ObjectOf(ident)
		values := origins[object]
		if len(values) == 0 || seen[object] {
			return eventMissing, false
		}
		seen[object] = true
		state := eventMissing
		for _, value := range values {
			valueState, ok := loggingFieldEventState(value, info, origins, seen)
			if !ok {
				continue
			}
			if valueState == eventDynamic {
				return eventDynamic, true
			}
			if valueState == eventStatic {
				state = eventStatic
			}
		}
		return state, state != eventMissing
	}
	if ellipsis, ok := expression.(*ast.Ellipsis); ok {
		return loggingFieldEventState(ellipsis.Elt, info, origins, seen)
	}
	if composite, ok := expression.(*ast.CompositeLit); ok {
		state := eventMissing
		for _, element := range composite.Elts {
			if pair, ok := element.(*ast.KeyValueExpr); ok {
				element = pair.Value
			}
			elementState, ok := loggingFieldEventState(element, info, origins, seen)
			if !ok {
				continue
			}
			if elementState == eventDynamic {
				return eventDynamic, true
			}
			if elementState == eventStatic {
				state = eventStatic
			}
		}
		return state, state != eventMissing
	}
	call, ok := expression.(*ast.CallExpr)
	if !ok || len(call.Args) < 2 {
		return eventMissing, false
	}
	if isAppendCall(call, info) {
		state := eventMissing
		for _, argument := range call.Args {
			argumentState, ok := loggingFieldEventState(argument, info, origins, seen)
			if !ok {
				continue
			}
			if argumentState == eventDynamic {
				return eventDynamic, true
			}
			if argumentState == eventStatic {
				state = eventStatic
			}
		}
		return state, state != eventMissing
	}
	object := callObject(info, call)
	function, ok := object.(*types.Func)
	if !ok || function.Pkg() == nil || len(call.Args) < 2 {
		return eventMissing, false
	}
	path := function.Pkg().Path()
	if path != zapPackagePath && path != slogPackagePath {
		return eventMissing, false
	}
	key, ok := stringConstantOK(info, call.Args[0])
	if !ok || key != "event" {
		return eventMissing, false
	}
	if function.Name() != "String" {
		return eventDynamic, true
	}
	value, ok := stringConstantOK(info, call.Args[1])
	if !ok || value == "" {
		return eventDynamic, true
	}
	return eventStatic, true
}

func loggerDerivationMode(call *ast.CallExpr, info *types.Info) (policyFieldMode, bool) {
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return policyNoFields, false
	}
	object := selectorObject(info, selector)
	function, ok := object.(*types.Func)
	if !ok || function.Pkg() == nil {
		return policyNoFields, false
	}
	signature, ok := function.Type().(*types.Signature)
	if !ok || signature.Recv() == nil {
		return policyNoFields, false
	}
	switch function.Pkg().Path() {
	case zapPackagePath:
		if function.Name() != "With" && function.Name() != "WithLazy" || !isZapLoggerMethod(function) {
			return policyNoFields, false
		}
		if zapLoggerReceiver(function) == "Logger" {
			return policyZapFields, true
		}
		if zapLoggerReceiver(function) == "SugaredLogger" {
			return policyKeyValues, true
		}
	case slogPackagePath:
		if function.Name() == "With" && isLoggerType(signature.Recv().Type(), slogPackagePath) {
			return policyKeyValues, true
		}
	}
	return policyNoFields, false
}

func isUnknownLogField(expression ast.Expr, info *types.Info) bool {
	call, ok := expression.(*ast.CallExpr)
	if !ok {
		return false
	}
	object := callObject(info, call)
	function, ok := object.(*types.Func)
	if !ok || function.Pkg() == nil {
		return false
	}
	switch function.Pkg().Path() {
	case zapPackagePath:
		switch function.Name() {
		case "Any", "Object", "Array", "Reflect", "Stringer", "Interface":
			return true
		}
	case slogPackagePath:
		return function.Name() == "Any"
	}
	return false
}

func isLoggingFieldExpression(expression ast.Expr, info *types.Info) bool {
	if expression == nil {
		return false
	}
	if _, ok := expression.(*ast.ParenExpr); ok {
		return isLoggingFieldExpression(expression.(*ast.ParenExpr).X, info)
	}
	call, ok := expression.(*ast.CallExpr)
	if !ok {
		return isLoggingFieldType(infoTypeOf(info, expression))
	}
	return isLoggingFieldType(infoTypeOf(info, call))
}

func isLoggingFieldType(typ types.Type) bool {
	if typ == nil {
		return false
	}
	if pointer, ok := typ.(*types.Pointer); ok {
		typ = pointer.Elem()
	}
	named, ok := typ.(*types.Named)
	if !ok || named.Obj() == nil || named.Obj().Pkg() == nil {
		return false
	}
	path, name := named.Obj().Pkg().Path(), named.Obj().Name()
	return (path == zapPackagePath && name == "Field") || (path == slogPackagePath && name == "Attr")
}

func isLoggingContainerType(typ types.Type, mode policyFieldMode) bool {
	if typ == nil {
		return false
	}
	var element types.Type
	switch value := typ.(type) {
	case *types.Slice:
		element = value.Elem()
	case *types.Array:
		element = value.Elem()
	default:
		return false
	}
	if isLoggingFieldType(element) {
		return true
	}
	if mode == policyKeyValues {
		if _, ok := element.Underlying().(*types.Interface); ok {
			return true
		}
	}
	return false
}

func isSafeLogValue(expression ast.Expr, info *types.Info, origins map[types.Object][]ast.Expr, seen map[types.Object]bool) bool {
	if expression == nil {
		return false
	}
	for {
		paren, ok := expression.(*ast.ParenExpr)
		if !ok {
			break
		}
		expression = paren.X
	}
	if ellipsis, ok := expression.(*ast.Ellipsis); ok {
		return isSafeLogValue(ellipsis.Elt, info, origins, seen)
	}
	if isUnknownLogField(expression, info) || isLoggingFieldExpression(expression, info) {
		return !isUnknownLogField(expression, info)
	}
	if ident, ok := expression.(*ast.Ident); ok {
		if info == nil {
			return false
		}
		object := info.ObjectOf(ident)
		if values := origins[object]; len(values) == 1 {
			if seen[object] {
				return false
			}
			seen[object] = true
			return isSafeLogValue(values[0], info, origins, seen)
		}
	}
	typ := infoTypeOf(info, expression)
	if typ == nil {
		return false
	}
	if isLoggingFieldType(typ) {
		return true
	}
	underlying := typ.Underlying()
	if basic, ok := underlying.(*types.Basic); ok {
		return basic.Info()&(types.IsBoolean|types.IsInteger|types.IsFloat|types.IsComplex|types.IsString) != 0
	}
	return false
}

func isAppendCall(call *ast.CallExpr, info *types.Info) bool {
	if call == nil || info == nil {
		return false
	}
	builtin, ok := callObject(info, call).(*types.Builtin)
	return ok && builtin.Name() == "append"
}

func containsErrorString(expression ast.Expr, info *types.Info) bool {
	found := false
	ast.Inspect(expression, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || selector.Sel.Name != "Error" {
			return true
		}
		if object := selectorObject(info, selector); object != nil {
			if function, ok := object.(*types.Func); ok && function.Pkg() != nil && function.Pkg().Path() == zapPackagePath {
				return true
			}
		}
		if typ := infoTypeOf(info, call); typ != nil {
			if basic, ok := typ.Underlying().(*types.Basic); ok && basic.Info()&types.IsString != 0 {
				found = true
			}
		}
		return !found
	})
	return found
}

func containsErrorStringFlow(expression ast.Expr, info *types.Info, origins map[types.Object][]ast.Expr, seen map[types.Object]bool) bool {
	if expression == nil {
		return false
	}
	if containsErrorString(expression, info) {
		return true
	}
	found := false
	ast.Inspect(expression, func(node ast.Node) bool {
		ident, ok := node.(*ast.Ident)
		if !ok || info == nil {
			return true
		}
		object := info.ObjectOf(ident)
		if object == nil || seen[object] {
			return true
		}
		values := origins[object]
		if len(values) == 0 {
			return true
		}
		seen[object] = true
		for _, value := range values {
			if containsErrorStringFlow(value, info, origins, seen) {
				found = true
				return false
			}
		}
		return true
	})
	return found
}

func isStdoutBypassCall(call *ast.CallExpr, info *types.Info) bool {
	object := callObject(info, call)
	function, ok := object.(*types.Func)
	if !ok || function.Pkg() == nil {
		return false
	}
	path, name := function.Pkg().Path(), function.Name()
	if path == fmtPackagePath {
		switch name {
		case "Print", "Printf", "Println":
			return true
		case "Fprint", "Fprintf", "Fprintln":
			return len(call.Args) > 0 && isStdStream(call.Args[0], info)
		}
	}
	if path == ioPackagePath && name == "WriteString" {
		return len(call.Args) > 0 && isStdStream(call.Args[0], info)
	}
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || (name != "Write" && name != "WriteString") {
		return false
	}
	return isStdStream(selector.X, info)
}

func isStdStream(expression ast.Expr, info *types.Info) bool {
	selector, ok := expression.(*ast.SelectorExpr)
	if !ok || (selector.Sel.Name != "Stdout" && selector.Sel.Name != "Stderr") {
		return false
	}
	object := selectorObject(info, selector)
	variable, ok := object.(*types.Var)
	return ok && variable.Pkg() != nil && variable.Pkg().Path() == osPackagePath && (variable.Name() == "Stdout" || variable.Name() == "Stderr")
}

func stringConstantOK(info *types.Info, expression ast.Expr) (string, bool) {
	for {
		paren, ok := expression.(*ast.ParenExpr)
		if !ok {
			break
		}
		expression = paren.X
	}
	if info != nil {
		if typed, ok := info.Types[expression]; ok && typed.Value != nil {
			if basic, ok := typed.Type.Underlying().(*types.Basic); ok && basic.Info()&types.IsString != 0 && typed.Value.Kind() == constant.String {
				return constant.StringVal(typed.Value), true
			}
		}
	}
	if literal, ok := expression.(*ast.BasicLit); ok && literal.Kind == token.STRING {
		value, err := strconv.Unquote(literal.Value)
		return value, err == nil
	}
	return "", false
}

func isStringConstant(info *types.Info, expression ast.Expr) bool {
	_, ok := stringConstantOK(info, expression)
	return ok
}

func (a policyAnalyzer) matchLegacy(file, function, receiver, method, message string) bool {
	for index := range a.exceptions {
		exception := a.exceptions[index]
		if exception.PackagePath != a.pkg.PkgPath {
			continue
		}
		if exception.File != file || exception.Function != function || exception.Receiver != receiver || exception.Method != method || exception.Message != message {
			continue
		}
		if a.usage[index] >= maxExceptionCount(exception.Count) {
			continue
		}
		a.usage[index]++
		return true
	}
	return false
}

func maxExceptionCount(count int) int {
	if count <= 0 {
		return 1
	}
	return count
}

func selectorMethod(call *ast.CallExpr) string {
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return ""
	}
	return selector.Sel.Name
}

func receiverName(function *ast.FuncDecl) string {
	if function == nil || function.Recv == nil || len(function.Recv.List) == 0 {
		return ""
	}
	return typeName(function.Recv.List[0].Type)
}

func buildIgnoredFiles(root string) []string {
	result := make([]string, 0)
	_ = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if path != root && (entry.Name() == ".git" || entry.Name() == "vendor" || entry.Name() == "third_party" || entry.Name() == "node_modules") {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".go" {
			return nil
		}
		parsed, parseErr := parser.ParseFile(token.NewFileSet(), path, nil, parser.ParseComments)
		if parseErr != nil {
			return nil
		}
		if hasIgnoreBuildConstraint(parsed) {
			rel, relErr := filepath.Rel(root, path)
			if relErr == nil {
				result = append(result, filepath.ToSlash(rel))
			}
		}
		return nil
	})
	sort.Strings(result)
	return result
}

func hasIgnoreBuildConstraint(file *ast.File) bool {
	for _, group := range file.Comments {
		for _, comment := range group.List {
			text := strings.TrimSpace(comment.Text)
			if strings.HasPrefix(text, "//go:build") && strings.Contains(strings.TrimSpace(strings.TrimPrefix(text, "//go:build")), "ignore") {
				return true
			}
			if strings.HasPrefix(text, "// +build") && strings.Contains(strings.TrimSpace(strings.TrimPrefix(text, "// +build")), "ignore") {
				return true
			}
		}
	}
	return false
}

func reviewedBuildIgnoredFile(file string) (string, bool) {
	switch filepath.ToSlash(file) {
	case "resource/database/gb28181/migrations/run-merge-sip-log.go":
		return "manual migration CLI; exact file exception, operator invoked", true
	case "resource/database/gb28181/migrations/remove_sip_log_new_menu.go":
		return "manual migration CLI; exact file exception, operator invoked", true
	default:
		return "build-ignored source requires an exact review record", false
	}
}

func sortPolicyFindings(report *PolicyReport) {
	sort.Slice(report.Findings, func(i, j int) bool {
		a, b := report.Findings[i], report.Findings[j]
		if a.File != b.File {
			return a.File < b.File
		}
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		if a.Column != b.Column {
			return a.Column < b.Column
		}
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		return a.Description < b.Description
	})
}

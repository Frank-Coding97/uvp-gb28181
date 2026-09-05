package loggingcontract

import (
	"go/ast"
	"go/token"
	"strings"
)

var zapLogMethods = map[string]bool{
	"Debug": true, "Info": true, "Warn": true, "Error": true,
	"DPanic": true, "Panic": true, "Fatal": true,
	"Debugf": true, "Infof": true, "Warnf": true, "Errorf": true,
	"DPanicf": true, "Panicf": true, "Fatalf": true,
	"Debugw": true, "Infow": true, "Warnw": true, "Errorw": true,
	"DPanicw": true, "Panicw": true, "Fatalw": true,
}

var standardLogMethods = map[string]bool{
	"Print": true, "Printf": true, "Println": true,
	"Fatal": true, "Fatalf": true, "Fatalln": true,
	"Panic": true, "Panicf": true, "Panicln": true,
	"Output": true,
}

var slogMethods = map[string]bool{
	"Debug": true, "Info": true, "Warn": true, "Error": true,
	"Log": true, "DebugContext": true, "InfoContext": true,
	"WarnContext": true, "ErrorContext": true, "LogAttrs": true,
}

var dbMethods = map[string]bool{
	"Association": true, "Count": true, "Create": true, "Delete": true,
	"Exec": true, "Find": true, "First": true, "Joins": true,
	"Last": true, "Model": true, "Preload": true, "Raw": true,
	"Save": true, "Scan": true, "Scopes": true, "Session": true,
	"Take": true, "Transaction": true, "Update": true, "Updates": true,
	"Where": true, "WithContext": true, "Debug": true,
}

func scanFile(state *scanState, report *Report, file *sourceFile) {
	scanRoutes(state.fset, report, file)
	for _, fn := range file.functions {
		locals := localLoggerNames(fn)
		if fn.decl.Body == nil {
			continue
		}
		ast.Inspect(fn.decl.Body, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			position := state.fset.Position(call.Pos())
			selector, selectorOK := call.Fun.(*ast.SelectorExpr)

			if selectorOK && isZapConstructorCall(call, file.imports) {
				addSite(report, Site{
					File:         file.rel,
					Package:      file.file.Name.Name,
					Function:     fn.name,
					Receiver:     fn.receiver,
					Line:         position.Line,
					Column:       position.Column,
					Kind:         KindZapConstructor,
					Operation:    selector.Sel.Name,
					LoggerSource: constructorSource(fn, file, call),
					MessageClass: MessageNone,
					functionID:   fn.id,
				})
			}

			if selectorOK && isZapFieldCall(selector, file.imports) {
				addSite(report, Site{
					File:         file.rel,
					Package:      file.file.Name.Name,
					Function:     fn.name,
					Receiver:     fn.receiver,
					Line:         position.Line,
					Column:       position.Column,
					Kind:         KindZapField,
					Operation:    selector.Sel.Name,
					MessageClass: MessageNone,
					LoggerSource: LoggerBridge,
					functionID:   fn.id,
				})
				if selector.Sel.Name == "Any" {
					addIssue(report, Issue{
						Kind:        IssueBareAny,
						File:        file.rel,
						Function:    fn.name,
						Line:        position.Line,
						Column:      position.Column,
						Description: "zap.Any requires an explicit field review before migration",
					})
				}
			}

			if selectorOK {
				if source, isLogger := zapLoggerSource(selector.X, file, locals); isLogger && zapLogMethods[selector.Sel.Name] {
					messageIndex := 0
					messageClass := messageClassAt(call.Args, messageIndex, file.constants)
					addSite(report, Site{
						File:         file.rel,
						Package:      file.file.Name.Name,
						Function:     fn.name,
						Receiver:     fn.receiver,
						Line:         position.Line,
						Column:       position.Column,
						Kind:         KindZapLog,
						Operation:    selector.Sel.Name,
						LoggerSource: source,
						MessageClass: messageClass,
						functionID:   fn.id,
					})
					addDynamicMessageIssue(report, file, fn, position, messageClass)
				}
				if source, isLogger := standardLoggerSource(selector.X, file); isLogger && standardLogMethods[selector.Sel.Name] {
					messageClass := messageClassAt(call.Args, 0, file.constants)
					addSite(report, Site{
						File:         file.rel,
						Package:      file.file.Name.Name,
						Function:     fn.name,
						Receiver:     fn.receiver,
						Line:         position.Line,
						Column:       position.Column,
						Kind:         KindStandardLog,
						Operation:    selector.Sel.Name,
						LoggerSource: source,
						MessageClass: messageClass,
						functionID:   fn.id,
					})
					addDynamicMessageIssue(report, file, fn, position, messageClass)
				}
				if source, isLogger := slogLoggerSource(selector.X, file); isLogger && slogMethods[selector.Sel.Name] {
					messageIndex := 0
					if strings.HasSuffix(selector.Sel.Name, "Context") {
						messageIndex = 1
					}
					messageClass := messageClassAt(call.Args, messageIndex, file.constants)
					addSite(report, Site{
						File:         file.rel,
						Package:      file.file.Name.Name,
						Function:     fn.name,
						Receiver:     fn.receiver,
						Line:         position.Line,
						Column:       position.Column,
						Kind:         KindSlog,
						Operation:    selector.Sel.Name,
						LoggerSource: source,
						MessageClass: messageClass,
						functionID:   fn.id,
					})
					addDynamicMessageIssue(report, file, fn, position, messageClass)
				}
				if isFmtCall(selector, file.imports) {
					kind, messageIndex := fmtKindAndMessageIndex(call, selector, file.imports)
					if kind != "" {
						messageClass := messageClassAt(call.Args, messageIndex, file.constants)
						addSite(report, Site{
							File:         file.rel,
							Package:      file.file.Name.Name,
							Function:     fn.name,
							Receiver:     fn.receiver,
							Line:         position.Line,
							Column:       position.Column,
							Kind:         SiteKind(kind),
							Operation:    selector.Sel.Name,
							LoggerSource: LoggerBridge,
							MessageClass: messageClass,
							functionID:   fn.id,
						})
						if kind == string(KindStdoutBypass) {
							addIssue(report, Issue{
								Kind:        IssueStdoutBypass,
								File:        file.rel,
								Function:    fn.name,
								Line:        position.Line,
								Column:      position.Column,
								Description: "direct fmt output bypasses the unified logger",
							})
						}
						addDynamicMessageIssue(report, file, fn, position, messageClass)
					}
				}
			}

			if isDirectStdoutWrite(call, file.imports) {
				addSite(report, Site{
					File:         file.rel,
					Package:      file.file.Name.Name,
					Function:     fn.name,
					Receiver:     fn.receiver,
					Line:         position.Line,
					Column:       position.Column,
					Kind:         KindStdoutBypass,
					Operation:    "Write",
					LoggerSource: LoggerBridge,
					MessageClass: MessageDynamic,
					functionID:   fn.id,
				})
				addIssue(report, Issue{
					Kind:        IssueStdoutBypass,
					File:        file.rel,
					Function:    fn.name,
					Line:        position.Line,
					Column:      position.Column,
					Description: "direct stdout write bypasses the unified logger",
				})
			}

			if isDynamicErrorConstructor(call, file.imports, file.constants) {
				addIssue(report, Issue{
					Kind:        IssueDynamicError,
					File:        file.rel,
					Function:    fn.name,
					Line:        position.Line,
					Column:      position.Column,
					Description: "error text depends on a non-static expression",
				})
			}

			if isDBBoundary(call, file) {
				operation := selectorName(call.Fun)
				if operation == "" {
					operation = "DB"
				}
				report.DBBoundaries = append(report.DBBoundaries, DBBoundary{
					File:           file.rel,
					Package:        file.file.Name.Name,
					Function:       fn.name,
					Line:           position.Line,
					Column:         position.Column,
					Operation:      operation,
					ContextSource:  ContextBackground,
					ResolutionNote: "DB boundary inferred from app.DB or conservative ORM receiver naming",
				})
			}

			if callee := selectorName(call.Fun); callee != "" || identName(call.Fun) != "" {
				callee = firstNonEmpty(callee, identName(call.Fun))
				state.edges = append(state.edges, graphEdge{from: fn.id, to: callee})
			}
			return true
		})
	}
}

func addSite(report *Report, site Site) { report.Sites = append(report.Sites, site) }

func addIssue(report *Report, issue Issue) { report.Issues = append(report.Issues, issue) }

func addDynamicMessageIssue(report *Report, file *sourceFile, fn *functionDecl, position token.Position, class MessageClass) {
	if class != MessageDynamic {
		return
	}
	addIssue(report, Issue{
		Kind:        IssueDynamicMessage,
		File:        file.rel,
		Function:    fn.name,
		Line:        position.Line,
		Column:      position.Column,
		Description: "log message is not a compile-time string constant",
	})
}

func localLoggerNames(fn *functionDecl) map[string]LoggerSource {
	result := make(map[string]LoggerSource)
	if fn.decl.Type != nil && fn.decl.Type.Params != nil {
		for _, field := range fn.decl.Type.Params.List {
			if !isZapLoggerType(field.Type, fn.file.imports) {
				continue
			}
			for _, name := range field.Names {
				result[name.Name] = LoggerInjected
			}
		}
	}
	if fn.decl.Body == nil {
		return result
	}
	ast.Inspect(fn.decl.Body, func(node ast.Node) bool {
		switch statement := node.(type) {
		case *ast.AssignStmt:
			for index, expression := range statement.Rhs {
				if !isZapConstructorExpr(expression, fn.file.imports) || index >= len(statement.Lhs) {
					continue
				}
				if name, ok := statement.Lhs[index].(*ast.Ident); ok {
					result[name.Name] = LoggerLocal
				}
			}
		case *ast.ValueSpec:
			for index, expression := range statement.Values {
				if !isZapConstructorExpr(expression, fn.file.imports) || index >= len(statement.Names) {
					continue
				}
				result[statement.Names[index].Name] = LoggerLocal
			}
		}
		return true
	})
	return result
}

func constructorSource(fn *functionDecl, file *sourceFile, call *ast.CallExpr) LoggerSource {
	if fn.receiver != "" {
		return LoggerLocal
	}
	for name := range file.globalLoggers {
		if name == "_" {
			return LoggerGlobal
		}
	}
	if fn.name == "init" {
		return LoggerGlobal
	}
	return LoggerLocal
}

func zapLoggerSource(expression ast.Expr, file *sourceFile, locals map[string]LoggerSource) (LoggerSource, bool) {
	switch value := expression.(type) {
	case *ast.Ident:
		if source, ok := locals[value.Name]; ok {
			return source, true
		}
		if file.globalLoggers[value.Name] {
			return LoggerGlobal, true
		}
	case *ast.SelectorExpr:
		if value.Sel.Name == "ZapLog" && isGlobalAppSelector(value.X, file.imports) {
			return LoggerGlobal, true
		}
		if file.loggerFields[value.Sel.Name] {
			return LoggerInjected, true
		}
		if source, ok := zapLoggerSource(value.X, file, locals); ok {
			if value.Sel.Name == "Sugar" || value.Sel.Name == "Named" || value.Sel.Name == "With" || value.Sel.Name == "WithOptions" {
				return source, true
			}
		}
	case *ast.CallExpr:
		if isZapConstructorExpr(value, file.imports) {
			return LoggerLocal, true
		}
		if selector, ok := value.Fun.(*ast.SelectorExpr); ok {
			if source, ok := zapLoggerSource(selector.X, file, locals); ok && (selector.Sel.Name == "Sugar" || selector.Sel.Name == "Named" || selector.Sel.Name == "With" || selector.Sel.Name == "WithOptions") {
				return source, true
			}
		}
	}
	return LoggerUnknown, false
}

func standardLoggerSource(expression ast.Expr, file *sourceFile) (LoggerSource, bool) {
	selector, ok := expression.(*ast.Ident)
	if ok && file.imports[selector.Name] == "log" {
		return LoggerBridge, true
	}
	if call, ok := expression.(*ast.CallExpr); ok {
		if nested, ok := call.Fun.(*ast.SelectorExpr); ok {
			identifier, ok := nested.X.(*ast.Ident)
			if ok && file.imports[identifier.Name] == "log" && (nested.Sel.Name == "Default" || nested.Sel.Name == "New") {
				return LoggerBridge, true
			}
		}
	}
	return LoggerUnknown, false
}

func slogLoggerSource(expression ast.Expr, file *sourceFile) (LoggerSource, bool) {
	if identifier, ok := expression.(*ast.Ident); ok && file.imports[identifier.Name] == "log/slog" {
		return LoggerBridge, true
	}
	if call, ok := expression.(*ast.CallExpr); ok {
		if nested, ok := call.Fun.(*ast.SelectorExpr); ok {
			identifier, ok := nested.X.(*ast.Ident)
			if ok && file.imports[identifier.Name] == "log/slog" && (nested.Sel.Name == "Default" || nested.Sel.Name == "New") {
				return LoggerBridge, true
			}
		}
	}
	return LoggerUnknown, false
}

func isGlobalAppSelector(expression ast.Expr, imports map[string]string) bool {
	identifier, ok := expression.(*ast.Ident)
	if !ok {
		return false
	}
	path := imports[identifier.Name]
	return strings.HasSuffix(path, "/app/global/app") || path == "uvplatform.cn/uvp-gb28181/app/global/app"
}

func isZapConstructorCall(call *ast.CallExpr, imports map[string]string) bool {
	return isZapConstructorExpr(call, imports)
}

func isZapFieldCall(selector *ast.SelectorExpr, imports map[string]string) bool {
	identifier, ok := selector.X.(*ast.Ident)
	if !ok || imports[identifier.Name] != "go.uber.org/zap" {
		return false
	}
	return selector.Sel.Name == "Any" || selector.Sel.Name == "Error" || selector.Sel.Name == "NamedError"
}

func messageClassAt(args []ast.Expr, index int, constants map[string]bool) MessageClass {
	if index < 0 || index >= len(args) {
		return MessageNone
	}
	if isStaticString(args[index], constants) {
		return MessageStatic
	}
	return MessageDynamic
}

func isFmtCall(selector *ast.SelectorExpr, imports map[string]string) bool {
	identifier, ok := selector.X.(*ast.Ident)
	if !ok || imports[identifier.Name] != "fmt" {
		return false
	}
	switch selector.Sel.Name {
	case "Print", "Printf", "Println", "Fprint", "Fprintf", "Fprintln":
		return true
	default:
		return false
	}
}

func fmtKindAndMessageIndex(call *ast.CallExpr, selector *ast.SelectorExpr, imports map[string]string) (string, int) {
	if strings.HasPrefix(selector.Sel.Name, "Fprint") {
		if len(call.Args) > 0 && isStdoutExpr(call.Args[0], imports) {
			return string(KindStdoutBypass), 1
		}
		return string(KindFmtOutput), 1
	}
	return string(KindStdoutBypass), 0
}

func isStdoutExpr(expression ast.Expr, imports map[string]string) bool {
	selector, ok := expression.(*ast.SelectorExpr)
	if !ok || selector.Sel.Name != "Stdout" {
		return false
	}
	identifier, ok := selector.X.(*ast.Ident)
	return ok && imports[identifier.Name] == "os"
}

func isDirectStdoutWrite(call *ast.CallExpr, imports map[string]string) bool {
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || selector.Sel.Name != "Write" || !isStdoutExpr(selector.X, imports) {
		return false
	}
	return true
}

func isDynamicErrorConstructor(call *ast.CallExpr, imports map[string]string, constants map[string]bool) bool {
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || len(call.Args) == 0 {
		return false
	}
	identifier, ok := selector.X.(*ast.Ident)
	if !ok {
		return false
	}
	if imports[identifier.Name] == "errors" && selector.Sel.Name == "New" {
		return !isStaticString(call.Args[0], constants)
	}
	if imports[identifier.Name] == "fmt" && selector.Sel.Name == "Errorf" {
		return len(call.Args) > 1 || !isStaticString(call.Args[0], constants)
	}
	return false
}

func isDBBoundary(call *ast.CallExpr, file *sourceFile) bool {
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	if selector.Sel.Name == "DB" && isGlobalAppSelector(selector.X, file.imports) {
		return true
	}
	if !dbMethods[selector.Sel.Name] {
		return false
	}
	return looksLikeDBReceiver(selector.X)
}

func looksLikeDBReceiver(expression ast.Expr) bool {
	parts := selectorParts(expression)
	for _, part := range parts {
		switch strings.ToLower(part) {
		case "db", "tx", "gorm", "query", "repo", "repository", "database", "datastore", "store", "session", "model", "scope":
			return true
		}
	}
	return false
}

func selectorParts(expression ast.Expr) []string {
	switch value := expression.(type) {
	case *ast.Ident:
		return []string{value.Name}
	case *ast.SelectorExpr:
		return append(selectorParts(value.X), value.Sel.Name)
	case *ast.CallExpr:
		return selectorParts(value.Fun)
	default:
		return nil
	}
}

func selectorName(expression ast.Expr) string {
	selector, ok := expression.(*ast.SelectorExpr)
	if !ok {
		return ""
	}
	return selector.Sel.Name
}

func identName(expression ast.Expr) string {
	identifier, ok := expression.(*ast.Ident)
	if !ok {
		return ""
	}
	return identifier.Name
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

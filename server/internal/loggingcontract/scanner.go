package loggingcontract

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type sourceFile struct {
	path           string
	rel            string
	file           *ast.File
	imports        map[string]string
	constants      map[string]bool
	constantValues map[string]string
	loggerFields   map[string]bool
	globalLoggers  map[string]bool
	functions      []*functionDecl
}

type functionDecl struct {
	file     *sourceFile
	decl     *ast.FuncDecl
	id       string
	name     string
	receiver string
}

type graphEdge struct {
	from string
	to   string
}

type scanState struct {
	root      string
	fset      *token.FileSet
	files     []*sourceFile
	functions []*functionDecl
	byName    map[string][]*functionDecl
	edges     []graphEdge
}

// Scan parses production Go source under root and returns a migration
// inventory. It does not type-check, compile, execute init functions, inspect
// configuration, or connect to application services.
func Scan(root string, opts Options) (Report, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return Report{}, fmt.Errorf("logging inventory root: %w", err)
	}
	stat, err := os.Stat(absRoot)
	if err != nil {
		return Report{}, fmt.Errorf("logging inventory root %q: %w", root, err)
	}
	if !stat.IsDir() {
		return Report{}, fmt.Errorf("logging inventory root %q is not a directory", root)
	}

	excludedDirs := map[string]bool{
		".git":         true,
		"docs":         true,
		"node_modules": true,
		"testdata":     true,
		"third_party":  true,
		"vendor":       true,
	}
	for _, name := range opts.ExcludeDirs {
		if name != "" {
			excludedDirs[name] = true
		}
	}

	state := &scanState{
		root:   absRoot,
		fset:   token.NewFileSet(),
		byName: make(map[string][]*functionDecl),
	}
	report := Report{
		Root:         absRoot,
		Sites:        make([]Site, 0),
		Routes:       make([]Route, 0),
		CallEdges:    make([]CallEdge, 0),
		DBBoundaries: make([]DBBoundary, 0),
		Issues:       make([]Issue, 0),
		Excluded:     make([]ExcludedFile, 0),
		Unassigned:   make([]Site, 0),
	}
	walkErr := filepath.WalkDir(absRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if path != absRoot && excludedDirs[entry.Name()] {
				report.Excluded = append(report.Excluded, ExcludedFile{
					Path:   state.relative(path),
					Reason: "directory excluded from production inventory",
				})
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".go" {
			return nil
		}
		if strings.HasSuffix(entry.Name(), "_test.go") && !opts.IncludeTests {
			report.Excluded = append(report.Excluded, ExcludedFile{
				Path:   state.relative(path),
				Reason: "test source excluded by default; set IncludeTests to inspect it",
			})
			return nil
		}
		parsed, parseErr := parser.ParseFile(state.fset, path, nil, parser.ParseComments)
		if parseErr != nil {
			return fmt.Errorf("parse %s: %w", state.relative(path), parseErr)
		}
		file := parseSourceFile(state, path, parsed)
		state.files = append(state.files, file)
		return nil
	})
	if walkErr != nil {
		return report, walkErr
	}

	for _, file := range state.files {
		for _, fn := range file.functions {
			state.functions = append(state.functions, fn)
			state.byName[fn.name] = append(state.byName[fn.name], fn)
		}
	}

	for _, file := range state.files {
		scanFile(state, &report, file)
	}
	resolveCallGraph(state, &report)
	assignReachabilityAndOwnership(state, &report)
	sortReport(&report)
	return report, nil
}

// ScanDir is a short form used by small command-line integrations.
func ScanDir(root string) (Report, error) { return Scan(root, Options{}) }

func (s *scanState) relative(path string) string {
	rel, err := filepath.Rel(s.root, path)
	if err != nil {
		return filepath.ToSlash(path)
	}
	return filepath.ToSlash(rel)
}

func parseSourceFile(state *scanState, path string, file *ast.File) *sourceFile {
	parsed := &sourceFile{
		path:           path,
		rel:            state.relative(path),
		file:           file,
		imports:        make(map[string]string),
		constants:      make(map[string]bool),
		constantValues: make(map[string]string),
		loggerFields:   make(map[string]bool),
		globalLoggers:  make(map[string]bool),
	}
	for _, spec := range file.Imports {
		pathValue, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			continue
		}
		name := filepath.Base(pathValue)
		if spec.Name != nil {
			name = spec.Name.Name
		}
		if name != "_" && name != "." {
			parsed.imports[name] = pathValue
		}
	}
	for _, decl := range file.Decls {
		switch declaration := decl.(type) {
		case *ast.GenDecl:
			collectPackageDeclarations(parsed, declaration)
		case *ast.FuncDecl:
			fn := newFunctionDecl(parsed, declaration)
			parsed.functions = append(parsed.functions, fn)
		}
	}
	return parsed
}

func collectPackageDeclarations(file *sourceFile, declaration *ast.GenDecl) {
	switch declaration.Tok {
	case token.CONST:
		for _, spec := range declaration.Specs {
			values, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for index, name := range values.Names {
				var value ast.Expr
				if index < len(values.Values) {
					value = values.Values[index]
				}
				constantValue, isConstant := staticStringValue(value, file.constantValues)
				file.constants[name.Name] = isConstant
				if isConstant {
					file.constantValues[name.Name] = constantValue
				} else {
					delete(file.constantValues, name.Name)
				}
			}
		}
	case token.VAR:
		for _, spec := range declaration.Specs {
			values, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			if isZapLoggerType(values.Type, file.imports) {
				for _, name := range values.Names {
					file.globalLoggers[name.Name] = true
				}
			}
			for index, name := range values.Names {
				if index < len(values.Values) && isZapConstructorExpr(values.Values[index], file.imports) {
					file.globalLoggers[name.Name] = true
				}
			}
		}
	case token.TYPE:
		for _, spec := range declaration.Specs {
			typeSpec, ok := spec.(*ast.TypeSpec)
			if !ok {
				continue
			}
			structType, ok := typeSpec.Type.(*ast.StructType)
			if !ok || structType.Fields == nil {
				continue
			}
			for _, field := range structType.Fields.List {
				if !isZapLoggerType(field.Type, file.imports) {
					continue
				}
				for _, name := range field.Names {
					file.loggerFields[name.Name] = true
				}
			}
		}
	}
}

func newFunctionDecl(file *sourceFile, declaration *ast.FuncDecl) *functionDecl {
	receiver := ""
	if declaration.Recv != nil && len(declaration.Recv.List) > 0 {
		receiver = typeName(declaration.Recv.List[0].Type)
	}
	return &functionDecl{
		file:     file,
		decl:     declaration,
		id:       file.rel + "#" + receiver + "." + declaration.Name.Name,
		name:     declaration.Name.Name,
		receiver: receiver,
	}
}

func typeName(expression ast.Expr) string {
	switch value := expression.(type) {
	case *ast.Ident:
		return value.Name
	case *ast.StarExpr:
		return "*" + typeName(value.X)
	case *ast.SelectorExpr:
		return typeName(value.X) + "." + value.Sel.Name
	case *ast.IndexExpr:
		return typeName(value.X)
	case *ast.IndexListExpr:
		return typeName(value.X)
	case *ast.ArrayType:
		return "[]" + typeName(value.Elt)
	case *ast.InterfaceType:
		return "interface{}"
	default:
		return "<type>"
	}
}

func isZapLoggerType(expression ast.Expr, imports map[string]string) bool {
	star, ok := expression.(*ast.StarExpr)
	if !ok {
		return false
	}
	selector, ok := star.X.(*ast.SelectorExpr)
	if !ok || selector.Sel.Name != "Logger" {
		return false
	}
	identifier, ok := selector.X.(*ast.Ident)
	return ok && imports[identifier.Name] == "go.uber.org/zap"
}

func isStaticString(expression ast.Expr, constants map[string]bool) bool {
	switch value := expression.(type) {
	case nil:
		return false
	case *ast.BasicLit:
		return value.Kind == token.STRING
	case *ast.Ident:
		return constants[value.Name]
	case *ast.ParenExpr:
		return isStaticString(value.X, constants)
	case *ast.BinaryExpr:
		return value.Op == token.ADD && isStaticString(value.X, constants) && isStaticString(value.Y, constants)
	default:
		return false
	}
}

func staticStringValue(expression ast.Expr, constants map[string]string) (string, bool) {
	switch value := expression.(type) {
	case nil:
		return "", false
	case *ast.BasicLit:
		if value.Kind != token.STRING {
			return "", false
		}
		text, err := strconv.Unquote(value.Value)
		if err != nil {
			return "", false
		}
		return text, true
	case *ast.Ident:
		text, ok := constants[value.Name]
		return text, ok
	case *ast.ParenExpr:
		return staticStringValue(value.X, constants)
	case *ast.BinaryExpr:
		if value.Op != token.ADD {
			return "", false
		}
		left, leftOK := staticStringValue(value.X, constants)
		right, rightOK := staticStringValue(value.Y, constants)
		return left + right, leftOK && rightOK
	default:
		return "", false
	}
}

func isZapConstructorExpr(expression ast.Expr, imports map[string]string) bool {
	call, ok := expression.(*ast.CallExpr)
	if !ok {
		return false
	}
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	identifier, ok := selector.X.(*ast.Ident)
	if ok && imports[identifier.Name] == "go.uber.org/zap" {
		return strings.HasPrefix(selector.Sel.Name, "New") || selector.Sel.Name == "Build"
	}
	return selector.Sel.Name == "Build" && isZapConfigExpr(selector.X, imports)
}

func isZapConfigExpr(expression ast.Expr, imports map[string]string) bool {
	composite, ok := expression.(*ast.CompositeLit)
	if !ok {
		return false
	}
	selector, ok := composite.Type.(*ast.SelectorExpr)
	if !ok || selector.Sel.Name != "Config" {
		return false
	}
	identifier, ok := selector.X.(*ast.Ident)
	return ok && imports[identifier.Name] == "go.uber.org/zap"
}

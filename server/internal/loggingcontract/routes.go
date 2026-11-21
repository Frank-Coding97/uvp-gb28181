package loggingcontract

import (
	"go/ast"
	"go/token"
	"strconv"
	"strings"
)

var routeMethods = map[string]bool{
	"GET": true, "POST": true, "PUT": true, "PATCH": true,
	"DELETE": true, "HEAD": true, "OPTIONS": true, "Any": true,
	"Match": true, "Handle": true,
}

func scanRoutes(fset *token.FileSet, report *Report, file *sourceFile) {
	if file.file == nil {
		return
	}
	prefixes := collectRoutePrefixes(file)
	for _, fn := range file.functions {
		if fn.decl.Body == nil {
			continue
		}
		ast.Inspect(fn.decl.Body, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || !routeMethods[selector.Sel.Name] || len(call.Args) == 0 {
				return true
			}
			position := fset.Position(call.Pos())
			path, literal := routePath(call.Args[0], selector.X, prefixes, file.constants, file.constantValues)
			handlers := make([]string, 0)
			for _, argument := range call.Args[1:] {
				if name := handlerName(argument); name != "" {
					handlers = append(handlers, name)
				}
			}
			note := "handler resolution is bounded to function names; interfaces, generated and dynamic registrations remain unresolved"
			if !literal {
				note += "; route path is not a compile-time string"
			}
			if len(handlers) == 0 {
				note += "; no static handler expression"
			}
			report.Routes = append(report.Routes, Route{
				File:           file.rel,
				Package:        file.file.Name.Name,
				Function:       fn.name,
				Line:           position.Line,
				Column:         position.Column,
				Method:         selector.Sel.Name,
				Path:           path,
				PathLiteral:    literal,
				Handlers:       handlers,
				OwnerTask:      routeOwnerTask(file.rel),
				OwnerReason:    routeOwnerReason(file.rel),
				ResolutionNote: note,
			})
			return true
		})
	}
}

// collectRoutePrefixes resolves only literal Group assignments. It is kept
// separate so an unresolved dynamic group remains visible in Route.Path and
// cannot be mistaken for a complete route map.
func collectRoutePrefixes(file *sourceFile) map[string]string {
	prefixes := make(map[string]string)
	for pass := 0; pass < 3; pass++ {
		for _, fn := range file.functions {
			if fn.decl.Body == nil {
				continue
			}
			ast.Inspect(fn.decl.Body, func(node ast.Node) bool {
				assign, ok := node.(*ast.AssignStmt)
				if !ok {
					return true
				}
				for index, lhs := range assign.Lhs {
					name, ok := lhs.(*ast.Ident)
					if !ok || index >= len(assign.Rhs) {
						continue
					}
					call, ok := assign.Rhs[index].(*ast.CallExpr)
					if !ok || len(call.Args) == 0 {
						continue
					}
					selector, ok := call.Fun.(*ast.SelectorExpr)
					if !ok || selector.Sel.Name != "Group" {
						continue
					}
					group, literal := staticPath(call.Args[0], file.constants, file.constantValues)
					if !literal {
						continue
					}
					prefixes[name.Name] = joinRoutePath(routeReceiverPrefix(selector.X, prefixes, file.constants, file.constantValues), group)
				}
				return true
			})
		}
	}
	return prefixes
}

func routePath(pathExpr ast.Expr, receiver ast.Expr, prefixes map[string]string, constants map[string]bool, values map[string]string) (string, bool) {
	path, literal := staticPath(pathExpr, constants, values)
	if !literal {
		return "", false
	}
	return joinRoutePath(routeReceiverPrefix(receiver, prefixes, constants, values), path), true
}

func routeReceiverPrefix(receiver ast.Expr, prefixes map[string]string, constants map[string]bool, values map[string]string) string {
	switch value := receiver.(type) {
	case *ast.Ident:
		return prefixes[value.Name]
	case *ast.CallExpr:
		selector, ok := value.Fun.(*ast.SelectorExpr)
		if !ok || selector.Sel.Name != "Group" || len(value.Args) == 0 {
			return ""
		}
		group, literal := staticPath(value.Args[0], constants, values)
		if !literal {
			return ""
		}
		return joinRoutePath(routeReceiverPrefix(selector.X, prefixes, constants, values), group)
	default:
		return ""
	}
}

func staticPath(expression ast.Expr, constants map[string]bool, values map[string]string) (string, bool) {
	literal, ok := expression.(*ast.BasicLit)
	if ok && literal.Kind.String() == "STRING" {
		value, err := unquote(literal.Value)
		return value, err == nil
	}
	if identifier, ok := expression.(*ast.Ident); ok && constants != nil && constants[identifier.Name] {
		if values != nil {
			return values[identifier.Name], true
		}
		return identifier.Name, true
	}
	if binary, ok := expression.(*ast.BinaryExpr); ok && binary.Op.String() == "+" {
		left, leftOK := staticPath(binary.X, constants, values)
		right, rightOK := staticPath(binary.Y, constants, values)
		return left + right, leftOK && rightOK
	}
	if paren, ok := expression.(*ast.ParenExpr); ok {
		return staticPath(paren.X, constants, values)
	}
	return "", false
}

func unquote(value string) (string, error) {
	if len(value) >= 2 && value[0] == '`' {
		return strings.TrimSuffix(strings.TrimPrefix(value, "`"), "`"), nil
	}
	if len(value) >= 2 && value[0] == '"' {
		return strconv.Unquote(value)
	}
	return "", strconv.ErrSyntax
}

func joinRoutePath(prefix, path string) string {
	if prefix == "" {
		if path == "" {
			return "/"
		}
		if strings.HasPrefix(path, "/") {
			return path
		}
		return "/" + path
	}
	if path == "" {
		return prefix
	}
	return strings.TrimRight(prefix, "/") + "/" + strings.TrimLeft(path, "/")
}

func handlerName(expression ast.Expr) string {
	switch value := expression.(type) {
	case *ast.Ident:
		return value.Name
	case *ast.SelectorExpr:
		return selectorExpressionName(value)
	case *ast.FuncLit:
		return "<anonymous>"
	case *ast.ParenExpr:
		return handlerName(value.X)
	default:
		return ""
	}
}

func selectorExpressionName(selector *ast.SelectorExpr) string {
	parts := selectorParts(selector)
	return strings.Join(parts, ".")
}

func routeOwnerTask(file string) string {
	file = filepathToSlash(file)
	if strings.HasPrefix(file, "app/gb28181/") {
		return "T09"
	}
	if strings.HasPrefix(file, "app/") || strings.HasPrefix(file, "bootstrap/") {
		return "T08"
	}
	return "T14"
}

func routeOwnerReason(file string) string {
	file = filepathToSlash(file)
	if strings.HasPrefix(file, "app/gb28181/") {
		return "GB28181 HTTP/SIP route registration is tracked by T09; protocol-only handlers need separate reachability review"
	}
	if strings.HasPrefix(file, "app/") {
		return "base application route registration is tracked by T08"
	}
	return "remaining route registration is tracked by T14"
}

func filepathToSlash(path string) string { return strings.ReplaceAll(path, "\\", "/") }

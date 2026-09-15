package loggingcontract

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"testing"
)

// Inspect entrypoint wiring without importing bootstrap's database-opening init.
func TestLoggingEntrypointShutdownOwnership(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "../../main.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	funcs := map[string]*ast.FuncDecl{}
	ast.Inspect(file, func(n ast.Node) bool {
		if f, ok := n.(*ast.FuncDecl); ok {
			funcs[f.Name.Name] = f
		}
		if c, ok := n.(*ast.CallExpr); ok {
			if s, ok := c.Fun.(*ast.SelectorExpr); ok && s.Sel.Name == "Fatal" {
				t.Error("Fatal bypasses runtime Close")
			}
		}
		return true
	})
	for _, name := range []string{"main", "runApplication", "stopApplication", "finishApplication"} {
		if funcs[name] == nil {
			t.Errorf("missing %s", name)
		}
	}
	if t.Failed() {
		return
	}
	var components []string
	ast.Inspect(funcs["stopApplication"].Body, func(n ast.Node) bool {
		if kv, ok := n.(*ast.KeyValueExpr); ok {
			if k, ok := kv.Key.(*ast.Ident); ok && k.Name == "Component" {
				if v, ok := kv.Value.(*ast.BasicLit); ok {
					value, _ := strconv.Unquote(v.Value)
					components = append(components, value)
				}
			}
		}
		return true
	})
	want := []string{"config_callbacks", "scheduler", "job_results", "sip_requests", "http_background", "gb28181", "casbin"}
	if len(components) != len(want) {
		t.Fatalf("shutdown steps = %v", components)
	}
	for i := range want {
		if components[i] != want[i] {
			t.Errorf("shutdown order = %v", components)
		}
	}
	attached := false
	ast.Inspect(funcs["runApplication"].Body, func(n ast.Node) bool {
		if c, ok := n.(*ast.CallExpr); ok {
			if s, ok := c.Fun.(*ast.SelectorExpr); ok && s.Sel.Name == "StartServer" && len(c.Args) == 2 {
				if arg, ok := c.Args[1].(*ast.Ident); ok && arg.Name == "stopApplication" {
					attached = true
				}
			}
		}
		return true
	})
	if !attached {
		t.Error("HTTP lifecycle does not invoke application drain")
	}
	deferredMigrationStop := false
	ast.Inspect(funcs["runApplication"].Body, func(n ast.Node) bool {
		if d, ok := n.(*ast.DeferStmt); ok {
			ast.Inspect(d.Call, func(n ast.Node) bool {
				if c, ok := n.(*ast.CallExpr); ok {
					if id, ok := c.Fun.(*ast.Ident); ok && id.Name == "stopApplication" {
						deferredMigrationStop = true
					}
				}
				return true
			})
		}
		return true
	})
	if !deferredMigrationStop {
		t.Error("migration exits must drain config callbacks before the logging runtime closes")
	}
	var closePos, exitPos token.Pos
	ast.Inspect(funcs["finishApplication"].Body, func(n ast.Node) bool {
		if c, ok := n.(*ast.CallExpr); ok {
			if s, ok := c.Fun.(*ast.SelectorExpr); ok {
				if s.Sel.Name == "Close" {
					if receiver, ok := s.X.(*ast.SelectorExpr); ok && receiver.Sel.Name == "LogRuntime" {
						if pkg, ok := receiver.X.(*ast.Ident); ok && pkg.Name == "app" {
							closePos = c.Pos()
						}
					}
				}
				if s.Sel.Name == "Exit" {
					exitPos = c.Pos()
				}
			}
		}
		return true
	})
	if closePos == token.NoPos || exitPos == token.NoPos || closePos >= exitPos {
		t.Error("logging runtime must close before process exit")
	}
	mainCalls := []string{}
	ast.Inspect(funcs["main"].Body, func(n ast.Node) bool {
		if c, ok := n.(*ast.CallExpr); ok {
			if id, ok := c.Fun.(*ast.Ident); ok {
				mainCalls = append(mainCalls, id.Name)
			}
		}
		return true
	})
	if len(mainCalls) != 2 || mainCalls[0] != "finishApplication" || mainCalls[1] != "runApplication" {
		t.Errorf("main must finish run result: %v", mainCalls)
	}
}

// Node addresses may contain URL userinfo. The known source must use the
// endpoint field so the common sanitizer strips authentication information.
func TestLoggingSeedNodeUsesSanitizedEndpoint(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "../../app/gb28181/bootstrap.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || len(call.Args) == 0 {
			return true
		}
		message, ok := call.Args[0].(*ast.BasicLit)
		if !ok || message.Value != strconv.Quote("GB28181 ZLM 已 seed 默认节点") {
			return true
		}
		for _, arg := range call.Args[1:] {
			field, ok := arg.(*ast.CallExpr)
			if !ok || len(field.Args) < 2 {
				continue
			}
			key, ok := field.Args[0].(*ast.BasicLit)
			if !ok {
				continue
			}
			if key.Value == `"host"` {
				t.Error("raw node host may expose URL userinfo")
			}
			if key.Value == `"endpoint"` {
				found = true
			}
		}
		return true
	})
	if !found {
		t.Error("seed event must retain a sanitized endpoint")
	}
}

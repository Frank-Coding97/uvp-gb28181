package loggingcontract

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoggingCascadeWarning(t *testing.T) {
	root := serverRoot(t)
	bootstrap := parseLoggingCascadeFile(t, filepath.Join(root, "app", "gb28181", "bootstrap.go"))
	cascade := parseLoggingCascadeFile(t, filepath.Join(root, "app", "gb28181", "cascade_runtime.go"))

	warning := findLoggingCascadeFunction(cascade, "warnCascadeCredentialKeyUnavailable")
	if warning == nil {
		t.Fatal("cascade credential warning helper missing")
	}
	if got := countLoggingCascadeCalls(warning.Body, "Warn"); got != 1 {
		t.Fatalf("cascade warning helper Warn calls = %d, want 1", got)
	}
	literals := loggingCascadeStringLiterals(warning.Body)
	for _, want := range []string{"event", "env", "config_write", "restricted", "encrypted_platform_runtime"} {
		if !literals[want] {
			t.Errorf("cascade warning helper missing literal %q", want)
		}
	}
	if !strings.Contains(loggingCascadeSource(t, filepath.Join(root, "app", "gb28181", "cascade_runtime.go")), cascadeCredentialKeyEnvLiteral) {
		t.Errorf("cascade warning source missing key environment %q", cascadeCredentialKeyEnvLiteral)
	}

	warningMessage := loggingCascadeConstString(cascade, "cascadeCredentialKeyWarningMessage")
	for _, want := range []string{"配置写入受限", "已有加密凭据平台运行受限"} {
		if !strings.Contains(warningMessage, want) {
			t.Errorf("cascade warning message missing %q: %q", want, warningMessage)
		}
	}

	controlPlane := requireLoggingCascadeFunction(t, bootstrap, "startControlPlane")
	if controlPlane.Type == nil || controlPlane.Type.Results == nil || len(controlPlane.Type.Results.List) != 1 {
		t.Fatal("startControlPlane must return per-start warning state")
	}
	if got := countLoggingCascadeCalls(controlPlane.Body, "warnCascadeCredentialKeyUnavailable"); got != 1 {
		t.Fatalf("startControlPlane warning helper calls = %d, want 1", got)
	}

	startDeps := requireLoggingCascadeFunction(t, bootstrap, "startSIPDependencies")
	if got := countLoggingCascadeCalls(startDeps.Body, "startCascadeRuntime"); got != 1 {
		t.Fatalf("startSIPDependencies startCascadeRuntime calls = %d, want 1", got)
	}
	if got := loggingCascadeCallArgCounts(startDeps.Body, "startCascadeRuntime"); len(got) != 1 || got[0] != 3 {
		t.Fatalf("startCascadeRuntime args from startSIPDependencies = %v, want [3]", got)
	}
	if got := countLoggingCascadeCalls(startDeps.Body, "warnCascadeCredentialKeyUnavailable"); got != 0 {
		t.Fatalf("startSIPDependencies must not own a second warning, got %d", got)
	}

	startCascade := requireLoggingCascadeFunction(t, cascade, "startCascadeRuntime")
	if got := countLoggingCascadeCalls(startCascade.Body, "Warn"); got != 0 {
		t.Fatalf("startCascadeRuntime direct Warn calls = %d, want 0", got)
	}
	if got := countLoggingCascadeCalls(startCascade.Body, "warnCascadeCredentialKeyUnavailable"); got != 1 {
		t.Fatalf("startCascadeRuntime warning helper calls = %d, want 1", got)
	}

	start := requireLoggingCascadeFunction(t, bootstrap, "Start")
	if got := loggingCascadeCallArgCounts(start.Body, "startSIPDependencies"); len(got) != 1 || got[0] != 2 {
		t.Fatalf("Start startSIPDependencies args = %v, want [2]", got)
	}
	reload := requireLoggingCascadeFunction(t, bootstrap, "ReloadSIP")
	if got := loggingCascadeCallArgCounts(reload.Body, "startSIPDependencies"); len(got) != 1 || got[0] != 2 {
		t.Fatalf("ReloadSIP startSIPDependencies args = %v, want [2]", got)
	}

	for _, source := range []string{
		loggingCascadeSource(t, filepath.Join(root, "app", "gb28181", "bootstrap.go")),
		loggingCascadeSource(t, filepath.Join(root, "app", "gb28181", "cascade_runtime.go")),
	} {
		if strings.Contains(source, "sync.Once") {
			t.Error("cascade warning must not use process-global sync.Once")
		}
	}
}

const cascadeCredentialKeyEnvLiteral = "UVP_GB28181_CASCADE_KEY"

func parseLoggingCascadeFile(t *testing.T, path string) *ast.File {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return f
}

func loggingCascadeSource(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

func findLoggingCascadeFunction(file *ast.File, name string) *ast.FuncDecl {
	for _, declaration := range file.Decls {
		if fn, ok := declaration.(*ast.FuncDecl); ok && fn.Name.Name == name {
			return fn
		}
	}
	return nil
}

func requireLoggingCascadeFunction(t *testing.T, file *ast.File, name string) *ast.FuncDecl {
	t.Helper()
	fn := findLoggingCascadeFunction(file, name)
	if fn == nil || fn.Body == nil {
		t.Fatalf("function %s missing", name)
	}
	return fn
}

func countLoggingCascadeCalls(body *ast.BlockStmt, name string) int {
	count := 0
	ast.Inspect(body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if ok && loggingCascadeCallName(call.Fun) == name {
			count++
		}
		return true
	})
	return count
}

func loggingCascadeCallArgCounts(body *ast.BlockStmt, name string) []int {
	counts := make([]int, 0)
	ast.Inspect(body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if ok && loggingCascadeCallName(call.Fun) == name {
			counts = append(counts, len(call.Args))
		}
		return true
	})
	return counts
}

func loggingCascadeCallName(expression ast.Expr) string {
	switch value := expression.(type) {
	case *ast.Ident:
		return value.Name
	case *ast.SelectorExpr:
		return value.Sel.Name
	default:
		return ""
	}
}

func loggingCascadeStringLiterals(body *ast.BlockStmt) map[string]bool {
	literals := make(map[string]bool)
	ast.Inspect(body, func(node ast.Node) bool {
		literal, ok := node.(*ast.BasicLit)
		if ok && literal.Kind == token.STRING {
			value := strings.Trim(literal.Value, "\"")
			literals[value] = true
		}
		return true
	})
	return literals
}

func loggingCascadeConstString(file *ast.File, name string) string {
	var value string
	ast.Inspect(file, func(node ast.Node) bool {
		declaration, ok := node.(*ast.ValueSpec)
		if !ok {
			return true
		}
		for index, identifier := range declaration.Names {
			if identifier.Name != name || index >= len(declaration.Values) {
				continue
			}
			literal, ok := declaration.Values[index].(*ast.BasicLit)
			if ok && literal.Kind == token.STRING {
				value = strings.Trim(literal.Value, "\"")
			}
		}
		return true
	})
	return value
}

package loggingcontract

import (
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoggingStartupFailureCleanupWiring(t *testing.T) {
	root := serverRoot(t)
	path := filepath.Join(root, "bootstrap", "init.go")
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatal(err)
	}

	var startupFail *ast.FuncDecl
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if ok && function.Name.Name == "startupFail" {
			startupFail = function
			break
		}
	}
	if startupFail == nil {
		t.Fatal("bootstrap.startupFail is missing")
	}

	functionText := nodeText(t, fset, startupFail)
	for _, required := range []string{"app.ConfigYml", "app.JobScheduler", "StopContext", "StopResultHandlerContext"} {
		if !strings.Contains(functionText, required) {
			t.Errorf("startupFail missing %s cleanup wiring", required)
		}
	}

	var calls []string
	var timeoutDuration string
	ast.Inspect(startupFail.Body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		name := selectorCallName(t, fset, call.Fun)
		if name == "" {
			return true
		}
		calls = append(calls, name)
		if name == "context.WithTimeout" && len(call.Args) == 2 {
			timeoutDuration = nodeText(t, fset, call.Args[1])
		}
		return true
	})

	if strings.ReplaceAll(timeoutDuration, " ", "") != "30*time.Second" {
		t.Errorf("startup cleanup timeout = %q, want 30*time.Second", timeoutDuration)
	}
	shutdownIndex := indexOf(calls, "ginhelper.Shutdown")
	closeIndex := indexOf(calls, "app.LogRuntime.Close")
	exitIndex := indexOf(calls, "os.Exit")
	resultIndex := indexOf(calls, "scheduler.StopResultHandlerContext")
	stopIndexes := indexesWithSuffix(calls, ".StopContext")
	if shutdownIndex < 0 {
		t.Error("startupFail must use ginhelper.Shutdown for safe cleanup records")
	}
	if resultIndex < 0 {
		t.Error("startupFail must stop the scheduler result consumer")
	}
	if len(stopIndexes) < 2 {
		t.Errorf("startupFail StopContext calls = %d, want config and scheduler", len(stopIndexes))
	}
	configIndex := strings.Index(functionText, `Component: "config"`)
	schedulerIndex := strings.Index(functionText, `Component: "scheduler"`)
	resultsComponentIndex := strings.Index(functionText, `Component: "scheduler.results"`)
	if configIndex < 0 || schedulerIndex < 0 || resultsComponentIndex < 0 || !(configIndex < schedulerIndex && schedulerIndex < resultsComponentIndex) {
		t.Errorf("cleanup component order = config:%d scheduler:%d results:%d", configIndex, schedulerIndex, resultsComponentIndex)
	}
	if shutdownIndex >= 0 && resultIndex >= 0 && shutdownIndex <= resultIndex {
		t.Errorf("ginhelper.Shutdown call index %d should follow deferred result stop %d", shutdownIndex, resultIndex)
	}
	if shutdownIndex >= 0 && closeIndex >= 0 && shutdownIndex >= closeIndex {
		t.Errorf("cleanup call index %d is not before runtime close %d", shutdownIndex, closeIndex)
	}
	if closeIndex >= 0 && exitIndex >= 0 && closeIndex >= exitIndex {
		t.Errorf("runtime close index %d is not before os.Exit %d", closeIndex, exitIndex)
	}
}

func selectorCallName(t *testing.T, fset *token.FileSet, expression ast.Expr) string {
	t.Helper()
	selector, ok := expression.(*ast.SelectorExpr)
	if !ok {
		return ""
	}
	return nodeText(t, fset, selector)
}

func nodeText(t *testing.T, fset *token.FileSet, node ast.Node) string {
	t.Helper()
	var builder strings.Builder
	if err := format.Node(&builder, fset, node); err != nil {
		t.Fatalf("format AST node: %v", err)
	}
	return builder.String()
}

func indexOf(values []string, want string) int {
	for index, value := range values {
		if value == want {
			return index
		}
	}
	return -1
}

func indexesWithSuffix(values []string, suffix string) []int {
	indexes := make([]int, 0)
	for index, value := range values {
		if strings.HasSuffix(value, suffix) {
			indexes = append(indexes, index)
		}
	}
	return indexes
}

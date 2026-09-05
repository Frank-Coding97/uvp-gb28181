package loggingcontract

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

// Parse wiring without importing bootstrap: its init opens the configured database.
func TestLoggingBootstrapWiring(t *testing.T) {
	f, err := parser.ParseFile(token.NewFileSet(), "../../bootstrap/init.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	calls := map[string]int{}
	ast.Inspect(f, func(n ast.Node) bool {
		if c, ok := n.(*ast.CallExpr); ok {
			if s, ok := c.Fun.(*ast.SelectorExpr); ok {
				calls[s.Sel.Name]++
			}
		}
		return true
	})
	for _, name := range []string{"OpenRuntime", "InstallStandardBridge", "NoticeReload", "LoadYamlFactory"} {
		if calls[name] == 0 {
			t.Errorf("bootstrap missing %s", name)
		}
	}
	b, _ := os.ReadFile("../../bootstrap/init.go")
	for _, bad := range []string{"createZapFactory", "zap.Hooks", "zap.AddStacktrace", "log.Fatal"} {
		if strings.Contains(string(b), bad) {
			t.Errorf("obsolete bootstrap output: %s", bad)
		}
	}
	if _, err := os.Stat("../../app/service/zaphooks.go"); !os.IsNotExist(err) {
		t.Error("empty per-entry goroutine hook remains")
	}
}

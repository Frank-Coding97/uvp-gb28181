package sip

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

func TestLoggingSIPBridge(t *testing.T) {
	f, err := parser.ParseFile(token.NewFileSet(), "server.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	calls := map[string]int{}
	ast.Inspect(f, func(n ast.Node) bool {
		if c, ok := n.(*ast.CallExpr); ok {
			if sel, ok := c.Fun.(*ast.SelectorExpr); ok {
				calls[sel.Sel.Name]++
				if sel.Sel.Name == "NewClient" && len(c.Args) < 2 {
					t.Error("client missing logger option")
				}
			}
		}
		return true
	})
	for _, name := range []string{"WithServerLogger", "WithClientLogger", "WithTransportLayerLogger", "WithTransactionLayerLogger"} {
		if calls[name] == 0 {
			t.Errorf("missing %s", name)
		}
	}
	b, err := os.ReadFile("../uac/uac.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "clientOptions = append(clientOptions, options...)") {
		t.Error("UAC does not accept client logger injection")
	}
}

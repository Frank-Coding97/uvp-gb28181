package contracts

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// InternalPath identifies management permission metadata only. External
// handlers remain code-owned adapters; each metadata reference must still
// identify an actual API seeded for a clean installation.
func TestEveryOpenAPIOperationReferencesRealSeededSysAPIMetadata(t *testing.T) {
	file, err := os.Open(filepath.Join("..", "..", "..", "resource", "database", "baseline", "seeds", "sys_api.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	type api struct {
		Path   string `json:"path"`
		Method string `json:"method"`
		Group  string `json:"api_group"`
	}
	type route struct{ Path, Method string }
	seeded := map[route]api{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var row api
		if err := json.Unmarshal(scanner.Bytes(), &row); err != nil {
			t.Fatal(err)
		}
		seeded[route{row.Path, row.Method}] = row
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	for _, operation := range Operations() {
		t.Run(operation.Scope, func(t *testing.T) {
			method := operation.SysAPIMethod
			if method == "" {
				method = operation.Method
			}
			metadata, exists := seeded[route{Path: operation.InternalPath, Method: method}]
			if !exists {
				t.Errorf("OpenAPI %s references missing sys_api metadata: %s %s", operation.Scope, method, operation.InternalPath)
				return
			}
			group := operation.SysAPIGroup
			if group == "" {
				group = operation.GroupName
			}
			if group != metadata.Group {
				t.Errorf("OpenAPI %s sys_api group %q differs from seeded metadata %q", operation.Scope, group, metadata.Group)
			}
		})
	}
}

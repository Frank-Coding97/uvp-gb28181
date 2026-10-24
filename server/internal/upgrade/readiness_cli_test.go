package upgrade

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWriteRuntimeReadinessQueriesExistingContainerProcess(t *testing.T) {
	expected := testRuntimeIdentity()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != RuntimeIdentityPath {
			t.Errorf("path %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(expected)
	}))
	defer server.Close()
	var output bytes.Buffer
	if err := WriteRuntimeReadiness(context.Background(), strings.TrimPrefix(server.URL, "http://"), &output); err != nil {
		t.Fatal(err)
	}
	var actual RuntimeIdentity
	if err := json.Unmarshal(output.Bytes(), &actual); err != nil {
		t.Fatal(err)
	}
	if actual != expected {
		t.Fatalf("identity %#v", actual)
	}
}

package launcher

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMediaReadinessChecksVersionAndCustomCapabilities(t *testing.T) {
	for _, mode := range []string{"valid", "missing capability", "wrong commit", "missing code", "unauthorized"} {
		t.Run(mode, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "POST" || r.URL.RawQuery != "" {
					t.Error("secret must use request body")
				}
				if r.FormValue("secret") != "test-secret" {
					t.Error("missing instance authentication")
				}
				response := map[string]any{"code": 0}
				if mode == "unauthorized" {
					response["code"] = -100
				}
				if mode == "missing code" {
					delete(response, "code")
				}
				if r.URL.Path == "/index/api/version" {
					commit := mediaCommit
					if mode == "wrong commit" {
						commit = "00000000"
					}
					response["data"] = map[string]string{"commitHash": commit[:8]}
				} else {
					names := append([]string(nil), requiredMediaAPIs...)
					if mode == "missing capability" {
						names = names[1:]
					}
					response["data"] = names
				}
				_ = json.NewEncoder(w).Encode(response)
			}))
			defer srv.Close()
			err := checkMedia(context.Background(), srv.URL, "test-secret")
			if (err == nil) != (mode == "valid") {
				t.Fatalf("mode=%s err=%v", mode, err)
			}
		})
	}
}

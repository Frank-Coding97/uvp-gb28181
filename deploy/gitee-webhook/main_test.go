package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func testHandler(t *testing.T) (webhookHandler, string) {
	t.Helper()
	queueDir := t.TempDir()
	return webhookHandler{
		cfg: config{
			Token:        "test-token",
			Repository:   "Frank-Coding/uvp-gb28181",
			Ref:          "refs/heads/develop",
			QueueDir:     queueDir,
			MaxBodyBytes: 1024,
		},
		queue: fileQueue{dir: queueDir, now: func() time.Time { return time.Unix(100, 0).UTC() }},
	}, queueDir
}

func pushBody(t *testing.T, ref, sha, repository string, deleted bool) []byte {
	t.Helper()
	body, err := json.Marshal(pushPayload{
		Ref:     ref,
		After:   sha,
		Deleted: deleted,
		Repository: repositoryPayload{
			PathWithNameSpace: repository,
		},
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	return body
}

func TestGiteeWebhookRejectsInvalidToken(t *testing.T) {
	handler, queueDir := testHandler(t)
	req := httptest.NewRequest(http.MethodPost, "/hooks/gitee", bytes.NewReader(pushBody(t, handler.cfg.Ref, "0123456789abcdef0123456789abcdef01234567", handler.cfg.Repository, false)))
	req.Header.Set("X-Gitee-Token", "wrong")
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", res.Code, http.StatusUnauthorized)
	}
	entries, err := os.ReadDir(queueDir)
	if err != nil {
		t.Fatalf("read queue: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("queue entries = %d, want 0", len(entries))
	}
}

func TestGiteeWebhookFiltersRepositoryRefAndCommit(t *testing.T) {
	tests := []struct {
		name       string
		ref        string
		sha        string
		repository string
		deleted    bool
	}{
		{name: "wrong repository", ref: "refs/heads/develop", sha: "0123456789abcdef0123456789abcdef01234567", repository: "other/repo"},
		{name: "wrong ref", ref: "refs/heads/main", sha: "0123456789abcdef0123456789abcdef01234567", repository: "Frank-Coding/uvp-gb28181"},
		{name: "bad sha", ref: "refs/heads/develop", sha: "bad", repository: "Frank-Coding/uvp-gb28181"},
		{name: "zero sha", ref: "refs/heads/develop", sha: "0000000000000000000000000000000000000000", repository: "Frank-Coding/uvp-gb28181"},
		{name: "deleted", ref: "refs/heads/develop", sha: "0123456789abcdef0123456789abcdef01234567", repository: "Frank-Coding/uvp-gb28181", deleted: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			handler, queueDir := testHandler(t)
			req := httptest.NewRequest(http.MethodPost, "/hooks/gitee", bytes.NewReader(pushBody(t, test.ref, test.sha, test.repository, test.deleted)))
			req.Header.Set("X-Gitee-Token", handler.cfg.Token)
			res := httptest.NewRecorder()
			handler.ServeHTTP(res, req)
			if res.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d", res.Code, http.StatusBadRequest)
			}
			entries, err := os.ReadDir(queueDir)
			if err != nil {
				t.Fatalf("read queue: %v", err)
			}
			if len(entries) != 0 {
				t.Fatalf("queue entries = %d, want 0", len(entries))
			}
		})
	}
}

func TestGiteeWebhookQueuesValidPushAndDeduplicates(t *testing.T) {
	handler, queueDir := testHandler(t)
	body := pushBody(t, handler.cfg.Ref, "0123456789abcdef0123456789abcdef01234567", handler.cfg.Repository, false)
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodPost, "/hooks/gitee", bytes.NewReader(body))
		req.Header.Set("X-Gitee-Token", handler.cfg.Token)
		req.Header.Set("X-Gitee-Delivery", "delivery-1")
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, req)
		if res.Code != http.StatusAccepted {
			t.Fatalf("status = %d, want %d", res.Code, http.StatusAccepted)
		}
	}
	entries, err := os.ReadDir(queueDir)
	if err != nil {
		t.Fatalf("read queue: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("queue entries = %d, want 1", len(entries))
	}
	data, err := os.ReadFile(filepath.Join(queueDir, entries[0].Name()))
	if err != nil {
		t.Fatalf("read queued job: %v", err)
	}
	var job queuedJob
	if err := json.Unmarshal(data, &job); err != nil {
		t.Fatalf("decode queued job: %v", err)
	}
	if job.SHA != "0123456789abcdef0123456789abcdef01234567" {
		t.Fatalf("queued sha = %s", job.SHA)
	}
}

func TestGiteeWebhookHealthz(t *testing.T) {
	handler, _ := testHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", res.Code, http.StatusOK)
	}
	if res.Body.String() != "ok\n" {
		t.Fatalf("body = %q", res.Body.String())
	}
}

func TestGiteeWebhookRejectsOversizedPayload(t *testing.T) {
	handler, queueDir := testHandler(t)
	body := bytes.Repeat([]byte("x"), int(handler.cfg.MaxBodyBytes)+1)
	req := httptest.NewRequest(http.MethodPost, "/hooks/gitee", bytes.NewReader(body))
	req.Header.Set("X-Gitee-Token", handler.cfg.Token)
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d", res.Code, http.StatusRequestEntityTooLarge)
	}
	entries, err := os.ReadDir(queueDir)
	if err != nil {
		t.Fatalf("read queue: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("queue entries = %d, want 0", len(entries))
	}
}

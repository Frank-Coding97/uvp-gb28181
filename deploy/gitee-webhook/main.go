package main

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var (
	commitPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)
	zeroCommit    = strings.Repeat("0", 40)
)

type config struct {
	ListenAddr   string
	Token        string
	Repository   string
	Ref          string
	QueueDir     string
	MaxBodyBytes int64
}

type repositoryPayload struct {
	FullName          string `json:"full_name"`
	PathWithNameSpace string `json:"path_with_namespace"`
}

type pushPayload struct {
	Ref        string            `json:"ref"`
	After      string            `json:"after"`
	Deleted    bool              `json:"deleted"`
	Repository repositoryPayload `json:"repository"`
}

type queuedJob struct {
	SHA        string    `json:"sha"`
	Ref        string    `json:"ref"`
	Repository string    `json:"repository"`
	Delivery   string    `json:"delivery,omitempty"`
	ReceivedAt time.Time `json:"received_at"`
}

type fileQueue struct {
	dir string
	now func() time.Time
}

func (q fileQueue) enqueue(job queuedJob) error {
	if err := os.MkdirAll(q.dir, 0o750); err != nil {
		return err
	}
	pattern := filepath.Join(q.dir, "*-"+job.SHA+".json")
	if matches, err := filepath.Glob(pattern); err != nil {
		return err
	} else if len(matches) > 0 {
		return nil
	}

	if job.ReceivedAt.IsZero() {
		job.ReceivedAt = q.now()
	}
	data, err := json.Marshal(job)
	if err != nil {
		return err
	}
	name := fmt.Sprintf("%020d-%s.json", job.ReceivedAt.UnixNano(), job.SHA)
	tmp, err := os.CreateTemp(q.dir, ".queue-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() {
		_ = os.Remove(tmpName)
	}()
	if err := tmp.Chmod(0o640); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, filepath.Join(q.dir, name))
}

type webhookHandler struct {
	cfg   config
	queue fileQueue
}

func (h webhookHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/healthz":
		h.healthz(w, r)
	case "/hooks/gitee":
		h.gitee(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (h webhookHandler) healthz(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok\n"))
}

func (h webhookHandler) gitee(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if !constantTimeEqual(r.Header.Get("X-Gitee-Token"), h.cfg.Token) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if r.ContentLength > h.cfg.MaxBodyBytes {
		http.Error(w, "payload too large", http.StatusRequestEntityTooLarge)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, h.cfg.MaxBodyBytes)
	defer r.Body.Close()
	var payload pushPayload
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&payload); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			http.Error(w, "payload too large", http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}
	repository := payload.Repository.PathWithNameSpace
	if repository == "" {
		repository = payload.Repository.FullName
	}
	if payload.Deleted || payload.After == zeroCommit || payload.Ref != h.cfg.Ref || repository != h.cfg.Repository || !commitPattern.MatchString(payload.After) {
		http.Error(w, "unsupported push", http.StatusBadRequest)
		return
	}

	job := queuedJob{
		SHA:        payload.After,
		Ref:        payload.Ref,
		Repository: repository,
		Delivery:   r.Header.Get("X-Gitee-Delivery"),
		ReceivedAt: time.Now().UTC(),
	}
	if err := h.queue.enqueue(job); err != nil {
		log.Printf("queue gitee push sha=%s: %v", job.SHA, err)
		http.Error(w, "queue unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_, _ = fmt.Fprintf(w, `{"accepted":true,"sha":%q}`+"\n", job.SHA)
}

func constantTimeEqual(got, want string) bool {
	if got == "" || want == "" {
		return false
	}
	if len(got) != len(want) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

func loadConfig() (config, error) {
	cfg := config{
		ListenAddr:   envOr("UVP_GITEE_WEBHOOK_LISTEN", "127.0.0.1:17890"),
		Token:        os.Getenv("UVP_GITEE_WEBHOOK_TOKEN"),
		Repository:   envOr("UVP_GITEE_REPOSITORY", "Frank-Coding/uvp-gb28181"),
		Ref:          envOr("UVP_GITEE_REF", "refs/heads/develop"),
		QueueDir:     envOr("UVP_GITEE_QUEUE_DIR", "/var/lib/uvp-gitee-deployer/queue"),
		MaxBodyBytes: 1 << 20,
	}
	if cfg.Token == "" {
		return config{}, errors.New("UVP_GITEE_WEBHOOK_TOKEN is required")
	}
	return cfg, nil
}

func envOr(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func main() {
	cfg, err := loadConfig()
	if err != nil {
		log.Fatal(err)
	}
	server := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           webhookHandler{cfg: cfg, queue: fileQueue{dir: cfg.QueueDir, now: time.Now}},
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	log.Printf("gitee webhook listening on %s", cfg.ListenAddr)
	log.Fatal(server.ListenAndServe())
}

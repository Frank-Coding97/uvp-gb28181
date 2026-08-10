package zlm

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
)

func TestStartRecordSendsMP4Parameters(t *testing.T) {
	client, server := newMockClient(t, func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		if r.URL.Path != "/index/api/startRecord" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		for key, want := range map[string]string{
			"type": "1", "vhost": "__defaultVhost__", "app": "rtp",
			"stream": "stream-1", "max_second": "0",
		} {
			if got := query.Get(key); got != want {
				t.Errorf("%s=%q, want %q", key, got, want)
			}
		}
		_, _ = w.Write([]byte(`{"code":0,"result":true,"msg":"success"}`))
	})
	defer server.Close()

	if err := client.StartRecord(context.Background(), "__defaultVhost__", "rtp", "stream-1", 0); err != nil {
		t.Fatalf("StartRecord failed: %v", err)
	}
}

func TestIsRecordingAndMissingStream(t *testing.T) {
	t.Run("recording", func(t *testing.T) {
		client, server := newMockClient(t, func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"code":0,"status":true}`))
		})
		defer server.Close()
		got, err := client.IsRecording(context.Background(), "__defaultVhost__", "rtp", "stream")
		if err != nil || !got {
			t.Fatalf("got=%v err=%v", got, err)
		}
	})

	t.Run("missing", func(t *testing.T) {
		client, server := newMockClient(t, func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"code":-500,"msg":"can not find the stream"}`))
		})
		defer server.Close()
		got, err := client.IsRecording(context.Background(), "__defaultVhost__", "rtp", "missing")
		if err != nil || got {
			t.Fatalf("got=%v err=%v", got, err)
		}
	})
}

func TestStopRecordIsIdempotentForMissingStream(t *testing.T) {
	client, server := newMockClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/index/api/stopRecord" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"code":-500,"msg":"can not find the stream"}`))
	})
	defer server.Close()

	if err := client.StopRecord(context.Background(), "__defaultVhost__", "rtp", "missing"); err != nil {
		t.Fatalf("missing stream should be idempotent: %v", err)
	}
}

func TestGetMP4RecordFilesSendsTupleAndParsesPaths(t *testing.T) {
	client, server := newMockClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/index/api/getMP4RecordFile" {
			t.Fatalf("path=%q", r.URL.Path)
		}
		for key, want := range map[string]string{
			"secret": "test-secret", "vhost": "__defaultVhost__", "app": "rtp", "stream": "34020000001320000001", "period": "2026-08-10",
		} {
			if got := r.URL.Query().Get(key); got != want {
				t.Fatalf("%s=%q, want %q", key, got, want)
			}
		}
		_, _ = w.Write([]byte(`{"code":0,"data":{"rootPath":"/record/rtp/camera/2026-08-10/","paths":["090000-091000.mp4","091000-092000.mp4"]}}`))
	})
	defer server.Close()

	files, err := client.GetMP4RecordFiles(context.Background(), "__defaultVhost__", "rtp", "34020000001320000001", "2026-08-10")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Fatalf("len(files)=%d", len(files))
	}
	if files[0].FilePath != "/record/rtp/camera/2026-08-10/090000-091000.mp4" || files[0].FileName != "090000-091000.mp4" || files[0].Folder != "/record/rtp/camera/2026-08-10/" {
		t.Fatalf("unexpected file: %+v", files[0])
	}
	if files[0].URL != "" || files[0].StartTime != nil || files[0].DurationMS != nil || files[0].FileSize != nil {
		t.Fatalf("directory listing must retain unavailable metadata as nil: %+v", files[0])
	}
}

func TestGetMP4RecordFilesClassifiesZLMCode(t *testing.T) {
	for _, tc := range []struct {
		name string
		code int
		want error
	}{
		{name: "not found", code: -500, want: ErrRecordingNotFound},
		{name: "auth failed", code: -100, want: ErrRecordingAccessUnavailable},
		{name: "other failure", code: -1, want: ErrRecordingAccessUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client, server := newMockClient(t, func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(`{"code":` + strconv.Itoa(tc.code) + `,"msg":"upstream detail"}`))
			})
			defer server.Close()

			_, err := client.GetMP4RecordFiles(context.Background(), "v", "a", "s", "2026-08-10")
			if !errors.Is(err, tc.want) {
				t.Fatalf("err=%v, want errors.Is(%v)", err, tc.want)
			}
		})
	}
}

func TestGetMP4RecordFilesClassifiesControlFailures(t *testing.T) {
	t.Run("malformed JSON", func(t *testing.T) {
		client, server := newMockClient(t, func(w http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(w, "not json")
		})
		defer server.Close()

		_, err := client.GetMP4RecordFiles(context.Background(), "v", "a", "s", "2026-08-10")
		if !errors.Is(err, ErrRecordingAccessUnavailable) {
			t.Fatalf("err=%v, want access unavailable", err)
		}
	})

	t.Run("node unavailable", func(t *testing.T) {
		client, server := newMockClient(t, func(http.ResponseWriter, *http.Request) {})
		server.Close()

		_, err := client.GetMP4RecordFiles(context.Background(), "v", "a", "s", "2026-08-10")
		if !errors.Is(err, ErrRecordingNodeUnavailable) {
			t.Fatalf("err=%v, want node unavailable", err)
		}
	})
}

func TestDownloadFileStreamsFullAndRangeRequests(t *testing.T) {
	for _, tc := range []struct {
		name      string
		rangeHead string
		status    int
	}{
		{name: "full", status: http.StatusOK},
		{name: "single range", rangeHead: "bytes=10-19", status: http.StatusPartialContent},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client, server := newMockClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/index/api/downloadFile" {
					t.Fatalf("path=%q", r.URL.Path)
				}
				if got := r.URL.Query().Get("secret"); got != "test-secret" {
					t.Fatalf("secret=%q", got)
				}
				if got := r.URL.Query().Get("file_path"); got != "/record/camera.mp4" {
					t.Fatalf("file_path=%q", got)
				}
				if got := r.Header.Get("Range"); got != tc.rangeHead {
					t.Fatalf("Range=%q, want %q", got, tc.rangeHead)
				}
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, "mp4-bytes")
			})
			defer server.Close()

			response, err := client.DownloadFile(context.Background(), "/record/camera.mp4", tc.rangeHead)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			if response.StatusCode != tc.status {
				t.Fatalf("status=%d, want %d", response.StatusCode, tc.status)
			}
			body, err := io.ReadAll(response.Body)
			if err != nil || string(body) != "mp4-bytes" {
				t.Fatalf("body=%q err=%v", body, err)
			}
		})
	}
}

func TestDownloadFileDoesNotFollowRedirect(t *testing.T) {
	redirectTargetHit := false
	client, server := newMockClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/final" {
			redirectTargetHit = true
		}
		http.Redirect(w, r, "/final", http.StatusFound)
	})
	defer server.Close()

	response, err := client.DownloadFile(context.Background(), "/record/camera.mp4", "")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusFound || redirectTargetHit {
		t.Fatalf("status=%d redirectTargetHit=%v", response.StatusCode, redirectTargetHit)
	}
}

func TestDownloadFileContextCancelAndSanitizesTransportErrors(t *testing.T) {
	started := make(chan struct{})
	client, server := newMockClient(t, func(_ http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
	})
	client.secret = "top-secret"
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, err := client.DownloadFile(ctx, "/absolute/private/video.mp4", "")
		result <- err
	}()
	<-started
	cancel()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v, want context.Canceled", err)
	}
	server.Close()

	_, err := client.DownloadFile(context.Background(), "/absolute/private/video.mp4", "")
	if err == nil {
		t.Fatal("expected transport error")
	}
	if got := err.Error(); strings.Contains(got, "top-secret") || strings.Contains(got, "/absolute/private/video.mp4") {
		t.Fatalf("error leaks sensitive request data: %q", got)
	}
}

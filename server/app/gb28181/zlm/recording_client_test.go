package zlm

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
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

func TestClientRecorderSupportsHLSAndMP4(t *testing.T) {
	seenTypes := map[string][]string{}
	client, server := newMockClient(t, func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		if query.Get("vhost") != "__defaultVhost__" || query.Get("app") != "rtp" || query.Get("stream") != "stream-1" {
			t.Fatalf("unexpected media tuple: %v", query)
		}
		switch r.URL.Path {
		case "/index/api/startRecord":
			if query.Get("type") != "0" && query.Get("type") != "1" {
				t.Fatalf("unexpected recorder type=%q", query.Get("type"))
			}
			seenTypes[r.URL.Path] = append(seenTypes[r.URL.Path], query.Get("type"))
			_, _ = w.Write([]byte(`{"code":0,"result":true}`))
		case "/index/api/stopRecord":
			seenTypes[r.URL.Path] = append(seenTypes[r.URL.Path], query.Get("type"))
			_, _ = w.Write([]byte(`{"code":0,"result":true}`))
		case "/index/api/isRecording":
			seenTypes[r.URL.Path] = append(seenTypes[r.URL.Path], query.Get("type"))
			_, _ = w.Write([]byte(`{"code":0,"status":true}`))
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	})
	defer server.Close()

	for _, recorderType := range []RecorderType{RecorderHLS, RecorderMP4} {
		if err := client.StartRecordWithType(context.Background(), "__defaultVhost__", "rtp", "stream-1", recorderType, 0); err != nil {
			t.Fatalf("StartRecordWithType(%d): %v", recorderType, err)
		}
		if err := client.StopRecordWithType(context.Background(), "__defaultVhost__", "rtp", "stream-1", recorderType); err != nil {
			t.Fatalf("StopRecordWithType(%d): %v", recorderType, err)
		}
		got, err := client.IsRecordingWithType(context.Background(), "__defaultVhost__", "rtp", "stream-1", recorderType)
		if err != nil || !got {
			t.Fatalf("IsRecordingWithType(%d)=%v err=%v", recorderType, got, err)
		}
	}
	for path, got := range seenTypes {
		if strings.Join(got, ",") != "0,1" {
			t.Fatalf("%s types=%v, want [0 1]", path, got)
		}
	}
}

func TestClientRecorderRejectsUnknownType(t *testing.T) {
	client := &Client{}
	if err := client.StartRecordWithType(context.Background(), "v", "a", "s", RecorderType(99), 0); err == nil {
		t.Fatal("unknown recorder type must be rejected")
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
		if _, ok := r.URL.Query()["customized_path"]; ok {
			t.Fatal("legacy listing must not send customized_path")
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

func TestGetMP4RecordFilesInDirectorySendsCustomPathAndKeepsRootPath(t *testing.T) {
	client, server := newMockClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/index/api/getMP4RecordFile" {
			t.Fatalf("path=%q", r.URL.Path)
		}
		for key, want := range map[string]string{
			"secret": "test-secret", "vhost": "__defaultVhost__", "app": "rtp",
			"stream": "34020000001320000001", "period": "2026-08-10",
			"customized_path": "/srv/uvp/work-recordings/job-1",
		} {
			if got := r.URL.Query().Get(key); got != want {
				t.Fatalf("%s=%q, want %q", key, got, want)
			}
		}
		_, _ = w.Write([]byte(`{"code":0,"data":{"rootPath":"/srv/uvp/work-recordings/job-1/record/rtp/camera/2026-08-10/","paths":["090000-091000.mp4"]}}`))
	})
	defer server.Close()

	listing, err := client.GetMP4RecordFilesInDirectory(context.Background(), "__defaultVhost__", "rtp", "34020000001320000001", "2026-08-10", "/srv/uvp/work-recordings/job-1")
	require.NoError(t, err)
	require.NotNil(t, listing)
	require.Equal(t, "/srv/uvp/work-recordings/job-1/record/rtp/camera/2026-08-10/", listing.RootPath)
	require.Len(t, listing.Files, 1)
	require.Equal(t, "/srv/uvp/work-recordings/job-1/record/rtp/camera/2026-08-10/090000-091000.mp4", listing.Files[0].FilePath)
}

func TestGetMP4RecordFilesInDirectoryKeepsRootPathWhenEmpty(t *testing.T) {
	client, server := newMockClient(t, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/srv/uvp/work-recordings/job-empty", r.URL.Query().Get("customized_path"))
		_, _ = w.Write([]byte(`{"code":0,"data":{"rootPath":"/srv/uvp/work-recordings/job-empty/record/rtp/camera/2026-08-10/","paths":[]}}`))
	})
	defer server.Close()

	listing, err := client.GetMP4RecordFilesInDirectory(context.Background(), "v", "a", "s", "2026-08-10", "/srv/uvp/work-recordings/job-empty")
	require.NoError(t, err)
	require.NotNil(t, listing)
	require.Equal(t, "/srv/uvp/work-recordings/job-empty/record/rtp/camera/2026-08-10/", listing.RootPath)
	require.Empty(t, listing.Files)
}

func TestGetMP4RecordFilesInDirectoryWithoutPeriodOmitsPeriod(t *testing.T) {
	client, server := newMockClient(t, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/srv/uvp/work-recordings/job-root", r.URL.Query().Get("customized_path"))
		if _, ok := r.URL.Query()["period"]; ok {
			t.Fatal("root listing must omit period")
		}
		_, _ = w.Write([]byte(`{"code":0,"data":{"rootPath":"/srv/uvp/work-recordings/job-root/record/rtp/camera/","paths":[]}}`))
	})
	defer server.Close()

	listing, err := client.GetMP4RecordFilesInDirectory(context.Background(), "v", "a", "s", "", "/srv/uvp/work-recordings/job-root")
	require.NoError(t, err)
	require.NotNil(t, listing)
	require.Equal(t, "/srv/uvp/work-recordings/job-root/record/rtp/camera/", listing.RootPath)
	require.Empty(t, listing.Files)
}

func TestGetMP4RecordFilesInDirectoryDoesNotReturnRootOnFailure(t *testing.T) {
	client, server := newMockClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"code":-1,"msg":"access denied","data":{"rootPath":"/must-not-be-used/","paths":[]}}`))
	})
	defer server.Close()

	listing, err := client.GetMP4RecordFilesInDirectory(context.Background(), "v", "a", "s", "", "/srv/uvp/work-recordings/job-failed")
	require.ErrorIs(t, err, ErrRecordingAccessUnavailable)
	require.Nil(t, listing)
}

func TestGetMP4RecordFilesInDirectoryRejectsInvalidCustomPath(t *testing.T) {
	calls := 0
	client, server := newMockClient(t, func(http.ResponseWriter, *http.Request) { calls++ })
	defer server.Close()

	for _, directory := range []string{"", " ", "\t", "bad\x00path"} {
		listing, err := client.GetMP4RecordFilesInDirectory(context.Background(), "v", "a", "s", "2026-08-10", directory)
		require.ErrorIs(t, err, ErrRecordingPathInvalid, "directory=%q", directory)
		require.Nil(t, listing)
	}
	require.Zero(t, calls)
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

func TestDeleteMP4RecordFileSendsExactFileParameters(t *testing.T) {
	client, server := newMockClient(t, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/index/api/deleteRecordDirectory", r.URL.Path)
		for key, want := range map[string]string{
			"secret": "test-secret", "vhost": "__defaultVhost__", "app": "rtp",
			"stream": "34020000001320000001", "period": "2026-08-10", "name": "090000-091000.mp4",
		} {
			require.Equal(t, want, r.URL.Query().Get(key), key)
		}
		_, _ = w.Write([]byte(`{"code":0,"result":true}`))
	})
	defer server.Close()

	require.NoError(t, client.DeleteMP4RecordFile(context.Background(), "__defaultVhost__", "rtp", "34020000001320000001", "2026-08-10", "090000-091000.mp4"))
}

func TestDeleteMP4RecordFileRejectsDirectoryTraversal(t *testing.T) {
	client := NewClientForNode(&node.Node{Host: "127.0.0.1", APIPort: 1, APISecret: "test-secret"})
	require.ErrorIs(t, client.DeleteMP4RecordFile(context.Background(), "v", "a", "s", "2026-08-10", "../record.mp4"), ErrRecordingPathInvalid)
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

func TestDownloadFileNormalizesWindowsAbsolutePath(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{`C:\录像 目录\record\camera.mp4`, `C:/录像 目录/record/camera.mp4`},
		{`d:/record\camera.mp4`, `d:/record/camera.mp4`},
		{`/record/camera\name.mp4`, `/record/camera\name.mp4`},
	} {
		t.Run(tc.input, func(t *testing.T) {
			client, server := newMockClient(t, func(w http.ResponseWriter, r *http.Request) {
				if got := r.URL.Query().Get("file_path"); got != tc.want {
					t.Errorf("file_path=%q, want %q", got, tc.want)
				}
				w.WriteHeader(http.StatusOK)
			})
			defer server.Close()
			response, err := client.DownloadFile(context.Background(), tc.input, "")
			if err != nil {
				t.Fatal(err)
			}
			response.Body.Close()
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

func TestDownloadFileClassifiesResponseHeaderTimeout(t *testing.T) {
	client, server := newMockClient(t, func(_ http.ResponseWriter, request *http.Request) {
		<-request.Context().Done()
	})
	defer server.Close()
	client.secret = "top-secret"
	client.downloadHTTP = newRecordingDownloadHTTPClient(10 * time.Millisecond)

	_, err := client.DownloadFile(context.Background(), "/absolute/private/video.mp4", "")
	require.ErrorIs(t, err, ErrRecordingResponseTimeout)
	require.NotContains(t, err.Error(), "top-secret")
	require.NotContains(t, err.Error(), "/absolute/private/video.mp4")
}

func TestDownloadFileKeepsConnectionTimeoutAsNodeUnavailable(t *testing.T) {
	client, server := newMockClient(t, func(http.ResponseWriter, *http.Request) {})
	server.Close()
	client.downloadHTTP = &http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
		return nil, timeoutNetworkError{}
	})}

	_, err := client.DownloadFile(context.Background(), "/absolute/private/video.mp4", "")
	require.ErrorIs(t, err, ErrRecordingNodeUnavailable)
	require.NotErrorIs(t, err, ErrRecordingResponseTimeout)
}

func TestNewClientForNodeUsesSharedRecordingDownloadHTTPClient(t *testing.T) {
	first := NewClientForNode(&node.Node{Host: "127.0.0.1", APIPort: 80})
	second := NewClientForNode(&node.Node{Host: "127.0.0.2", APIPort: 80})

	require.Nil(t, first.downloadHTTP)
	require.Nil(t, second.downloadHTTP)
	require.Same(t, defaultRecordingDownloadHTTPClient, first.recordingDownloadHTTPClient())
	require.Same(t, first.recordingDownloadHTTPClient(), second.recordingDownloadHTTPClient())
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (fn roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

type timeoutNetworkError struct{}

func (timeoutNetworkError) Error() string   { return "connection timeout" }
func (timeoutNetworkError) Timeout() bool   { return true }
func (timeoutNetworkError) Temporary() bool { return true }

var _ net.Error = timeoutNetworkError{}

func TestStartMP4InDirectoryPreservesServerChosenPath(t *testing.T) {
	calls := 0
	client, server := newMockClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		require.Equal(t, "/index/api/startRecord", r.URL.Path)
		require.Equal(t, "1", r.URL.Query().Get("type"))
		require.Equal(t, "./www/work-recordings/job-1", r.URL.Query().Get("customized_path"))
		_, _ = w.Write([]byte(`{"code":0,"result":true}`))
	})
	defer server.Close()
	require.ErrorIs(t, client.StartMP4RecordInDirectory(context.Background(), "v", "rtp", "s", 0, " "), ErrRecordingPathInvalid)
	require.Zero(t, calls)
	require.NoError(t, client.StartMP4RecordInDirectory(context.Background(), "v", "rtp", "s", 0, "./www/work-recordings/job-1"))
	require.Equal(t, 1, calls)
}

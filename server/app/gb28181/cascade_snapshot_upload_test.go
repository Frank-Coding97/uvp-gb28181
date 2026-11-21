package gb28181

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/model"
	"uvplatform.com/uvp-gb28181/app/gb28181/routes"
	"uvplatform.com/uvp-gb28181/app/gb28181/uac"
	"uvplatform.com/uvp-gb28181/internal/authoritytest"
)

type cascadeSnapshotRoundTripFunc func(*http.Request) (*http.Response, error)

func (f cascadeSnapshotRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestCascadeSnapshotUploadRestoresRouting(t *testing.T) {
	for _, method := range []string{"POST", "PUT"} {
		t.Run(method, func(t *testing.T) {
			if !authoritytest.InProcess(t) {
				return
			}
			db, p, store, _ := cascadeAuthorizedControlFixture(t, "2022", cascadeRecordSenderFunc(func(context.Context, string, string, string, []byte) (uac.TrackedMessageResult, error) {
				return uac.TrackedMessageResult{}, fmt.Errorf("unexpected SIP")
			}))
			require.NoError(t, db.Model(&p).Update("config_enabled", true).Error)
			require.NoError(t, db.Table("gb_device").Where("id=10").Update("effective_version", "2022").Error)
			saved, _, err := reserveCascadeSnapshotSession(t.Context(), db, model.GbCascadeSnapshotSession{PlatformID: p.ID, UpstreamSessionID: "same-upstream-session-01234567890123", UpstreamUploadURL: "https://storage.example.test/upload?auth=opaque&SessionID=old", SourceDeviceID: 10, SourceChannelID: 20, SourceDeviceEpoch: 1, PublishedChannelID: "34020000001320000012", ConfigRevision: p.ConfigRevision, ProjectionRevision: p.ProjectionRevision, State: model.CascadeSnapshotSessionPending, ExpiresAt: time.Now().Add(time.Minute)})
			require.NoError(t, err)
			image := []byte{0xff, 0xd8, 1, 2, 0xff, 0xd9}
			var payload bytes.Buffer
			contentType := "image/jpeg"
			suffix := "/photo.jpg"
			if method == "POST" {
				suffix = "/"
				writer := multipart.NewWriter(&payload)
				require.NoError(t, writer.WriteField("SessionID", *saved.DownstreamSessionID))
				part, err := writer.CreateFormFile("image", "photo.jpg")
				require.NoError(t, err)
				_, err = part.Write(image)
				require.NoError(t, err)
				require.NoError(t, writer.Close())
				contentType = writer.FormDataContentType()
			} else {
				payload.Write(image)
			}
			raw := append([]byte(nil), payload.Bytes()...)
			calls := 0
			client := &http.Client{Transport: cascadeSnapshotRoundTripFunc(func(req *http.Request) (*http.Response, error) {
				calls++
				require.Equal(t, "storage.example.test", req.URL.Host)
				require.Equal(t, "opaque", req.URL.Query().Get("auth"))
				require.Equal(t, saved.UpstreamSessionID, req.URL.Query().Get("SessionID"))
				require.Empty(t, req.Header.Get("Authorization"))
				if method == "POST" {
					require.NoError(t, req.ParseMultipartForm(1<<20))
					require.Equal(t, saved.UpstreamSessionID, req.FormValue("SessionID"))
					file, header, err := req.FormFile("image")
					require.NoError(t, err)
					defer file.Close()
					require.Equal(t, "photo.jpg", header.Filename)
					got, err := io.ReadAll(file)
					require.NoError(t, err)
					require.Equal(t, image, got)
				} else {
					require.Equal(t, "/upload/photo.jpg", req.URL.Path)
					got, err := io.ReadAll(req.Body)
					require.NoError(t, err)
					require.Equal(t, image, got)
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("ok")), Header: make(http.Header)}, nil
			})}
			// A newly constructed handler must recover the capability from persistent state.
			handler := newCascadeSnapshotUploadHandler(store, client)
			routes.SetCascadeSnapshotUploadHandler(handler)
			t.Cleanup(func() { routes.SetCascadeSnapshotUploadHandler(nil) })
			run := func(token, query string) int {
				router := gin.New()
				routes.RegisterContentRoutes(router)
				req := httptest.NewRequest(method, "/api/gb28181/cascade-snapshots/uploads/"+token+suffix+query, bytes.NewReader(raw))
				req.Header.Set("Content-Type", contentType)
				req.Header.Set("Authorization", "Bearer device-secret")
				response := httptest.NewRecorder()
				router.ServeHTTP(response, req)
				return response.Code
			}
			require.Equal(t, 200, run(*saved.UploadToken, "?SessionID="+*saved.DownstreamSessionID))
			require.Equal(t, 1, calls)
			require.Equal(t, 404, run("wrong", ""))
			require.Equal(t, 400, run(*saved.UploadToken, "?SessionID=other"))
			require.Equal(t, 1, calls)
			require.NoError(t, db.Model(&p).Update("config_enabled", false).Error)
			require.Equal(t, 403, run(*saved.UploadToken, ""))
			require.Equal(t, 1, calls, "revoked uploads must never contact the upstream")
		})
	}
}

func TestCascadeSnapshotUploadRejectsInvalidMultipart(t *testing.T) {
	image := []byte{0xff, 0xd8, 1, 2, 0xff, 0xd9}
	for _, test := range []struct {
		name, filename string
		fields         []string
		fileField      string
		body           []byte
	}{
		{name: "wrong session", filename: "photo.jpg", fields: []string{"other"}, body: image},
		{name: "duplicate session", filename: "photo.jpg", fields: []string{"downstream", "downstream"}, body: image},
		{name: "filename traversal", filename: "../photo.jpg", body: image},
		{name: "session disguised as file", filename: "photo.jpg", fileField: "SessionID", body: image},
		{name: "not an image", filename: "photo.jpg", body: []byte("secret")},
	} {
		t.Run(test.name, func(t *testing.T) {
			var data bytes.Buffer
			writer := multipart.NewWriter(&data)
			for _, value := range test.fields {
				require.NoError(t, writer.WriteField("SessionID", value))
			}
			field := test.fileField
			if field == "" {
				field = "image"
			}
			part, err := writer.CreateFormFile(field, test.filename)
			require.NoError(t, err)
			_, err = part.Write(test.body)
			require.NoError(t, err)
			require.NoError(t, writer.Close())
			_, _, err = buildCascadeSnapshotUpload(data.Bytes(), writer.FormDataContentType(), "downstream", "upstream")
			require.Error(t, err)
		})
	}
}

func TestCascadeSnapshotUploadRejectsLocalStorage(t *testing.T) {
	for _, address := range []string{"127.0.0.1:1", "[::1]:1", "169.254.169.254:80", "0.0.0.0:80"} {
		connection, err := dialCascadeSnapshotUpload(t.Context(), "tcp", address)
		require.Error(t, err)
		require.Nil(t, connection)
		require.Contains(t, err.Error(), "not permitted")
	}
}

func TestCascadeSnapshotUploadAuthorizationAndTransport(t *testing.T) {
	for _, name := range []string{"expired", "completed", "failed", "epoch", "deleted channel", "unpublished", "config revision", "projection revision", "duplicate query", "oversized", "upstream failure", "redirect", "read revocation", "credentials"} {
		t.Run(name, func(t *testing.T) {
			if !authoritytest.InProcess(t) {
				return
			}
			db, p, store, _ := cascadeAuthorizedControlFixture(t, "2022", cascadeRecordSenderFunc(func(context.Context, string, string, string, []byte) (uac.TrackedMessageResult, error) {
				return uac.TrackedMessageResult{}, fmt.Errorf("unexpected SIP")
			}))
			require.NoError(t, db.Model(&p).Update("config_enabled", true).Error)
			require.NoError(t, db.Table("gb_device").Where("id=10").Update("effective_version", "2022").Error)
			saved, _, err := reserveCascadeSnapshotSession(t.Context(), db, model.GbCascadeSnapshotSession{PlatformID: p.ID, UpstreamSessionID: "original", UpstreamUploadURL: "https://storage.example.test/upload?auth=opaque", SourceDeviceID: 10, SourceChannelID: 20, SourceDeviceEpoch: 1, PublishedChannelID: "34020000001320000012", ConfigRevision: p.ConfigRevision, ProjectionRevision: p.ProjectionRevision, State: model.CascadeSnapshotSessionPending, ExpiresAt: time.Now().Add(time.Minute)})
			require.NoError(t, err)
			want, calls := 403, 0
			var body io.Reader = bytes.NewReader([]byte{0xff, 0xd8, 1, 2, 0xff, 0xd9})
			query := ""
			switch name {
			case "expired":
				require.NoError(t, db.Model(&saved).Update("expires_at", time.Now().Add(-time.Second)).Error)
			case "completed":
				require.NoError(t, db.Model(&saved).Update("state", model.CascadeSnapshotSessionCompleted).Error)
			case "failed":
				require.NoError(t, db.Model(&saved).Update("state", model.CascadeSnapshotSessionFailed).Error)
			case "epoch":
				require.NoError(t, db.Table("gb_device").Where("id=10").Update("access_epoch", 2).Error)
			case "deleted channel":
				require.NoError(t, db.Exec("UPDATE gb_channel SET deleted_at=CURRENT_TIMESTAMP WHERE id=20").Error)
			case "unpublished":
				require.NoError(t, db.Exec("DELETE FROM gb_cascade_channel_projection").Error)
			case "config revision":
				require.NoError(t, db.Model(&p).Update("config_revision", p.ConfigRevision+1).Error)
			case "projection revision":
				require.NoError(t, db.Model(&p).Update("projection_revision", p.ProjectionRevision+1).Error)
			case "duplicate query":
				want = 400
				query = "?SessionID=" + *saved.DownstreamSessionID + "&sessionid=" + *saved.DownstreamSessionID
			case "oversized":
				want = 413
				body = io.LimitReader(&cascadeSnapshotRepeatingReader{}, cascadeSnapshotUploadLimit+1)
			case "upstream failure", "redirect":
				want = 502
			case "read revocation":
				body = &cascadeSnapshotOnRead{Reader: body, run: func() { require.NoError(t, db.Model(&p).Update("config_enabled", false).Error) }}
			case "credentials":
				want = 200
			}
			client := &http.Client{Transport: cascadeSnapshotRoundTripFunc(func(req *http.Request) (*http.Response, error) {
				calls++
				for _, header := range []string{"Authorization", "Cookie", "X-Forwarded-For", "X-Secret"} {
					require.Empty(t, req.Header.Get(header))
				}
				code := 200
				headers := make(http.Header)
				headers.Set("X-Storage-Secret", "private")
				if name == "upstream failure" {
					code = 500
				}
				if name == "redirect" {
					code = 307
					headers.Set("Location", "https://other.example.test/leak")
				}
				return &http.Response{StatusCode: code, Header: headers, Body: io.NopCloser(strings.NewReader("private storage error"))}, nil
			})}
			router := gin.New()
			router.PUT("/api/gb28181/cascade-snapshots/uploads/*token", newCascadeSnapshotUploadHandler(store, client))
			req := httptest.NewRequest("PUT", "/api/gb28181/cascade-snapshots/uploads/"+*saved.UploadToken+"/photo.jpg"+query, body)
			req.Header.Set("Content-Type", "image/jpeg")
			req.Header.Set("Authorization", "Bearer private")
			req.Header.Set("Cookie", "session=private")
			req.Header.Set("X-Forwarded-For", "private")
			req.Header.Set("X-Secret", "private")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, req)
			require.Equal(t, want, response.Code)
			require.Empty(t, response.Body.String())
			require.Empty(t, response.Header().Get("Location"))
			require.Empty(t, response.Header().Get("X-Storage-Secret"))
			if name == "credentials" || name == "redirect" || name == "upstream failure" {
				require.Equal(t, 1, calls)
			} else {
				require.Zero(t, calls)
			}
		})
	}
}

type cascadeSnapshotRepeatingReader struct{}

func (*cascadeSnapshotRepeatingReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 1
	}
	return len(p), nil
}

type cascadeSnapshotOnRead struct {
	io.Reader
	run func()
}

func (r *cascadeSnapshotOnRead) Read(p []byte) (int, error) {
	if r.run != nil {
		run := r.run
		r.run = nil
		run()
	}
	return r.Reader.Read(p)
}

func TestCascadeSnapshotUploadSlowBodyDeadline(t *testing.T) {
	if !authoritytest.InProcess(t) {
		return
	}
	db, p, store, _ := cascadeAuthorizedControlFixture(t, "2022", cascadeRecordSenderFunc(func(context.Context, string, string, string, []byte) (uac.TrackedMessageResult, error) {
		return uac.TrackedMessageResult{}, fmt.Errorf("unexpected SIP")
	}))
	require.NoError(t, db.Model(&p).Update("config_enabled", true).Error)
	require.NoError(t, db.Table("gb_device").Where("id=10").Update("effective_version", "2022").Error)
	saved, _, err := reserveCascadeSnapshotSession(t.Context(), db, model.GbCascadeSnapshotSession{PlatformID: p.ID, UpstreamSessionID: "slow", UpstreamUploadURL: "https://storage.example.test/upload", SourceDeviceID: 10, SourceChannelID: 20, SourceDeviceEpoch: 1, PublishedChannelID: "34020000001320000012", ConfigRevision: p.ConfigRevision, ProjectionRevision: p.ProjectionRevision, State: model.CascadeSnapshotSessionPending, ExpiresAt: time.Now().Add(time.Minute)})
	require.NoError(t, err)
	calls := 0
	router := gin.New()
	router.Use(func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 150*time.Millisecond)
		defer cancel()
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	})
	router.PUT("/uploads/*token", newCascadeSnapshotUploadHandler(store, &http.Client{Transport: cascadeSnapshotRoundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return nil, fmt.Errorf("unexpected storage upload")
	})}))
	server := httptest.NewServer(router)
	defer server.Close()
	conn, err := net.Dial("tcp", server.Listener.Addr().String())
	require.NoError(t, err)
	defer conn.Close()
	require.NoError(t, conn.SetDeadline(time.Now().Add(2*time.Second)))
	_, err = fmt.Fprintf(conn, "PUT /uploads/%s/photo.jpg HTTP/1.1\r\nHost: localhost\r\nContent-Type: image/jpeg\r\nContent-Length: 6\r\n\r\na", *saved.UploadToken)
	require.NoError(t, err)
	// A partial body must time out without waiting for the client to close.
	response, err := http.ReadResponse(bufio.NewReader(conn), nil)
	require.NoError(t, err)
	defer response.Body.Close()
	require.Equal(t, http.StatusRequestTimeout, response.StatusCode)
	require.Zero(t, calls)
}

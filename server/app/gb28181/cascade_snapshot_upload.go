package gb28181

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net"
	"net/http"
	"net/textproto"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/control"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/model"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/repository"
	"uvplatform.com/uvp-gb28181/app/gb28181/protocol"
	"uvplatform.com/uvp-gb28181/app/global/app"
)

const cascadeSnapshotUploadLimit = 16 << 20

// Upload capabilities are durable and scoped to one capture, never to a device
// or upstream as a whole. No inbound credentials or headers are forwarded.
func newCascadeSnapshotUploadHandler(store *repository.GormRepository, client *http.Client) gin.HandlerFunc {
	if client == nil {
		client = &http.Client{Transport: &http.Transport{DialContext: dialCascadeSnapshotUpload, TLSHandshakeTimeout: 5 * time.Second, ResponseHeaderTimeout: 8 * time.Second, MaxIdleConns: 4, IdleConnTimeout: 30 * time.Second}}
	}
	bounded := *client
	bounded.Timeout = 10 * time.Second
	bounded.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	// A caller-supplied cookie jar must not attach unrelated credentials.
	bounded.Jar = nil
	slots := make(chan struct{}, 4)
	return func(c *gin.Context) {
		parts := strings.Split(strings.TrimPrefix(c.Param("token"), "/"), "/")
		if len(parts) > 2 || len(parts) == 0 || parts[0] == "" {
			c.Status(http.StatusNotFound)
			return
		}
		filename := ""
		if len(parts) == 2 {
			filename = parts[1]
		}
		if filename != "" && !validCascadeSnapshotFilename(filename) {
			c.Status(http.StatusBadRequest)
			return
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
		defer cancel()
		db := app.DB()
		if db == nil || store == nil {
			c.Status(http.StatusServiceUnavailable)
			return
		}
		var session model.GbCascadeSnapshotSession
		if err := db.WithContext(ctx).Where("upload_token = ?", parts[0]).First(&session).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				c.Status(http.StatusNotFound)
			} else {
				c.Status(http.StatusServiceUnavailable)
			}
			return
		}
		if err := authorizeCascadeSnapshotUpload(ctx, db, store, session); err != nil {
			c.Status(http.StatusForbidden)
			return
		}
		if err := validateCascadeSnapshotSID(c.Request.URL.RawQuery, *session.DownstreamSessionID); err != nil {
			c.Status(http.StatusBadRequest)
			return
		}
		select {
		case slots <- struct{}{}:
			defer func() { <-slots }()
		default:
			c.Status(http.StatusServiceUnavailable)
			return
		}
		// Context cancellation alone does not interrupt an HTTP body Read.
		// Apply the same bound to the connection (or HTTP/2 stream) itself.
		controller := http.NewResponseController(c.Writer)
		deadline, _ := ctx.Deadline()
		_ = controller.SetReadDeadline(deadline)
		interrupted := make(chan struct{})
		stopRead := context.AfterFunc(ctx, func() {
			_ = controller.SetReadDeadline(time.Now())
			close(interrupted)
		})
		body, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, cascadeSnapshotUploadLimit))
		if !stopRead() {
			<-interrupted
		}
		if err == nil {
			_ = controller.SetReadDeadline(time.Time{})
		}
		if err != nil {
			var tooLarge *http.MaxBytesError
			var networkError net.Error
			if errors.As(err, &networkError) && networkError.Timeout() || ctx.Err() != nil {
				c.Status(http.StatusRequestTimeout)
			} else if errors.As(err, &tooLarge) {
				c.Status(http.StatusRequestEntityTooLarge)
			} else {
				c.Status(http.StatusBadRequest)
			}
			return
		}
		contentType := c.GetHeader("Content-Type")
		body, contentType, err = buildCascadeSnapshotUpload(body, contentType, *session.DownstreamSessionID, session.UpstreamSessionID)
		if err != nil {
			c.Status(http.StatusBadRequest)
			return
		}
		destination, err := url.Parse(session.UpstreamUploadURL)
		if err != nil || destination.User != nil || destination.Hostname() == "" || destination.Fragment != "" || (destination.Scheme != "http" && destination.Scheme != "https") {
			c.Status(http.StatusForbidden)
			return
		}
		query, err := url.ParseQuery(destination.RawQuery)
		if err != nil {
			c.Status(http.StatusForbidden)
			return
		}
		for key := range query {
			if strings.EqualFold(key, "SessionID") {
				query.Del(key)
			}
		}
		query.Set("SessionID", session.UpstreamSessionID)
		destination.RawQuery = query.Encode()
		if filename != "" {
			destination.Path = strings.TrimRight(destination.Path, "/") + "/" + filename
			destination.RawPath = ""
		}
		request, err := http.NewRequestWithContext(ctx, c.Request.Method, destination.String(), bytes.NewReader(body))
		if err != nil {
			c.Status(http.StatusBadRequest)
			return
		}
		request.Header.Set("Content-Type", contentType)
		if err := authorizeCascadeSnapshotUpload(ctx, db, store, session); err != nil {
			c.Status(http.StatusForbidden)
			return
		}
		response, err := bounded.Do(request)
		if err != nil {
			c.Status(http.StatusBadGateway)
			return
		}
		defer response.Body.Close()
		// Do not reflect storage responses, redirect locations or private headers.
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			c.Status(http.StatusBadGateway)
			return
		}
		c.Status(http.StatusOK)
	}
}

func authorizeCascadeSnapshotUpload(ctx context.Context, db *gorm.DB, store *repository.GormRepository, expected model.GbCascadeSnapshotSession) error {
	var session model.GbCascadeSnapshotSession
	if err := db.WithContext(ctx).First(&session, expected.ID).Error; err != nil {
		return err
	}
	if session.DownstreamSessionID == nil || session.UploadToken == nil || !sameCascadeSnapshotSession(session, expected) || !sameCascadeSnapshotRouting(session, expected) {
		return fmt.Errorf("snapshot routing changed")
	}
	loaded, err := control.NewGormTargetLoader(db).LoadAuthorized(ctx, session.SourceDeviceID, session.SourceChannelID)
	if err != nil || session.SourceDeviceEpoch <= 0 || loaded.DeviceEpoch != session.SourceDeviceEpoch || loaded.Profile.Version != protocol.Version2022 {
		return fmt.Errorf("snapshot source authorization changed")
	}
	platform, err := store.FindPlatform(ctx, session.PlatformID)
	if err != nil || !cascadeSnapshotSessionAuthorized(session, *platform, session.SourceDeviceID, session.SourceChannelID, time.Now()) || cascadeWireProfile(*platform).Version != protocol.Version2022 {
		return fmt.Errorf("snapshot authorization changed")
	}
	projection, err := store.ProjectionSnapshot(ctx, session.PlatformID)
	if err != nil || projection == nil || projection.Revision != session.ProjectionRevision {
		return fmt.Errorf("snapshot projection changed")
	}
	publication, err := buildCascadeCatalogSnapshot(ctx, store, &projection.Platform)
	if err != nil || publication.Revision != session.ProjectionRevision {
		return fmt.Errorf("snapshot publication changed")
	}
	target, ok := resolveCascadeDeviceStatusTarget(*projection, publication, session.PublishedChannelID)
	if !ok || target.IsPlatform || target.SourceDeviceID != session.SourceDeviceID || target.SourceChannelID != session.SourceChannelID {
		return fmt.Errorf("snapshot target unpublished")
	}
	return nil
}

func validateCascadeSnapshotSID(rawQuery, expected string) error {
	values, err := url.ParseQuery(rawQuery)
	if err != nil {
		return err
	}
	count := 0
	for name, entries := range values {
		if !strings.EqualFold(name, "SessionID") {
			continue
		}
		for _, value := range entries {
			count++
			if value != expected {
				return fmt.Errorf("snapshot session mismatch")
			}
		}
	}
	if count > 1 {
		return fmt.Errorf("duplicate snapshot session")
	}
	return nil
}

func validCascadeSnapshotFilename(name string) bool {
	return name != "." && name != ".." && len(name) <= 255 && !strings.ContainsAny(name, "/\\\x00\r\n")
}

func buildCascadeSnapshotUpload(body []byte, contentType, downstreamID, upstreamID string) ([]byte, string, error) {
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		return nil, "", err
	}
	if mediaType != "multipart/form-data" {
		if (mediaType != "image/jpeg" && mediaType != "application/octet-stream") || !cascadeSnapshotJPEG(body) {
			return nil, "", fmt.Errorf("invalid snapshot image")
		}
		return body, mediaType, nil
	}
	reader := multipart.NewReader(bytes.NewReader(body), params["boundary"])
	var output bytes.Buffer
	writer := multipart.NewWriter(&output)
	files, sessions := 0, 0
	for count := 0; ; count++ {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil || count >= 64 {
			return nil, "", fmt.Errorf("invalid snapshot multipart")
		}
		name, filename := part.FormName(), part.FileName()
		data, err := io.ReadAll(part)
		if err != nil || name == "" {
			return nil, "", fmt.Errorf("invalid snapshot part")
		}
		if filename == "" {
			if strings.EqualFold(name, "SessionID") {
				sessions++
				if sessions > 1 || string(data) != downstreamID {
					return nil, "", fmt.Errorf("snapshot session mismatch")
				}
				data = []byte(upstreamID)
			}
			if err := writer.WriteField(name, string(data)); err != nil {
				return nil, "", err
			}
		} else {
			_, disposition, dispositionErr := mime.ParseMediaType(part.Header.Get("Content-Disposition"))
			if dispositionErr != nil || disposition["filename"] != filename || strings.EqualFold(name, "SessionID") || !validCascadeSnapshotFilename(filename) || !cascadeSnapshotJPEG(data) {
				return nil, "", fmt.Errorf("invalid snapshot file")
			}
			header := make(textproto.MIMEHeader)
			header.Set("Content-Disposition", mime.FormatMediaType("form-data", map[string]string{"name": name, "filename": filename}))
			header.Set("Content-Type", "image/jpeg")
			target, err := writer.CreatePart(header)
			if err != nil {
				return nil, "", err
			}
			if _, err := target.Write(data); err != nil {
				return nil, "", err
			}
			files++
		}
	}
	if files == 0 {
		return nil, "", fmt.Errorf("snapshot file missing")
	}
	if err := writer.Close(); err != nil {
		return nil, "", err
	}
	return output.Bytes(), writer.FormDataContentType(), nil
}

func cascadeSnapshotJPEG(body []byte) bool {
	return len(body) >= 4 && body[0] == 0xff && body[1] == 0xd8 && body[len(body)-2] == 0xff && body[len(body)-1] == 0xd9
}

// Resolve once, validate every answer, and dial the checked address directly.
// Private storage networks are supported; local services and metadata endpoints
// cannot be reached through an upstream-provided URL or a DNS rebind.
func dialCascadeSnapshotUpload(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	resolved, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	local, err := net.InterfaceAddrs()
	if err != nil {
		return nil, err
	}
	if len(resolved) == 0 {
		return nil, fmt.Errorf("snapshot storage has no addresses")
	}
	for _, item := range resolved {
		ip := item.IP
		if !ip.IsGlobalUnicast() || ip.IsLoopback() || item.Zone != "" {
			return nil, fmt.Errorf("snapshot storage address is not permitted")
		}
		for _, entry := range local {
			localIP, _, err := net.ParseCIDR(entry.String())
			if err == nil && localIP.Equal(ip) {
				return nil, fmt.Errorf("snapshot storage points to this platform")
			}
		}
	}
	dialer := net.Dialer{Timeout: 2 * time.Second}
	for _, item := range resolved {
		var connection net.Conn
		connection, err = dialer.DialContext(ctx, network, net.JoinHostPort(item.IP.String(), port))
		if err == nil {
			return connection, nil
		}
	}
	return nil, err
}

package controllers_test

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/http/httptest"
	"strconv"

	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	gbcontrollers "uvplatform.cn/uvp-gb28181/app/gb28181/controllers"
	"uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/workrecording"
	gbzlm "uvplatform.cn/uvp-gb28181/app/gb28181/zlm"
)

type workRecordingAPIFake struct{ starts, stops, gets int }

func (f *workRecordingAPIFake) Start(_ context.Context, _ uint, r workrecording.StartRequest, _ int) (workrecording.Snapshot, error) {
	f.starts++
	return workrecording.Snapshot{ID: "job", ChannelID: r.ChannelID, State: workrecording.StateRecording, Version: 1}, nil
}
func (f *workRecordingAPIFake) Stop(context.Context, string) (workrecording.Snapshot, error) {
	f.stops++
	return workrecording.Snapshot{State: workrecording.StateStopped}, nil
}
func (f *workRecordingAPIFake) Get(context.Context, string) (workrecording.Snapshot, error) {
	f.gets++
	return workrecording.Snapshot{State: workrecording.StateRecording}, nil
}

// workFileSourceFake stands in for the ZLM node that stores a recording. The
// controller must read bytes from it, never from this process's own filesystem:
// a real node holds the files, and the control host has no copy.
type workFileSourceFake struct {
	payload     []byte
	missing     bool
	unreachable bool
	opens       int
}

func (f *workFileSourceFake) DownloadFile(_ context.Context, _, byteRange string) (*gbzlm.DownloadResponse, error) {
	if f.unreachable {
		return nil, fmt.Errorf("node unreachable")
	}
	f.opens++
	if f.missing {
		return &gbzlm.DownloadResponse{StatusCode: http.StatusNotFound, Header: http.Header{}, Body: io.NopCloser(bytes.NewReader(nil))}, nil
	}
	header := http.Header{}
	header.Set("Content-Type", "video/mp4")
	header.Set("Content-Length", strconv.Itoa(len(f.payload)))
	status := http.StatusOK
	if byteRange != "" {
		header.Set("Content-Range", fmt.Sprintf("bytes 0-%d/%d", len(f.payload)-1, len(f.payload)))
		header.Set("Accept-Ranges", "bytes")
		status = http.StatusPartialContent
	}
	return &gbzlm.DownloadResponse{StatusCode: status, Header: header, Body: io.NopCloser(bytes.NewReader(f.payload))}, nil
}

func workRequest(r *gin.Engine, method, path, body string) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	request := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	request.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(response, request)
	return response
}

func workDownloadRouter(t *testing.T, db *gorm.DB, actor uint, source gbcontrollers.WorkRecordingFileSource) *gin.Engine {
	t.Helper()
	controller := gbcontrollers.NewWorkRecordingController(&workRecordingAPIFake{})
	controller.SetDB(func() *gorm.DB { return db })
	if source != nil {
		controller.SetFileSourceFactory(func(int64) (gbcontrollers.WorkRecordingFileSource, bool) { return source, true })
	}
	router := gin.New()
	router.Use(gin.Recovery(), withClaims(actor))
	router.GET("/work-orders/:id/download", controller.WorkOrderDownload)
	router.GET("/work-orders/:id/files/:fileId", controller.WorkOrderFile)
	return router
}

// seedDownloadableOrder 造一张已结束、分片已归档的作业单。路径是节点上的路径，
// 控制进程无从打开，正如真实部署。
func seedDownloadableOrder(t *testing.T, db *gorm.DB, batchID, jobID string) (models.GbChannel, string) {
	t.Helper()
	require.NoError(t, db.AutoMigrate(&models.GbWorkRecordingBatch{}, &models.GbWorkRecording{}, &models.GbRecordingFile{}, &models.GbWorkRecordingFile{}))
	channel := models.GbChannel{DeviceID: "ours", ChannelID: "own", OwnerDeptID: 10}
	require.NoError(t, db.Create(&channel).Error)
	batch := models.GbWorkRecordingBatch{ID: batchID, CreatedBy: 100, RequestID: "order-" + batchID, State: workrecording.StateStopped, Version: 1, FormState: workrecording.FormSubmitted, SchemaVersion: 1, FormJSON: `{"projectName":"沪宁线放线作业"}`}
	require.NoError(t, db.Create(&batch).Error)
	root := "/node/record/work-recordings/" + jobID
	path := root + "/record/rtp/stream/2026-09-10/slice-1.mp4"
	job := models.GbWorkRecording{ID: jobID, BatchID: batch.ID, ChannelID: channel.ID, CreatedBy: 100, RequestID: "order-" + batchID + "/1", State: workrecording.StateStopped, DesiredAction: workrecording.DesiredActionStop, Version: 1, RecordingRoot: root, FileState: workrecording.FileReady, FormState: workrecording.FormSubmitted, SchemaVersion: 1, FormJSON: "{}"}
	require.NoError(t, db.Create(&job).Error)
	file := models.GbRecordingFile{ChannelID: channel.ID, DeviceID: channel.DeviceID, NodeID: 1, VHost: "__defaultVhost__", App: "rtp", Stream: "stream", FileKey: "slice-" + jobID, FileName: "slice-1.mp4", FilePath: path, MetadataState: models.RecordingMetadataComplete}
	require.NoError(t, db.Create(&file).Error)
	require.NoError(t, db.Create(&models.GbWorkRecordingFile{FileID: file.ID, WorkRecordingID: job.ID}).Error)
	return channel, path
}

func TestWorkOrderDownloadStreamsAnUncompressedZip(t *testing.T) {
	db := newScopedDeviceDB(t)
	seedDeptScopedUser(t, db, 100, 10)
	channel, _ := seedDownloadableOrder(t, db, "9b057ecd-215f-4caf-af40-6ca570ff1f62", "9710bd64-e63a-4d34-b2bd-7b70547f1215")
	source := &workFileSourceFake{payload: []byte("video-from-node")}
	router := workDownloadRouter(t, db, 100, source)

	response := workRequest(router, http.MethodGet, "/work-orders/9b057ecd-215f-4caf-af40-6ca570ff1f62/download", "")
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	require.Equal(t, "application/zip", response.Header().Get("Content-Type"))

	archive, err := zip.NewReader(bytes.NewReader(response.Body.Bytes()), int64(response.Body.Len()))
	require.NoError(t, err)
	require.Len(t, archive.File, 1)
	require.Equal(t, "camera-"+uintStr(channel.ID)+"/slice-1.mp4", archive.File[0].Name)
	content, err := archive.File[0].Open()
	require.NoError(t, err)
	defer content.Close()
	payload, err := io.ReadAll(content)
	require.NoError(t, err)
	require.Equal(t, []byte("video-from-node"), payload, "归档内容必须来自节点，而不是控制进程的文件系统")
}

// 归档必须保持不压缩（MP4 本就压缩过），且文件名要带业务信息以便归档辨认。
func TestWorkOrderDownloadStoresEntriesAndNamesArchiveAfterProject(t *testing.T) {

	db := newScopedDeviceDB(t)
	seedDeptScopedUser(t, db, 100, 10)
	channel, _ := seedDownloadableOrder(t, db, "9b057ecd-215f-4caf-af40-6ca570ff1f62", "9710bd64-e63a-4d34-b2bd-7b70547f1215")
	router := workDownloadRouter(t, db, 100, &workFileSourceFake{payload: []byte("video")})

	response := workRequest(router, http.MethodGet, "/work-orders/9b057ecd-215f-4caf-af40-6ca570ff1f62/download", "")
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())

	archive, err := zip.NewReader(bytes.NewReader(response.Body.Bytes()), int64(response.Body.Len()))
	require.NoError(t, err)
	require.Len(t, archive.File, 1)
	require.Equal(t, zip.Store, archive.File[0].Method)
	require.Equal(t, "camera-"+uintStr(channel.ID)+"/slice-1.mp4", archive.File[0].Name)

	mediaType, params, err := mime.ParseMediaType(response.Header().Get("Content-Disposition"))
	require.NoError(t, err)
	require.Equal(t, "attachment", mediaType)
	require.Contains(t, params["filename"], "沪宁线放线作业")
	require.Contains(t, params["filename"], "9b057ecd")
}

// 节点拿不到分片时必须在写出任何响应字节之前整单失败，
// 而不是流出一个"看起来正常"的坏压缩包。
func TestWorkOrderDownloadRejectsSliceTheNodeCannotServe(t *testing.T) {
	db := newScopedDeviceDB(t)
	seedDeptScopedUser(t, db, 100, 10)
	seedDownloadableOrder(t, db, "9b057ecd-215f-4caf-af40-6ca570ff1f62", "9710bd64-e63a-4d34-b2bd-7b70547f1215")
	router := workDownloadRouter(t, db, 100, &workFileSourceFake{missing: true})

	response := workRequest(router, http.MethodGet, "/work-orders/9b057ecd-215f-4caf-af40-6ca570ff1f62/download", "")
	require.Equal(t, http.StatusConflict, response.Code, response.Body.String())
	require.Contains(t, response.Body.String(), "不可读")
	_, err := zip.NewReader(bytes.NewReader(response.Body.Bytes()), int64(response.Body.Len()))
	require.Error(t, err, "失败时不得流出 ZIP 字节")
}

func TestWorkOrderDownloadReportsAnUnavailableRecordingNode(t *testing.T) {
	db := newScopedDeviceDB(t)
	seedDeptScopedUser(t, db, 100, 10)
	seedDownloadableOrder(t, db, "9b057ecd-215f-4caf-af40-6ca570ff1f62", "9710bd64-e63a-4d34-b2bd-7b70547f1215")
	// 控制器不知道这个节点(注册表未命中)时不能假装能下载。
	router := workDownloadRouter(t, db, 100, nil)

	response := workRequest(router, http.MethodGet, "/work-orders/9b057ecd-215f-4caf-af40-6ca570ff1f62/download", "")
	require.Equal(t, http.StatusServiceUnavailable, response.Code, response.Body.String())
	require.Contains(t, response.Body.String(), "媒体节点不可用")
}

func TestWorkOrderFileStreamsTheSliceFromItsNode(t *testing.T) {
	db := newScopedDeviceDB(t)
	seedDeptScopedUser(t, db, 100, 10)
	seedDownloadableOrder(t, db, "9b057ecd-215f-4caf-af40-6ca570ff1f62", "9710bd64-e63a-4d34-b2bd-7b70547f1215")
	source := &workFileSourceFake{payload: []byte("slice-bytes")}
	router := workDownloadRouter(t, db, 100, source)

	response := workRequest(router, http.MethodGet, "/work-orders/9b057ecd-215f-4caf-af40-6ca570ff1f62/files/1", "")
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	require.Equal(t, "video/mp4", response.Header().Get("Content-Type"))
	require.Equal(t, "slice-bytes", response.Body.String())
	require.Equal(t, 1, source.opens)

	// 播放器的 Range 请求要透传到节点，否则进度条无法拖动。
	ranged := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/work-orders/9b057ecd-215f-4caf-af40-6ca570ff1f62/files/1", nil)
	request.Header.Set("Range", "bytes=0-")
	router.ServeHTTP(ranged, request)
	require.Equal(t, http.StatusPartialContent, ranged.Code, ranged.Body.String())
	require.Equal(t, "slice-bytes", ranged.Body.String())
	require.Equal(t, 2, source.opens, "Range 请求也要走节点")
}

func TestWorkOrderFileFailsBeforeWritingWhenTheNodeIsUnreachable(t *testing.T) {
	db := newScopedDeviceDB(t)
	seedDeptScopedUser(t, db, 100, 10)
	seedDownloadableOrder(t, db, "9b057ecd-215f-4caf-af40-6ca570ff1f62", "9710bd64-e63a-4d34-b2bd-7b70547f1215")
	router := workDownloadRouter(t, db, 100, &workFileSourceFake{unreachable: true})

	response := workRequest(router, http.MethodGet, "/work-orders/9b057ecd-215f-4caf-af40-6ca570ff1f62/files/1", "")
	require.Equal(t, http.StatusConflict, response.Code, response.Body.String())
	require.Contains(t, response.Body.String(), "不可读")
}

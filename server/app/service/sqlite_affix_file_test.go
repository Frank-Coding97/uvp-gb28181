package service

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/models"
	"uvplatform.cn/uvp-gb28181/app/utils/uploadhelper"
)

type sqliteAffixTestConfig struct {
	sqliteSystemTestConfig
	strings map[string]string
	ints    map[string]int
	slices  map[string][]string
}

func (c sqliteAffixTestConfig) GetString(key string) string { return c.strings[key] }
func (c sqliteAffixTestConfig) GetInt(key string) int       { return c.ints[key] }
func (c sqliteAffixTestConfig) GetStringSlice(key string) []string {
	return c.slices[key]
}

func newSQLiteAffixUploadConfig(root string) sqliteAffixTestConfig {
	return sqliteAffixTestConfig{
		sqliteSystemTestConfig: sqliteSystemTestConfig{},
		strings: map[string]string{
			"gormv2.usedbtype":          "sqlite",
			"casbin.tableprefix":        "",
			"casbin.tablename":          "sys_casbin_rule",
			"server.notcheckuser":       "",
			"upload.upload_type":        "local",
			"upload.local_path":         root,
			"httpserver.serverrootpath": "/public",
		},
		ints: map[string]int{
			"upload.max_size":       16,
			"upload.chunk_max_size": 16,
			"upload.max_chunk_size": 1,
		},
		slices: map[string][]string{
			"upload.allowed_types":       {".txt"},
			"upload.chunk_allowed_types": {".txt"},
		},
	}
}

func multipartFileHeader(t *testing.T, filename string, content []byte) *multipart.FileHeader {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", filename)
	require.NoError(t, err)
	_, err = part.Write(content)
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	request := httptest.NewRequest(http.MethodPost, "/upload", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = request
	require.NoError(t, request.ParseMultipartForm(1<<20))
	file, err := context.FormFile("file")
	require.NoError(t, err)
	return file
}

func TestSQLiteAffixChunkUploadMergeResumeAndCancel(t *testing.T) {
	db := newSQLiteSystemTestDB(t)
	oldConfig, oldUpload := app.ConfigYml, app.UploadService
	root := filepath.Join(t.TempDir(), "中文 上传目录 with spaces")
	app.ConfigYml = newSQLiteAffixUploadConfig(root)
	app.UploadService = uploadhelper.NewLocalUploadService()
	t.Cleanup(func() {
		app.ConfigYml, app.UploadService = oldConfig, oldUpload
	})

	ctx := context.Background()
	affixService := NewSysAffixService()
	content := []byte("SQLite full baseline upload content\n中文")
	fileName := "中文 upload file.txt"
	fileMD5 := "0123456789abcdef0123456789abcdef"
	initRequest := &models.ChunkInitRequest{
		FileMd5: fileMD5, FileName: fileName, FileSize: int64(len(content)), ChunkSize: len(content), TotalChunks: 1,
	}
	initialized, err := affixService.InitChunkUpload(ctx, initRequest)
	require.NoError(t, err)
	require.NotEmpty(t, initialized.UploadId)
	require.Empty(t, initialized.UploadedChunks)
	require.Nil(t, initialized.ExistFile)

	chunkFile := multipartFileHeader(t, fileName, content)
	require.NoError(t, affixService.SaveChunk(ctx, &models.ChunkUploadRequest{
		File: chunkFile, UploadId: initialized.UploadId, ChunkIndex: 0, FileMd5: fileMD5, TotalChunks: 1,
	}, 10001))
	tmpDir := filepath.Join(root, "tmp", initialized.UploadId)
	tmpChunk := filepath.Join(tmpDir, "chunk_0")
	storedChunk, err := os.ReadFile(tmpChunk)
	require.NoError(t, err)
	require.Equal(t, content, storedChunk)

	resumed, err := affixService.InitChunkUpload(ctx, initRequest)
	require.NoError(t, err)
	require.Equal(t, initialized.UploadId, resumed.UploadId)
	require.Equal(t, []int{0}, resumed.UploadedChunks)

	affix, err := affixService.MergeChunks(ctx, &models.ChunkMergeRequest{
		UploadId: initialized.UploadId, FileMd5: fileMD5, FileName: fileName, FileSize: int64(len(content)), TotalChunks: 1,
	}, 10001)
	require.NoError(t, err)
	require.NotZero(t, affix.ID)
	require.Equal(t, fileName, affix.Name)
	require.Equal(t, len(content), affix.Size)
	require.Equal(t, ".txt", affix.Suffix)
	require.Contains(t, affix.Path, root)
	require.Contains(t, affix.Url, "/public/uploads/")
	merged, err := os.ReadFile(affix.Path)
	require.NoError(t, err)
	require.Equal(t, content, merged)

	require.Eventually(t, func() bool {
		_, statErr := os.Stat(tmpDir)
		return os.IsNotExist(statErr)
	}, 5*time.Second, 10*time.Millisecond)
	require.Eventually(t, func() bool {
		var count int64
		return db.Model(&models.SysAffixChunk{}).Where("upload_id = ?", initialized.UploadId).Count(&count).Error == nil && count == 0
	}, 5*time.Second, 10*time.Millisecond)

	instant, err := affixService.InitChunkUpload(ctx, initRequest)
	require.NoError(t, err)
	require.Empty(t, instant.UploadId)
	require.Empty(t, instant.UploadedChunks)
	require.Equal(t, affix.ID, instant.ExistFile.ID)

	cancelMD5 := "fedcba9876543210fedcba9876543210"
	cancelInit, err := affixService.InitChunkUpload(ctx, &models.ChunkInitRequest{
		FileMd5: cancelMD5, FileName: "cancel.txt", FileSize: int64(len(content)), ChunkSize: len(content), TotalChunks: 1,
	})
	require.NoError(t, err)
	cancelUploadID := cancelInit.UploadId
	require.NoError(t, affixService.SaveChunk(ctx, &models.ChunkUploadRequest{
		File: multipartFileHeader(t, "cancel.txt", content), UploadId: cancelUploadID, ChunkIndex: 0, FileMd5: cancelMD5, TotalChunks: 1,
	}, 10001))
	cancelDir := filepath.Join(root, "tmp", cancelUploadID)
	_, err = os.Stat(cancelDir)
	require.NoError(t, err)
	require.NoError(t, affixService.CancelChunkUpload(ctx, cancelUploadID))
	_, err = os.Stat(cancelDir)
	require.ErrorIs(t, err, os.ErrNotExist)
	var pending int64
	require.NoError(t, db.Model(&models.SysAffixChunk{}).Where("upload_id = ?", cancelUploadID).Count(&pending).Error)
	require.Zero(t, pending)

	fresh, err := affixService.InitChunkUpload(ctx, &models.ChunkInitRequest{
		FileMd5: cancelMD5, FileName: "cancel.txt", FileSize: int64(len(content)), ChunkSize: len(content), TotalChunks: 1,
	})
	require.NoError(t, err)
	require.NotEmpty(t, fresh.UploadId)
	require.NotEqual(t, cancelUploadID, fresh.UploadId)
	require.Empty(t, fresh.UploadedChunks)
}

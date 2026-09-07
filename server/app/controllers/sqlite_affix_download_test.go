package controllers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"gorm.io/gorm"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/models"
	"uvplatform.cn/uvp-gb28181/app/utils/gormhelper"
	"uvplatform.cn/uvp-gb28181/app/utils/response"
	"uvplatform.cn/uvp-gb28181/internal/sqlitebootstrap"
)

type sqliteAffixDownloadConfig struct {
	strings map[string]string
	uints   map[string][]uint
}

func (c sqliteAffixDownloadConfig) ConfigFileChangeListen(...func()) {}
func (c sqliteAffixDownloadConfig) Get(key string) interface{} {
	return c.strings[key]
}
func (c sqliteAffixDownloadConfig) GetString(key string) string { return c.strings[key] }
func (sqliteAffixDownloadConfig) GetBool(string) bool           { return false }
func (sqliteAffixDownloadConfig) GetInt(string) int             { return 0 }
func (sqliteAffixDownloadConfig) GetInt32(string) int32         { return 0 }
func (sqliteAffixDownloadConfig) GetInt64(string) int64         { return 0 }
func (sqliteAffixDownloadConfig) GetFloat64(string) float64     { return 0 }
func (sqliteAffixDownloadConfig) GetDuration(string) time.Duration {
	return 0
}
func (sqliteAffixDownloadConfig) GetStringSlice(string) []string { return nil }
func (c sqliteAffixDownloadConfig) GetUintSlice(key string) []uint {
	return c.uints[key]
}
func (sqliteAffixDownloadConfig) Set(string, interface{}) {}
func (sqliteAffixDownloadConfig) SaveConfig() error       { return nil }

func newSQLiteAffixDownloadDB(t *testing.T) *gorm.DB {
	t.Helper()
	oldDB, oldConfig, oldLog, oldResponse := app.GormDbSQLite, app.ConfigYml, app.ZapLog, app.Response
	app.ConfigYml = sqliteAffixDownloadConfig{
		strings: map[string]string{
			"gormv2.usedbtype":   "sqlite",
			"casbin.tableprefix": "",
			"casbin.tablename":   "sys_casbin_rule",
		},
		uints: map[string][]uint{"server.notcheckuser": nil},
	}
	app.ZapLog = zap.NewNop()
	app.Response = response.NewResponseHandler()
	db, err := gormhelper.NewSQLiteClient(filepath.Join(t.TempDir(), "affix-download.db"))
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	_, err = sqlitebootstrap.Initialize(ctx, db)
	require.NoError(t, err)
	require.NoError(t, sqlitebootstrap.Migrate(ctx, db))
	app.GormDbSQLite = db
	t.Cleanup(func() {
		raw, dbErr := db.DB()
		if dbErr == nil {
			_ = raw.Close()
		}
		app.GormDbSQLite, app.ConfigYml, app.ZapLog, app.Response = oldDB, oldConfig, oldLog, oldResponse
	})
	return db
}

func TestSQLiteAffixDownloadReturnsStoredFileContract(t *testing.T) {
	db := newSQLiteAffixDownloadDB(t)
	content := []byte("SQLite download contract\n中文")
	filePath := filepath.Join(t.TempDir(), "中文 directory with spaces", "download.txt")
	require.NoError(t, os.MkdirAll(filepath.Dir(filePath), 0755))
	require.NoError(t, os.WriteFile(filePath, content, 0644))
	affix := &models.SysAffix{
		BaseModel: models.BaseModel{ID: 21001},
		Name:      "中文 download.txt",
		Path:      filePath,
		Url:       "/public/uploads/download.txt",
		Size:      len(content),
		Suffix:    ".txt",
		CreatedBy: 10001,
	}
	require.NoError(t, db.Create(affix).Error)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/sysAffix/download/21001", nil)
	ctx.Params = gin.Params{{Key: "id", Value: strconv.FormatUint(uint64(affix.ID), 10)}}
	NewSysAffixController().Download(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	var body struct {
		Code int `json:"code"`
		Data struct {
			ID   uint   `json:"id"`
			Name string `json:"name"`
			URL  string `json:"url"`
			Path string `json:"path"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
	require.Zero(t, body.Code)
	require.Equal(t, affix.ID, body.Data.ID)
	require.Equal(t, affix.Name, body.Data.Name)
	require.Equal(t, affix.Url, body.Data.URL)
	require.Equal(t, affix.Path, body.Data.Path)
	returned, err := os.ReadFile(body.Data.Path)
	require.NoError(t, err)
	require.Equal(t, content, returned)
}

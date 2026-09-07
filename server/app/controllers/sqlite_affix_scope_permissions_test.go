package controllers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/global/consts"
	"uvplatform.cn/uvp-gb28181/app/models"
)

type scopedAffixFixture struct {
	owner uint
	own   models.SysAffix
	child models.SysAffix
	other models.SysAffix
}

type affixDeleteSpy struct {
	app.FileUploadService
	calls []string
}

func (s *affixDeleteSpy) DeleteFile(fileURL string) error {
	s.calls = append(s.calls, fileURL)
	return nil
}

type affixCallResult struct {
	recorder *httptest.ResponseRecorder
	panicVal interface{}
}

func newScopedAffixContext(method, path string, userID uint, body []byte) (*gin.Context, *httptest.ResponseRecorder) {
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	request := httptest.NewRequest(method, path, bytes.NewReader(body))
	if len(body) > 0 {
		request.Header.Set("Content-Type", "application/json")
	}
	ctx.Request = request
	ctx.Set(consts.BindContextKeyName, &app.Claims{ClaimsUser: app.ClaimsUser{UserID: userID}})
	ctx.Set("affix-test-recorder", recorder)
	return ctx, recorder
}

func callAffixHandler(ctx *gin.Context, handler func(*gin.Context)) (result affixCallResult) {
	recorder, _ := ctx.Get("affix-test-recorder")
	result.recorder, _ = recorder.(*httptest.ResponseRecorder)
	defer func() { result.panicVal = recover() }()
	handler(ctx)
	return result
}

func seedScopedAffixes(t *testing.T, db *gorm.DB) scopedAffixFixture {
	t.Helper()
	status := int8(1)
	rootDept, childDept, otherDept := uint(73001), uint(73002), uint(73003)
	require.NoError(t, db.Create([]models.SysDepartment{
		{BaseModel: models.BaseModel{ID: rootDept}, Name: "scope root", Status: &status},
		{BaseModel: models.BaseModel{ID: childDept}, ParentID: &rootDept, Name: "scope child", Status: &status},
		{BaseModel: models.BaseModel{ID: otherDept}, Name: "scope other", Status: &status},
	}).Error)
	owner, childUser, otherUser := uint(73011), uint(73012), uint(73013)
	require.NoError(t, db.Create([]models.User{
		{BaseModel: models.BaseModel{ID: owner}, Username: "affix-scope-owner", Password: "x", Description: "scope test", Status: 1, DeptID: rootDept},
		{BaseModel: models.BaseModel{ID: childUser}, Username: "affix-scope-child", Password: "x", Description: "scope test", Status: 1, DeptID: childDept},
		{BaseModel: models.BaseModel{ID: otherUser}, Username: "affix-scope-other", Password: "x", Description: "scope test", Status: 1, DeptID: otherDept},
	}).Error)
	role := models.SysRole{BaseModel: models.BaseModel{ID: 73021}, Name: "affix scope role", Status: 1, DataScope: 4}
	require.NoError(t, db.Create(&role).Error)
	require.NoError(t, db.Create(&models.SysUserRole{UserID: owner, RoleID: role.ID}).Error)

	root := t.TempDir()
	makeAffix := func(id, userID uint, name string) models.SysAffix {
		path := filepath.Join(root, name)
		thumbnail := filepath.Join(root, "thumb-"+name)
		require.NoError(t, os.WriteFile(path, []byte("original"), 0644))
		require.NoError(t, os.WriteFile(thumbnail, []byte("thumbnail"), 0644))
		return models.SysAffix{
			BaseModel:     models.BaseModel{ID: id},
			Name:          name,
			Path:          path,
			ThumbnailPath: thumbnail,
			Url:           "/public/uploads/" + name,
			Size:          8,
			Suffix:        ".txt",
			CreatedBy:     userID,
		}
	}
	fixture := scopedAffixFixture{owner: owner}
	fixture.own = makeAffix(73031, owner, "own.txt")
	fixture.child = makeAffix(73032, childUser, "child.txt")
	fixture.other = makeAffix(73033, otherUser, "other.txt")
	require.NoError(t, db.Create([]models.SysAffix{fixture.own, fixture.child, fixture.other}).Error)
	return fixture
}

func requireSuccessAffixResponse(t *testing.T, result affixCallResult, id uint) {
	t.Helper()
	require.Nil(t, result.panicVal)
	require.Equal(t, http.StatusOK, result.recorder.Code)
	var body struct {
		Code int `json:"code"`
		Data struct {
			ID uint `json:"id"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(result.recorder.Body.Bytes(), &body))
	require.Zero(t, body.Code)
	require.Equal(t, id, body.Data.ID)
}

func requireSuccessAffixCall(t *testing.T, result affixCallResult) {
	t.Helper()
	require.Nil(t, result.panicVal)
	require.Equal(t, http.StatusOK, result.recorder.Code)
	var body struct {
		Code int `json:"code"`
	}
	require.NoError(t, json.Unmarshal(result.recorder.Body.Bytes(), &body))
	require.Zero(t, body.Code)
}

func requireHiddenAffixResponse(t *testing.T, denied, missing affixCallResult, secret ...string) {
	t.Helper()
	require.Equal(t, consts.RequestAborted, denied.panicVal)
	require.Equal(t, consts.RequestAborted, missing.panicVal)
	require.Equal(t, denied.recorder.Code, missing.recorder.Code)
	require.Equal(t, denied.recorder.Body.String(), missing.recorder.Body.String())
	for _, value := range secret {
		require.NotContains(t, denied.recorder.Body.String(), value)
		require.NotContains(t, missing.recorder.Body.String(), value)
	}
}

func TestSQLiteAffixScopedIDEntrypoints(t *testing.T) {
	t.Run("get-by-id", func(t *testing.T) {
		db := newSQLiteAffixDownloadDB(t)
		fixture := seedScopedAffixes(t, db)
		controller := NewSysAffixController()
		allowedOwn, _ := newScopedAffixContext(http.MethodGet, "/api/sysAffix/73031", fixture.owner, nil)
		allowedOwn.Params = gin.Params{{Key: "id", Value: "73031"}}
		requireSuccessAffixResponse(t, callAffixHandler(allowedOwn, controller.GetByID), fixture.own.ID)
		allowedChild, _ := newScopedAffixContext(http.MethodGet, "/api/sysAffix/73032", fixture.owner, nil)
		allowedChild.Params = gin.Params{{Key: "id", Value: "73032"}}
		requireSuccessAffixResponse(t, callAffixHandler(allowedChild, controller.GetByID), fixture.child.ID)
		denied, _ := newScopedAffixContext(http.MethodGet, "/api/sysAffix/73033", fixture.owner, nil)
		denied.Params = gin.Params{{Key: "id", Value: "73033"}}
		missing, _ := newScopedAffixContext(http.MethodGet, "/api/sysAffix/73999", fixture.owner, nil)
		missing.Params = gin.Params{{Key: "id", Value: "73999"}}
		requireHiddenAffixResponse(t, callAffixHandler(denied, controller.GetByID), callAffixHandler(missing, controller.GetByID), fixture.other.Name, fixture.other.Path, fixture.other.Url)
	})

	t.Run("download", func(t *testing.T) {
		db := newSQLiteAffixDownloadDB(t)
		fixture := seedScopedAffixes(t, db)
		controller := NewSysAffixController()
		allowedOwn, _ := newScopedAffixContext(http.MethodGet, "/api/sysAffix/download/73031", fixture.owner, nil)
		allowedOwn.Params = gin.Params{{Key: "id", Value: "73031"}}
		requireSuccessAffixResponse(t, callAffixHandler(allowedOwn, controller.Download), fixture.own.ID)
		allowedChild, _ := newScopedAffixContext(http.MethodGet, "/api/sysAffix/download/73032", fixture.owner, nil)
		allowedChild.Params = gin.Params{{Key: "id", Value: "73032"}}
		requireSuccessAffixResponse(t, callAffixHandler(allowedChild, controller.Download), fixture.child.ID)
		denied, _ := newScopedAffixContext(http.MethodGet, "/api/sysAffix/download/73033", fixture.owner, nil)
		denied.Params = gin.Params{{Key: "id", Value: "73033"}}
		missing, _ := newScopedAffixContext(http.MethodGet, "/api/sysAffix/download/73999", fixture.owner, nil)
		missing.Params = gin.Params{{Key: "id", Value: "73999"}}
		requireHiddenAffixResponse(t, callAffixHandler(denied, controller.Download), callAffixHandler(missing, controller.Download), fixture.other.Name, fixture.other.Path, fixture.other.Url)
	})

	t.Run("update-name", func(t *testing.T) {
		db := newSQLiteAffixDownloadDB(t)
		fixture := seedScopedAffixes(t, db)
		controller := NewSysAffixController()
		allowedOwn, _ := newScopedAffixContext(http.MethodPut, "/api/sysAffix/updateName", fixture.owner, []byte(`{"id":73031,"name":"own-renamed.txt"}`))
		requireSuccessAffixResponse(t, callAffixHandler(allowedOwn, controller.UpdateName), fixture.own.ID)
		allowedChild, _ := newScopedAffixContext(http.MethodPut, "/api/sysAffix/updateName", fixture.owner, []byte(`{"id":73032,"name":"child-renamed.txt"}`))
		requireSuccessAffixResponse(t, callAffixHandler(allowedChild, controller.UpdateName), fixture.child.ID)
		denied, _ := newScopedAffixContext(http.MethodPut, "/api/sysAffix/updateName", fixture.owner, []byte(`{"id":73033,"name":"other-renamed.txt"}`))
		missing, _ := newScopedAffixContext(http.MethodPut, "/api/sysAffix/updateName", fixture.owner, []byte(`{"id":73999,"name":"missing-renamed.txt"}`))
		requireHiddenAffixResponse(t, callAffixHandler(denied, controller.UpdateName), callAffixHandler(missing, controller.UpdateName), fixture.other.Name, fixture.other.Path, fixture.other.Url)
		var other models.SysAffix
		require.NoError(t, db.First(&other, fixture.other.ID).Error)
		require.Equal(t, fixture.other.Name, other.Name)
	})

	t.Run("delete", func(t *testing.T) {
		db := newSQLiteAffixDownloadDB(t)
		fixture := seedScopedAffixes(t, db)
		oldUpload := app.UploadService
		spy := &affixDeleteSpy{}
		app.UploadService = spy
		t.Cleanup(func() { app.UploadService = oldUpload })
		controller := NewSysAffixController()
		allowedOwn, _ := newScopedAffixContext(http.MethodDelete, "/api/sysAffix/delete", fixture.owner, []byte(`{"id":73031}`))
		requireSuccessAffixCall(t, callAffixHandler(allowedOwn, controller.Delete))
		allowedChild, _ := newScopedAffixContext(http.MethodDelete, "/api/sysAffix/delete", fixture.owner, []byte(`{"id":73032}`))
		requireSuccessAffixCall(t, callAffixHandler(allowedChild, controller.Delete))
		require.Len(t, spy.calls, 4)
		callsBeforeDenied := len(spy.calls)
		denied, _ := newScopedAffixContext(http.MethodDelete, "/api/sysAffix/delete", fixture.owner, []byte(`{"id":73033}`))
		missing, _ := newScopedAffixContext(http.MethodDelete, "/api/sysAffix/delete", fixture.owner, []byte(`{"id":73999}`))
		requireHiddenAffixResponse(t, callAffixHandler(denied, controller.Delete), callAffixHandler(missing, controller.Delete), fixture.other.Name, fixture.other.Path, fixture.other.Url)
		require.Len(t, spy.calls, callsBeforeDenied)
		var other models.SysAffix
		require.NoError(t, db.First(&other, fixture.other.ID).Error)
		require.Equal(t, fixture.other.Name, other.Name)
		require.NoError(t, func() error { _, err := os.Stat(fixture.other.Path); return err }())
		require.NoError(t, func() error { _, err := os.Stat(fixture.other.ThumbnailPath); return err }())
	})
}

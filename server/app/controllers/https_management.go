package controllers

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"runtime"
	"sync"

	"github.com/gin-gonic/gin"
	"uvplatform.com/uvp-gb28181/app/middleware"
	"uvplatform.com/uvp-gb28181/internal/httpsconfig"
)

type HTTPSManagementController struct {
	Common
	once    sync.Once
	manager *httpsconfig.Manager
}

func NewHTTPSManagementController() *HTTPSManagementController { return &HTTPSManagementController{} }
func (c *HTTPSManagementController) service() *httpsconfig.Manager {
	c.once.Do(func() {
		if c.manager == nil {
			c.manager = httpsconfig.New(os.Getenv("UVP_STANDALONE_ROOT"), runtime.GOOS)
		}
	})
	return c.manager
}
func (c *HTTPSManagementController) Status(ctx *gin.Context) { c.Success(ctx, c.service().Status()) }
func (c *HTTPSManagementController) CheckPort(ctx *gin.Context) {
	m := c.service()
	if err := m.Supported(); err != nil {
		c.Fail(ctx, err.Error(), err, http.StatusConflict)
		return
	}
	var req struct {
		Port int `json:"port"`
	}
	if err := ctx.ShouldBindJSON(&req); err != nil {
		c.Fail(ctx, "端口参数无效", nil)
		return
	}
	if err := m.CheckPort(req.Port); err != nil {
		c.Fail(ctx, err.Error(), err)
		return
	}
	c.Success(ctx, gin.H{"available": true})
}
func httpsUpload(ctx *gin.Context) ([]byte, []byte, error) {
	// Bound parsing before touching disk, and remove any multipart temporary
	// files even on parse failures. Uploaded filenames are never used as paths.
	ctx.Request.Body = http.MaxBytesReader(ctx.Writer, ctx.Request.Body, 2*httpsconfig.MaxMaterialBytes+(64<<10))
	err := ctx.Request.ParseMultipartForm(2 * httpsconfig.MaxMaterialBytes)
	if ctx.Request.MultipartForm != nil {
		defer ctx.Request.MultipartForm.RemoveAll()
	}
	if err != nil {
		return nil, nil, errors.New("上传失败：请求格式无效或文件超过大小限制")
	}
	read := func(name string) ([]byte, error) {
		files := ctx.Request.MultipartForm.File[name]
		if len(files) == 0 {
			return nil, nil
		}
		if len(files) != 1 || files[0].Size > httpsconfig.MaxMaterialBytes {
			return nil, errors.New("证书及私钥各限一个文件，每个不能超过 1 MiB")
		}
		f, err := files[0].Open()
		if err != nil {
			return nil, errors.New("无法读取上传文件")
		}
		defer f.Close()
		b, err := io.ReadAll(io.LimitReader(f, httpsconfig.MaxMaterialBytes+1))
		if err != nil || len(b) > httpsconfig.MaxMaterialBytes {
			return nil, errors.New("无法读取上传文件或文件过大")
		}
		return b, nil
	}
	crt, err := read("certificate")
	if err != nil {
		return nil, nil, err
	}
	key, err := read("privateKey")
	return crt, key, err
}
func (c *HTTPSManagementController) Certificate(ctx *gin.Context) {
	middleware.MarkSensitiveOperation(ctx, map[string]any{"resource": "https_certificate", "action": "validate"})
	crt, key, err := httpsUpload(ctx)
	if err == nil {
		var info httpsconfig.CertificateInfo
		info, err = httpsconfig.ValidateCertificate(crt, key)
		if err == nil {
			c.Success(ctx, info)
			return
		}
	}
	c.Fail(ctx, err.Error(), nil)
}
func (c *HTTPSManagementController) Apply(ctx *gin.Context) {
	middleware.MarkSensitiveOperation(ctx, map[string]any{"resource": "https_configuration", "action": "apply"})
	m := c.service()
	if err := m.Supported(); err != nil {
		c.Fail(ctx, err.Error(), nil, http.StatusConflict)
		return
	}
	crt, key, err := httpsUpload(ctx)
	if err != nil {
		c.Fail(ctx, err.Error(), nil)
		return
	}
	var options struct {
		Enabled *bool `json:"enabled"`
		Port    int   `json:"port"`
		SyncZLM bool  `json:"syncZlm"`
	}
	if err = json.Unmarshal([]byte(ctx.PostForm("options")), &options); err != nil || options.Enabled == nil {
		c.Fail(ctx, "HTTPS 配置参数无效", nil)
		return
	}
	middleware.MarkSensitiveOperation(ctx, map[string]any{"resource": "https_configuration", "enabled": *options.Enabled, "port": options.Port, "certificateUpdated": len(crt) > 0, "syncZlm": options.SyncZLM})
	result, err := m.Start(httpsconfig.Request{Settings: httpsconfig.Settings{Enabled: *options.Enabled, Port: options.Port}, Certificate: crt, PrivateKey: key, SyncZLM: options.SyncZLM})
	if err != nil {
		c.Fail(ctx, err.Error(), nil, http.StatusConflict)
		return
	}
	c.Success(ctx, result)
}

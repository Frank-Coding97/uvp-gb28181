package controllers

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"uvplatform.cn/uvp-gb28181/app/gb28181/devicecapture"
	"uvplatform.cn/uvp-gb28181/app/gb28181/manscdp"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/protocol"
	"uvplatform.cn/uvp-gb28181/app/gb28181/ptz"
)

type snapshotCreateBody struct {
	SnapNum  int `json:"snapNum"`
	Interval int `json:"interval"`
}

func (dc *DeviceMgmtController) CreateSnapshotSession(c *gin.Context) {
	registry, service := dc.captureRuntime(), dc.ptzServiceSnapshot()
	claims := dc.GetClaims(c)
	if registry == nil || service == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": 503, "message": "设备抓拍服务未就绪"})
		return
	}
	if claims == nil || claims.UserID == 0 {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "通道不存在或无权限"})
		return
	}
	var body snapshotCreateBody
	if err := decodePlaybackJSON(c, &body); err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"code": 422, "message": "抓拍配置参数不合法"})
		return
	}
	if body.SnapNum == 0 {
		body.SnapNum = 1
	}
	if body.Interval == 0 {
		body.Interval = 1
	}
	if body.SnapNum < 1 || body.SnapNum > 10 || body.Interval < 1 || body.Interval > 3600 {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"code": 422, "message": "抓拍数量需为 1-10，间隔需为 1-3600 秒"})
		return
	}
	channel, ok := dc.ptzChannel(c)
	if !ok {
		return
	}
	target, ok := dc.loadPTZTarget(c, channel)
	if !ok {
		return
	}
	if target.Profile.Version != protocol.Version2022 {
		c.JSON(http.StatusConflict, gin.H{"code": 409, "message": "图像抓拍配置仅支持 GB/T 28181-2022 设备"})
		return
	}
	session := registry.Create(devicecapture.CreateRequest{
		OwnerID: strconv.FormatUint(uint64(claims.UserID), 10), ChannelID: strconv.FormatUint(uint64(channel.ID), 10),
		ChannelCode: target.ChannelCode, DeviceCode: target.DeviceCode, SnapNum: body.SnapNum, Interval: body.Interval,
	})
	uploadURL, err := snapshotUploadURL(c, session.UploadToken)
	if err != nil {
		registry.MarkFailed(session.ID, err)
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": 503, "message": err.Error()})
		return
	}
	key := strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	if key == "" {
		key = "snapshot-" + session.ID
	}
	operation, err := service.Execute(c.Request.Context(), target, ptz.Command{
		CmdType: manscdp.CmdDeviceControl, Action: "snapshot_config", IdempotencyKey: key,
		Profile: target.Profile, TargetScope: gbmodels.ControlTargetScopeChannel, TargetCode: target.ChannelCode,
		Payload: map[string]interface{}{"sessionId": session.ID, "snapNum": body.SnapNum, "interval": body.Interval},
		Build: func(sn int) ([]byte, error) {
			return manscdp.BuildSnapshotConfigWithProfile(target.Profile, target.ChannelCode, sn, manscdp.SnapshotConfig{
				SessionID: session.ID, UploadURL: uploadURL, SnapNum: body.SnapNum, Interval: body.Interval,
			})
		},
	})
	if err != nil {
		registry.MarkFailed(session.ID, err)
		c.JSON(http.StatusBadGateway, gin.H{"code": 502, "message": "下发图像抓拍配置失败: " + err.Error()})
		return
	}
	registry.MarkWaiting(session.ID)
	session, _ = registry.GetForOwner(session.ID, strconv.FormatUint(uint64(claims.UserID), 10))
	dc.Success(c, snapshotSessionView(c, session, operation.OperationID))
}

func (dc *DeviceMgmtController) GetSnapshotSession(c *gin.Context) {
	registry, claims := dc.captureRuntime(), dc.GetClaims(c)
	if registry == nil || claims == nil || claims.UserID == 0 {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "抓拍任务不存在"})
		return
	}
	session, ok := registry.GetForOwner(c.Param("sessionId"), strconv.FormatUint(uint64(claims.UserID), 10))
	if !ok || session.ChannelID != c.Param("id") {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "抓拍任务不存在"})
		return
	}
	dc.Success(c, snapshotSessionView(c, session, ""))
}

func (dc *DeviceMgmtController) UploadDeviceSnapshot(c *gin.Context) {
	registry := dc.captureRuntime()
	if registry == nil {
		c.Status(http.StatusServiceUnavailable)
		return
	}
	file, err := registry.Upload(c.Param("token"), c.Param("filename"), c.GetHeader("Content-Type"), c.Request.Body)
	if err != nil {
		status := http.StatusUnprocessableEntity
		if errors.Is(err, devicecapture.ErrNotFound) {
			status = http.StatusNotFound
		}
		c.JSON(status, gin.H{"code": status, "message": "抓拍图片接收失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": gin.H{"name": file.Name, "size": file.Size}})
}

func (dc *DeviceMgmtController) DeviceSnapshotContent(c *gin.Context) {
	registry := dc.captureRuntime()
	if registry == nil {
		c.Status(http.StatusNotFound)
		return
	}
	file, err := registry.Content(c.Param("token"), c.Param("filename"))
	if err != nil {
		c.Status(http.StatusNotFound)
		return
	}
	c.Header("Cache-Control", "private, max-age=300")
	c.File(file.Path)
}

func snapshotUploadURL(c *gin.Context, token string) (string, error) {
	scheme := strings.TrimSpace(strings.Split(c.GetHeader("X-Forwarded-Proto"), ",")[0])
	if scheme == "" {
		scheme = "http"
		if c.Request.TLS != nil {
			scheme = "https"
		}
	}
	host := strings.TrimSpace(strings.Split(c.GetHeader("X-Forwarded-Host"), ",")[0])
	if host == "" {
		host = c.Request.Host
	}
	if host == "" || scheme != "http" && scheme != "https" {
		return "", fmt.Errorf("无法生成设备可访问的抓拍上传地址")
	}
	return (&url.URL{Scheme: scheme, Host: host, Path: "/api/gb28181/device-snapshots/uploads/" + token + "/"}).String(), nil
}

func snapshotSessionView(c *gin.Context, session devicecapture.Session, operationID string) gin.H {
	files := make([]gin.H, 0, len(session.Files))
	for _, file := range session.Files {
		path := "/api/gb28181/device-snapshots/uploads/" + session.UploadToken + "/" + url.PathEscape(file.Name)
		files = append(files, gin.H{"name": file.Name, "size": file.Size, "receivedAt": file.ReceivedAt, "url": path})
	}
	view := gin.H{
		"sessionId": session.ID, "channelId": session.ChannelID, "channelCode": session.ChannelCode,
		"deviceCode": session.DeviceCode, "snapNum": session.SnapNum, "interval": session.Interval,
		"state": session.State, "receivedCount": len(session.Files), "notifiedCount": session.NotifiedCount,
		"files": files, "error": session.Error, "createdAt": session.CreatedAt, "updatedAt": session.UpdatedAt,
	}
	if operationID != "" {
		view["operationId"] = operationID
	}
	return view
}

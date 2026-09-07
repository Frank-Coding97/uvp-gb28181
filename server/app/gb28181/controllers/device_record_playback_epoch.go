package controllers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	gbplayback "uvplatform.cn/uvp-gb28181/app/gb28181/playback"
	"uvplatform.cn/uvp-gb28181/app/utils/datascope"
)

// The same SQL row supplies visibility, target identity and the original epoch.
// Resolving the opaque record key later must never refresh this authorization.
func (dc *DeviceMgmtController) loadPlaybackTarget(c *gin.Context, channelID uint) (*recordQueryTarget, gbplayback.AuthorizationSnapshot, bool) {
	db := dc.db()
	var empty gbplayback.AuthorizationSnapshot
	if db == nil {
		writePlaybackFailure(c, http.StatusServiceUnavailable, playbackFailure, "数据库未就绪", "unavailable", "service_unavailable")
		return nil, empty, false
	}
	var rows []struct {
		gbmodels.GbChannel       `gorm:"embedded"`
		RootPK                   int64
		RootCode                 string
		RootOwnerDeptID          uint
		RootEpoch, RootCompleted *int64
		RootIP, RootTransport    string
		RootStatus               int8
		RootPort                 int
		RootNodeID               int64
	}
	lookup := db.WithContext(c.Request.Context())
	result := lookup.Table("gb_channel").Select(`gb_channel.*, playback_root.id AS root_pk,
		playback_root.device_id AS root_code, playback_root.owner_dept_id AS root_owner_dept_id,
		playback_root.access_epoch AS root_epoch, playback_root.cleanup_completed_epoch AS root_completed,
		playback_root.ip AS root_ip, playback_root.port AS root_port, playback_root.transport AS root_transport,
		playback_root.status AS root_status, playback_root.zlm_node_id AS root_node_id`).
		Joins("JOIN gb_device playback_root ON playback_root.device_id = gb_channel.device_id AND playback_root.deleted_at IS NULL").
		Scopes(datascope.VisibilityScopeWithDB(c, lookup, "gb_channel.owner_dept_id", "gb_channel.device_id"),
			datascope.VisibilityScopeWithDB(c, lookup, "playback_root.owner_dept_id", "playback_root.device_id")).
		Where("gb_channel.id = ? AND gb_channel.deleted_at IS NULL", channelID).Limit(2).Find(&rows)
	if result.Error != nil {
		writePlaybackFailure(c, http.StatusServiceUnavailable, playbackFailure, "查询回放权限失败", "unavailable", "service_unavailable")
		return nil, empty, false
	}
	if result.RowsAffected != 1 || len(rows) != 1 || rows[0].RootPK <= 0 || rows[0].RootEpoch == nil || rows[0].RootCompleted == nil ||
		*rows[0].RootEpoch <= 0 || *rows[0].RootCompleted != *rows[0].RootEpoch || rows[0].OwnerDeptID != rows[0].RootOwnerDeptID {
		writePlaybackFailure(c, http.StatusNotFound, playbackNotFound, "通道不存在或暂不可回放", "not_found", "not_found")
		return nil, empty, false
	}
	row := rows[0]
	target := &recordQueryTarget{channel: row.GbChannel, device: gbmodels.GbDevice{
		DeviceID: row.RootCode, OwnerDeptID: row.RootOwnerDeptID, IP: row.RootIP, Port: row.RootPort,
		Transport: row.RootTransport, Status: row.RootStatus, ZLMNodeID: row.RootNodeID,
	}}
	target.device.ID = uint(row.RootPK)
	return target, gbplayback.AuthorizationSnapshot{DevicePK: row.RootPK, DeviceCode: row.RootCode,
		DeviceEpoch: *row.RootEpoch, CleanupCompletedEpoch: *row.RootCompleted,
		ChannelPK: int64(row.ID), ChannelCode: row.ChannelID}, true
}

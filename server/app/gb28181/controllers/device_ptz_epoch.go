package controllers

import (
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/plugin/dbresolver"
	gbconfig "uvplatform.cn/uvp-gb28181/app/gb28181/config"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
	"uvplatform.cn/uvp-gb28181/app/gb28181/ptz"
	"uvplatform.cn/uvp-gb28181/app/utils/datascope"
)

// Permission and original epoch come from the same joined row. Earlier
// controller reads are only hints; they cannot authorize or refresh a command.
func capturePTZAuthorization(c *gin.Context, db *gorm.DB, hint ptz.Target) (ptz.Target, error) {
	if db == nil {
		return ptz.Target{}, playauth.ErrDeviceIntentUnavailable
	}
	var rows []struct {
		DevicePK                        uint
		DeviceCode                      string
		ChannelPK                       uint
		ChannelCode                     string
		Owner, ChannelOwner             uint
		Epoch, Completed                *int64
		IP, Transport, EffectiveVersion string
		Port                            int
		DeviceStatus, ChannelStatus     int8
	}
	lookup := db.WithContext(c.Request.Context()).Clauses(dbresolver.Write)
	err := lookup.Table("gb_channel").Select(`ptz_root.id AS device_pk, ptz_root.device_id AS device_code,
 gb_channel.id AS channel_pk, gb_channel.channel_id AS channel_code, ptz_root.owner_dept_id AS owner,
 gb_channel.owner_dept_id AS channel_owner, ptz_root.access_epoch AS epoch, ptz_root.cleanup_completed_epoch AS completed,
 ptz_root.ip AS ip, ptz_root.port AS port, ptz_root.transport AS transport, ptz_root.effective_version AS effective_version,
 ptz_root.status AS device_status, gb_channel.status AS channel_status`).
		Joins("JOIN gb_device ptz_root ON ptz_root.device_id=gb_channel.device_id AND ptz_root.deleted_at IS NULL").
		Scopes(datascope.VisibilityScopeWithDB(c, lookup, "gb_channel.owner_dept_id", "gb_channel.device_id"),
			datascope.VisibilityScopeWithDB(c, lookup, "ptz_root.owner_dept_id", "ptz_root.device_id")).
		Where("gb_channel.id=? AND gb_channel.deleted_at IS NULL", hint.ChannelID).Limit(2).Find(&rows).Error
	if err != nil {
		return ptz.Target{}, playauth.ErrDeviceIntentUnavailable
	}
	if len(rows) != 1 {
		return ptz.Target{}, playauth.ErrDeviceIntentRevoked
	}
	r := rows[0]
	if r.DevicePK == 0 || r.DevicePK != hint.DeviceID || r.DeviceCode != hint.DeviceCode || r.ChannelPK != hint.ChannelID || r.ChannelCode != hint.ChannelCode ||
		r.Owner != r.ChannelOwner || r.Epoch == nil || r.Completed == nil || *r.Epoch <= 0 || *r.Epoch != *r.Completed {
		return ptz.Target{}, playauth.ErrDeviceIntentRevoked
	}
	return ptz.Target{DeviceID: r.DevicePK, DeviceCode: r.DeviceCode, ChannelID: r.ChannelPK, ChannelCode: r.ChannelCode, DeviceEpoch: *r.Epoch,
		IP: r.IP, Port: r.Port, Transport: r.Transport, DeviceOnline: r.DeviceStatus == gbmodels.DeviceStatusOnline, ChannelOnline: r.ChannelStatus == gbmodels.ChannelStatusOnline,
		Profile: profileForDevice(&gbmodels.GbDevice{EffectiveVersion: r.EffectiveVersion})}, nil
}

func (dc *DeviceMgmtController) authorizePTZTarget(c *gin.Context, target *ptz.Target) bool {
	if !gbconfig.CurrentPlayAuthSettings().RequiredByOpenAPI {
		return true
	}
	captured, err := capturePTZAuthorization(c, dc.db(), *target)
	if err != nil {
		dc.FailAndAbort(c, "设备权限已失效或正在转移", err)
		return false
	}
	*target = captured
	return true
}

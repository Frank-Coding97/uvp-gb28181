package recordcache

import (
	"context"
	"net"
	"strconv"
	"strings"

	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
	gbplayback "uvplatform.com/uvp-gb28181/app/gb28181/playback"
)

// Target 是缓存任务要拉流的那一对（设备, 通道）的全部事实。
//
// ⭐ 为什么服务层要自己载入它：缓存任务是**长跑**的（长录像要分片续播几十分钟），
// 每次续播都要重新建一个 download 回放会话，而建会话需要授权快照
// （epoch / cleanup_completed_epoch / 传输参数）。这些事实在进程重启后
// 只能从库里重新读，所以它必须是服务的依赖，而不是控制器在建任务时
// 一次性塞进来的入参。
type Target struct {
	ChannelPK     uint
	ChannelCode   string
	ChannelName   string
	ChannelStatus int8
	// StreamTransport 是通道的传输偏好（含 TCP 时按 TCP 拉流）。
	StreamTransport string

	DevicePK              int64
	DeviceCode            string
	DeviceName            string
	DeviceStatus          int8
	DeviceEpoch           int64
	CleanupCompletedEpoch int64
	DeviceIP              string
	DevicePort            int
	DeviceTransport       string
	PreferredNodeID       int64

	OwnerDeptID uint
}

// Online 报告设备与通道是否都在线（缓存任务要求两者在线，与回放一致）。
func (t Target) Online() bool {
	return t.DeviceStatus == gbmodels.DeviceStatusOnline && t.ChannelStatus == gbmodels.ChannelStatusOnline
}

// Authorization 把目标事实转成回放服务要的授权快照。
func (t Target) Authorization() gbplayback.AuthorizationSnapshot {
	return gbplayback.AuthorizationSnapshot{
		DevicePK: t.DevicePK, DeviceCode: t.DeviceCode,
		DeviceEpoch: t.DeviceEpoch, CleanupCompletedEpoch: t.CleanupCompletedEpoch,
		ChannelPK: int64(t.ChannelPK), ChannelCode: t.ChannelCode,
	}
}

// Destination 是 SIP 目的地址 host:port。
func (t Target) Destination() string {
	return joinHostPort(t.DeviceIP, t.DevicePort)
}

// TCPMode 报告通道是否要求 TCP 传输。
func (t Target) TCPMode() bool {
	return strings.Contains(strings.ToUpper(t.StreamTransport), "TCP")
}

// TargetLoader 载入（设备, 通道）事实。channelID 与 deviceCode 至少给一个；
// 两个都给时按二者同时匹配（防止任务里的通道被归属迁移到别的设备后错拉）。
type TargetLoader interface {
	LoadTarget(ctx context.Context, channelID uint, deviceCode string) (Target, error)
}

// LoadTarget 实现 TargetLoader。
func (r *GormRepo) LoadTarget(ctx context.Context, channelID uint, deviceCode string) (Target, error) {
	if r == nil || r.db == nil || (channelID == 0 && strings.TrimSpace(deviceCode) == "") {
		return Target{}, ErrInvalidRequest
	}
	var rows []struct {
		gbmodels.GbChannel `gorm:"embedded"`
		RootPK             int64
		RootCode           string
		RootOwnerDeptID    uint
		RootEpoch          *int64
		RootCompleted      *int64
		RootIP             string
		RootTransport      string
		RootStatus         int8
		RootPort           int
		RootNodeID         int64
		RootName           string
	}
	query := r.db.WithContext(ctx).Table("gb_channel").
		Select(`gb_channel.*, root.id AS root_pk, root.device_id AS root_code,
			root.owner_dept_id AS root_owner_dept_id, root.access_epoch AS root_epoch,
			root.cleanup_completed_epoch AS root_completed, root.ip AS root_ip,
			root.port AS root_port, root.transport AS root_transport,
			root.status AS root_status, root.zlm_node_id AS root_node_id, root.name AS root_name`).
		Joins("JOIN gb_device root ON root.device_id = gb_channel.device_id AND root.deleted_at IS NULL").
		Where("gb_channel.deleted_at IS NULL")
	if channelID > 0 {
		query = query.Where("gb_channel.id = ?", channelID)
	}
	if code := strings.TrimSpace(deviceCode); code != "" {
		query = query.Where("gb_channel.device_id = ?", code)
	}
	if err := query.Limit(2).Find(&rows).Error; err != nil {
		return Target{}, err
	}
	if len(rows) != 1 {
		return Target{}, ErrTaskNotFound
	}
	row := rows[0]
	if row.RootPK <= 0 || row.RootEpoch == nil || row.RootCompleted == nil || *row.RootEpoch <= 0 ||
		*row.RootCompleted != *row.RootEpoch {
		// 归属迁移未完成（cleanup 未对齐）时 fail-closed：与回放路径同一条判据，
		// 否则会在旧 epoch 上发起媒体操作，被 playauth 拒掉后表现为"点播失败"。
		return Target{}, ErrTaskNotFound
	}
	target := Target{
		ChannelPK: row.ID, ChannelCode: row.ChannelID, ChannelName: row.Name,
		ChannelStatus: row.Status, StreamTransport: row.StreamTransport,
		DevicePK: row.RootPK, DeviceCode: row.RootCode, DeviceName: row.RootName,
		DeviceStatus: row.RootStatus, DeviceEpoch: *row.RootEpoch, CleanupCompletedEpoch: *row.RootCompleted,
		DeviceIP: row.RootIP, DevicePort: row.RootPort, DeviceTransport: row.RootTransport,
		PreferredNodeID: row.RootNodeID, OwnerDeptID: row.RootOwnerDeptID,
	}
	return target, nil
}

// joinHostPort 拼 SIP 目的地址。端口缺失时不写 ":0"（那会变成非法目的地址）。
func joinHostPort(host string, port int) string {
	host = strings.TrimSpace(host)
	if host == "" {
		return ""
	}
	if port <= 0 {
		return host
	}
	return net.JoinHostPort(host, strconv.Itoa(port))
}

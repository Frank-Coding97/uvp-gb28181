package favorites

import (
	"errors"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/utils/datascope"
)

const (
	ErrGroupNameInvalid  = "CHANNEL_FAVORITE_GROUP_NAME_INVALID"
	ErrGroupNameConflict = "CHANNEL_FAVORITE_GROUP_NAME_CONFLICT"
	ErrChannelInvalid    = "CHANNEL_FAVORITE_CHANNEL_INVALID"
	ErrChannelNotVisible = "CHANNEL_FAVORITE_CHANNEL_NOT_VISIBLE"
	ErrGroupNotFound     = "CHANNEL_FAVORITE_GROUP_NOT_FOUND"
)

type DomainError struct {
	Code    string
	Message string
}

func (e *DomainError) Error() string { return e.Code + ": " + e.Message }

type ChannelInput struct {
	DeviceCode  string `json:"deviceCode"`
	ChannelCode string `json:"channelCode"`
}
type CreateRequest struct {
	Name     string         `json:"name"`
	Channels []ChannelInput `json:"channels"`
}
type AppendResult struct {
	RequestedCount int `json:"requestedCount"`
	AddedCount     int `json:"addedCount"`
	SkippedCount   int `json:"skippedCount"`
}

type ChannelDTO struct {
	ID                    uint    `json:"id"`
	DeviceID              string  `json:"deviceId"`
	ChannelID             string  `json:"channelId"`
	Name                  string  `json:"name"`
	Alias                 string  `json:"alias"`
	Manufacturer          string  `json:"manufacturer"`
	Model                 string  `json:"model"`
	Owner                 string  `json:"owner"`
	CivilCode             string  `json:"civilCode"`
	ParentID              string  `json:"parentId"`
	PTZType               int8    `json:"ptzType"`
	Longitude             float64 `json:"longitude"`
	Latitude              float64 `json:"latitude"`
	Status                int8    `json:"status"`
	StreamID              string  `json:"streamId"`
	OnDemandLive          bool    `json:"onDemandLive"`
	StreamTransport       string  `json:"streamTransport"`
	AudioEnabled          bool    `json:"audioEnabled"`
	CloudRecordingEnabled bool    `json:"cloudRecordingEnabled"`
}
type ItemDTO struct {
	ID          uint        `json:"id"`
	DeviceCode  string      `json:"deviceCode"`
	ChannelCode string      `json:"channelCode"`
	DeviceName  string      `json:"deviceName"`
	ChannelName string      `json:"channelName"`
	Channel     *ChannelDTO `json:"channel,omitempty"`
}
type GroupDTO struct {
	ID               uint      `json:"id"`
	Name             string    `json:"name"`
	Items            []ItemDTO `json:"items"`
	AvailableCount   int       `json:"availableCount"`
	UnavailableCount int       `json:"unavailableCount"`
}

type Service struct{ db *gorm.DB }

func NewService(db *gorm.DB) *Service { return &Service{db: db} }

func (s *Service) visibleChannel(c *gin.Context, db *gorm.DB, deviceCode, channelCode string) (*gbmodels.GbChannel, error) {
	deviceCode, channelCode = strings.TrimSpace(deviceCode), strings.TrimSpace(channelCode)
	if deviceCode == "" || channelCode == "" {
		return nil, &DomainError{ErrChannelInvalid, "设备编码和通道编码不能为空"}
	}
	var channel gbmodels.GbChannel
	result := db.WithContext(c).Model(&gbmodels.GbChannel{}).
		Scopes(datascope.VisibilityScopeWithDB(c, db, "owner_dept_id", "device_id")).
		Where("device_id = ? AND channel_id = ?", deviceCode, channelCode).Limit(1).Find(&channel)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, &DomainError{ErrChannelNotVisible, "通道不可见"}
	}
	return &channel, nil
}

func (s *Service) group(c *gin.Context, db *gorm.DB, id, actor uint) (*gbmodels.GbChannelFavoriteGroup, error) {
	var group gbmodels.GbChannelFavoriteGroup
	result := db.WithContext(c).Where("id = ? AND owner_user_id = ?", id, actor).Limit(1).Find(&group)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, &DomainError{ErrGroupNotFound, "收藏组不存在"}
	}
	return &group, nil
}

func channelDTO(ch *gbmodels.GbChannel) *ChannelDTO {
	return &ChannelDTO{ID: ch.ID, DeviceID: ch.DeviceID, ChannelID: ch.ChannelID, Name: ch.Name, Alias: ch.Alias, Manufacturer: ch.Manufacturer, Model: ch.Model, Owner: ch.Owner, CivilCode: ch.CivilCode, ParentID: ch.ParentID, PTZType: ch.PTZType, Longitude: ch.Longitude, Latitude: ch.Latitude, Status: ch.Status, StreamID: ch.StreamID, OnDemandLive: ch.OnDemandLive, StreamTransport: ch.StreamTransport, AudioEnabled: ch.AudioEnabled, CloudRecordingEnabled: ch.CloudRecordingEnabled}
}

func (s *Service) List(c *gin.Context, actor uint) ([]GroupDTO, error) {
	var groups []gbmodels.GbChannelFavoriteGroup
	if err := s.db.WithContext(c).Where("owner_user_id = ?", actor).Order("id ASC").Find(&groups).Error; err != nil {
		return nil, err
	}
	result := make([]GroupDTO, 0, len(groups))
	for _, group := range groups {
		var items []gbmodels.GbChannelFavoriteItem
		if err := s.db.WithContext(c).Where("group_id = ?", group.ID).Order("id ASC").Find(&items).Error; err != nil {
			return nil, err
		}
		dto := GroupDTO{ID: group.ID, Name: group.Name, Items: make([]ItemDTO, 0)}
		for _, item := range items {
			entry := ItemDTO{ID: item.ID, DeviceCode: item.DeviceCode, ChannelCode: item.ChannelCode, DeviceName: item.DeviceName, ChannelName: item.ChannelName}
			ch, err := s.visibleChannel(c, s.db, item.DeviceCode, item.ChannelCode)
			if err == nil {
				entry.Channel = channelDTO(ch)
				dto.AvailableCount++
			} else {
				var domain *DomainError
				if errors.As(err, &domain) && domain.Code == ErrChannelNotVisible {
					dto.UnavailableCount++
					continue
				}
				return nil, err
			}
			dto.Items = append(dto.Items, entry)
		}
		result = append(result, dto)
	}
	return result, nil
}

func (s *Service) Create(c *gin.Context, actor uint, req CreateRequest) (GroupDTO, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" || len([]rune(name)) > 64 || len(req.Channels) == 0 {
		return GroupDTO{}, &DomainError{ErrGroupNameInvalid, "收藏组名称和通道不能为空"}
	}
	var dto GroupDTO
	err := s.db.WithContext(c).Transaction(func(tx *gorm.DB) error {
		var group gbmodels.GbChannelFavoriteGroup
		if err := tx.Where("owner_user_id = ? AND name = ?", actor, name).Limit(1).Find(&group).Error; err != nil {
			return err
		}
		if group.ID != 0 {
			return &DomainError{ErrGroupNameConflict, "收藏组名称已存在"}
		}
		group = gbmodels.GbChannelFavoriteGroup{OwnerUserID: actor, Name: name}
		if err := tx.Create(&group).Error; err != nil {
			return err
		}
		for _, input := range req.Channels {
			ch, err := s.visibleChannel(c, tx, input.DeviceCode, input.ChannelCode)
			if err != nil {
				return err
			}
			if err := tx.Create(&gbmodels.GbChannelFavoriteItem{GroupID: group.ID, DeviceCode: ch.DeviceID, ChannelCode: ch.ChannelID, DeviceName: s.deviceName(c, tx, ch), ChannelName: ch.Name}).Error; err != nil {
				return err
			}
		}
		dto = GroupDTO{ID: group.ID, Name: group.Name, AvailableCount: len(req.Channels), Items: make([]ItemDTO, 0, len(req.Channels))}
		for _, input := range req.Channels {
			ch, _ := s.visibleChannel(c, tx, input.DeviceCode, input.ChannelCode)
			dto.Items = append(dto.Items, ItemDTO{DeviceCode: ch.DeviceID, ChannelCode: ch.ChannelID, DeviceName: s.deviceName(c, tx, ch), ChannelName: ch.Name, Channel: channelDTO(ch)})
		}
		return nil
	})
	if err != nil && (strings.Contains(strings.ToLower(err.Error()), "unique") || strings.Contains(strings.ToLower(err.Error()), "duplicate")) {
		return GroupDTO{}, &DomainError{Code: ErrGroupNameConflict, Message: "收藏组名称已存在"}
	}
	return dto, err
}

func (s *Service) deviceName(c *gin.Context, db *gorm.DB, ch *gbmodels.GbChannel) string {
	var device gbmodels.GbDevice
	if result := db.WithContext(c).Select("name, alias").Where("device_id = ?", ch.DeviceID).Limit(1).Find(&device); result.Error == nil && result.RowsAffected > 0 {
		if strings.TrimSpace(device.Alias) != "" {
			return device.Alias
		}
		if strings.TrimSpace(device.Name) != "" {
			return device.Name
		}
	}
	return ch.DeviceID
}

func (s *Service) Append(c *gin.Context, actor, groupID uint, inputs []ChannelInput) (AppendResult, error) {
	result := AppendResult{RequestedCount: len(inputs)}
	if len(inputs) == 0 {
		return result, &DomainError{ErrChannelInvalid, "通道不能为空"}
	}
	return result, s.db.WithContext(c).Transaction(func(tx *gorm.DB) error {
		if _, err := s.group(c, tx, groupID, actor); err != nil {
			return err
		}
		resolved := make([]*gbmodels.GbChannel, 0, len(inputs))
		for _, input := range inputs {
			ch, err := s.visibleChannel(c, tx, input.DeviceCode, input.ChannelCode)
			if err != nil {
				return err
			}
			resolved = append(resolved, ch)
		}
		for _, ch := range resolved {
			created := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&gbmodels.GbChannelFavoriteItem{GroupID: groupID, DeviceCode: ch.DeviceID, ChannelCode: ch.ChannelID, DeviceName: s.deviceName(c, tx, ch), ChannelName: ch.Name})
			if created.Error != nil {
				return created.Error
			}
			if created.RowsAffected == 0 {
				result.SkippedCount++
			} else {
				result.AddedCount++
			}
		}
		return nil
	})
}

func (s *Service) Remove(c *gin.Context, actor, groupID uint, deviceCode, channelCode string) error {
	if _, err := s.group(c, s.db, groupID, actor); err != nil {
		return err
	}
	return s.db.WithContext(c).Where("group_id = ? AND device_code = ? AND channel_code = ?", groupID, deviceCode, channelCode).Delete(&gbmodels.GbChannelFavoriteItem{}).Error
}

func (s *Service) Delete(c *gin.Context, actor, groupID uint) error {
	return s.db.WithContext(c).Transaction(func(tx *gorm.DB) error {
		if _, err := s.group(c, tx, groupID, actor); err != nil {
			return err
		}
		if err := tx.Where("group_id = ?", groupID).Delete(&gbmodels.GbChannelFavoriteItem{}).Error; err != nil {
			return err
		}
		return tx.Delete(&gbmodels.GbChannelFavoriteGroup{}, groupID).Error
	})
}

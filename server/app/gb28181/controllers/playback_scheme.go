package controllers

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"uvplatform.cn/uvp-gb28181/app/controllers"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	basemodels "uvplatform.cn/uvp-gb28181/app/models"
)

const maxPlaybackSchemePageSize = 100

var validPlaybackLayouts = map[int]bool{1: true, 4: true, 6: true, 8: true, 9: true, 16: true}

type playbackSchemeError struct {
	code    string
	message string
	status  int
}

func (e *playbackSchemeError) Error() string { return e.message }

func schemeError(code, message string, status int) error {
	return &playbackSchemeError{code: code, message: message, status: status}
}

type playbackSchemeSlotInput struct {
	SlotIndex   int    `json:"slotIndex"`
	DeviceCode  string `json:"deviceCode"`
	ChannelCode string `json:"channelCode"`
}

type playbackSchemeLayoutInput struct {
	LayoutSize int                       `json:"layoutSize"`
	Slots      []playbackSchemeSlotInput `json:"slots"`
}

type PlaybackSchemeController struct {
	controllers.Common
	db func() *gorm.DB
}

func NewPlaybackSchemeController(provider ...func() *gorm.DB) *PlaybackSchemeController {
	db := func() *gorm.DB { return app.DB() }
	if len(provider) > 0 && provider[0] != nil {
		db = provider[0]
	}
	return &PlaybackSchemeController{db: db}
}

func (pc *PlaybackSchemeController) List(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("pageSize", "20"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > maxPlaybackSchemePageSize {
		pageSize = 20
	}
	owner := pc.GetCurrentUserID(c)
	query := pc.db().WithContext(c).Model(&gbmodels.GbPlaybackScheme{}).Where("owner_user_id = ?", owner)
	if keyword := strings.TrimSpace(c.Query("q")); keyword != "" {
		query = query.Where("name LIKE ?", "%"+keyword+"%")
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		pc.writeSchemeError(c, err)
		return
	}
	var schemes []gbmodels.GbPlaybackScheme
	if err := query.Order("updated_at DESC, id DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&schemes).Error; err != nil {
		pc.writeSchemeError(c, err)
		return
	}
	pc.Success(c, gin.H{"list": schemes, "total": total, "page": page, "pageSize": pageSize})
}

func (pc *PlaybackSchemeController) Detail(c *gin.Context) {
	scheme, ok := pc.ownedScheme(c, pc.db())
	if !ok {
		return
	}
	var stored []gbmodels.GbPlaybackSchemeSlot
	if err := pc.db().WithContext(c).Where("scheme_id = ?", scheme.ID).Order("slot_index ASC").Find(&stored).Error; err != nil {
		pc.writeSchemeError(c, err)
		return
	}
	slots := make([]gin.H, 0, len(stored))
	for _, slot := range stored {
		slots = append(slots, pc.resolveSlot(c, slot))
	}
	pc.Success(c, gin.H{
		"id": scheme.ID, "name": scheme.Name, "layoutSize": scheme.LayoutSize,
		"slotCount": scheme.SlotCount, "updatedAt": scheme.UpdatedAt, "slots": slots,
	})
}

func (pc *PlaybackSchemeController) Create(c *gin.Context) {
	var body struct {
		Name string `json:"name"`
		playbackSchemeLayoutInput
	}
	if c.ShouldBindJSON(&body) != nil {
		pc.writeSchemeError(c, schemeError("SCHEME_NAME_INVALID", "方案参数不合法", http.StatusBadRequest))
		return
	}
	name, err := validatePlaybackSchemeName(body.Name)
	if err != nil {
		pc.writeSchemeError(c, err)
		return
	}
	owner := pc.GetCurrentUserID(c)
	db := pc.db()
	if pc.schemeNameExists(c, db, owner, name, 0) {
		pc.writeSchemeError(c, schemeError("SCHEME_NAME_CONFLICT", "方案名称已存在", http.StatusConflict))
		return
	}
	slots, err := pc.buildSlots(c, db, body.LayoutSize, body.Slots)
	if err != nil {
		pc.writeSchemeError(c, err)
		return
	}
	var user basemodels.User
	if result := db.WithContext(c).Select("dept_id").Where("id = ?", owner).Limit(1).Find(&user); result.Error != nil || result.RowsAffected == 0 {
		pc.writeSchemeError(c, errors.New("当前用户不存在"))
		return
	}
	now := time.Now()
	scheme := gbmodels.GbPlaybackScheme{
		OwnerUserID: owner, OwnerDeptID: user.DeptID, Name: name, LayoutSize: int16(body.LayoutSize),
		SlotCount: len(slots), CreatedBy: owner, UpdatedBy: owner, CreatedAt: now, UpdatedAt: now,
	}
	err = db.WithContext(c).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&scheme).Error; err != nil {
			return err
		}
		for i := range slots {
			slots[i].SchemeID = scheme.ID
			slots[i].CreatedAt = now
		}
		return tx.Create(&slots).Error
	})
	if err != nil {
		if isPlaybackSchemeUniqueError(err) {
			err = schemeError("SCHEME_NAME_CONFLICT", "方案名称已存在", http.StatusConflict)
		}
		pc.writeSchemeError(c, err)
		return
	}
	pc.Success(c, scheme)
}

func (pc *PlaybackSchemeController) Rename(c *gin.Context) {
	scheme, ok := pc.ownedScheme(c, pc.db())
	if !ok {
		return
	}
	var body struct {
		Name string `json:"name"`
	}
	if c.ShouldBindJSON(&body) != nil {
		pc.writeSchemeError(c, schemeError("SCHEME_NAME_INVALID", "方案名称不合法", http.StatusBadRequest))
		return
	}
	name, err := validatePlaybackSchemeName(body.Name)
	if err != nil {
		pc.writeSchemeError(c, err)
		return
	}
	if pc.schemeNameExists(c, pc.db(), scheme.OwnerUserID, name, scheme.ID) {
		pc.writeSchemeError(c, schemeError("SCHEME_NAME_CONFLICT", "方案名称已存在", http.StatusConflict))
		return
	}
	updates := map[string]any{"name": name, "updated_by": pc.GetCurrentUserID(c), "updated_at": time.Now()}
	if err := pc.db().WithContext(c).Model(&gbmodels.GbPlaybackScheme{}).Where("id = ? AND owner_user_id = ?", scheme.ID, scheme.OwnerUserID).Updates(updates).Error; err != nil {
		pc.writeSchemeError(c, err)
		return
	}
	pc.Success(c, gin.H{"id": scheme.ID, "name": name})
}

func (pc *PlaybackSchemeController) ReplaceLayout(c *gin.Context) {
	scheme, ok := pc.ownedScheme(c, pc.db())
	if !ok {
		return
	}
	var body playbackSchemeLayoutInput
	if c.ShouldBindJSON(&body) != nil {
		pc.writeSchemeError(c, schemeError("SCHEME_LAYOUT_INVALID", "布局参数不合法", http.StatusBadRequest))
		return
	}
	slots, err := pc.buildSlots(c, pc.db(), body.LayoutSize, body.Slots)
	if err != nil {
		pc.writeSchemeError(c, err)
		return
	}
	actor := pc.GetCurrentUserID(c)
	now := time.Now()
	err = pc.db().WithContext(c).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&gbmodels.GbPlaybackScheme{}).
			Where("id = ? AND owner_user_id = ?", scheme.ID, actor).
			Updates(map[string]any{"layout_size": body.LayoutSize, "slot_count": len(slots), "updated_by": actor, "updated_at": now})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return schemeError("SCHEME_NOT_FOUND", "播放方案不存在", http.StatusNotFound)
		}
		if err := tx.Where("scheme_id = ?", scheme.ID).Delete(&gbmodels.GbPlaybackSchemeSlot{}).Error; err != nil {
			return err
		}
		for i := range slots {
			slots[i].SchemeID = scheme.ID
			slots[i].CreatedAt = now
		}
		return tx.Create(&slots).Error
	})
	if err != nil {
		pc.writeSchemeError(c, err)
		return
	}
	pc.Success(c, gin.H{"id": scheme.ID, "layoutSize": body.LayoutSize, "slotCount": len(slots)})
}

func (pc *PlaybackSchemeController) Delete(c *gin.Context) {
	scheme, ok := pc.ownedScheme(c, pc.db())
	if !ok {
		return
	}
	err := pc.db().WithContext(c).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("scheme_id = ?", scheme.ID).Delete(&gbmodels.GbPlaybackSchemeSlot{}).Error; err != nil {
			return err
		}
		return tx.Where("id = ? AND owner_user_id = ?", scheme.ID, scheme.OwnerUserID).Delete(&gbmodels.GbPlaybackScheme{}).Error
	})
	if err != nil {
		pc.writeSchemeError(c, err)
		return
	}
	pc.Success(c, gin.H{"id": scheme.ID})
}

func (pc *PlaybackSchemeController) buildSlots(c *gin.Context, db *gorm.DB, layoutSize int, input []playbackSchemeSlotInput) ([]gbmodels.GbPlaybackSchemeSlot, error) {
	if !validPlaybackLayouts[layoutSize] {
		return nil, schemeError("SCHEME_LAYOUT_INVALID", "不支持的分屏布局", http.StatusBadRequest)
	}
	if len(input) == 0 {
		return nil, schemeError("SCHEME_EMPTY", "当前画面没有可保存的通道", http.StatusBadRequest)
	}
	indices := make(map[int]struct{}, len(input))
	channels := make(map[string]struct{}, len(input))
	result := make([]gbmodels.GbPlaybackSchemeSlot, 0, len(input))
	for _, requested := range input {
		deviceCode := strings.TrimSpace(requested.DeviceCode)
		channelCode := strings.TrimSpace(requested.ChannelCode)
		if requested.SlotIndex < 0 || requested.SlotIndex >= layoutSize || deviceCode == "" || channelCode == "" {
			return nil, schemeError("SCHEME_LAYOUT_INVALID", "槽位参数超出当前布局", http.StatusBadRequest)
		}
		if _, exists := indices[requested.SlotIndex]; exists {
			return nil, schemeError("SCHEME_LAYOUT_INVALID", "槽位编号重复", http.StatusBadRequest)
		}
		key := deviceCode + "\x00" + channelCode
		if _, exists := channels[key]; exists {
			return nil, schemeError("SCHEME_CHANNEL_DUPLICATE", "同一通道不能重复保存", http.StatusBadRequest)
		}
		indices[requested.SlotIndex] = struct{}{}
		channels[key] = struct{}{}

		var channel gbmodels.GbChannel
		found := db.WithContext(c).Scopes(ownerDeptScope(c)).Where("device_id = ? AND channel_id = ?", deviceCode, channelCode).Limit(1).Find(&channel)
		if found.Error != nil {
			return nil, found.Error
		}
		if found.RowsAffected == 0 {
			return nil, schemeError("SCHEME_CHANNEL_NOT_VISIBLE", "通道不存在或无权访问", http.StatusBadRequest)
		}
		var device gbmodels.GbDevice
		db.WithContext(c).Scopes(ownerDeptScope(c)).Select("name", "alias").Where("device_id = ?", deviceCode).Limit(1).Find(&device)
		deviceName := strings.TrimSpace(device.Alias)
		if deviceName == "" {
			deviceName = strings.TrimSpace(device.Name)
		}
		if deviceName == "" {
			deviceName = deviceCode
		}
		channelName := strings.TrimSpace(channel.Alias)
		if channelName == "" {
			channelName = strings.TrimSpace(channel.Name)
		}
		if channelName == "" {
			channelName = channelCode
		}
		result = append(result, gbmodels.GbPlaybackSchemeSlot{
			SlotIndex: requested.SlotIndex, DeviceCode: deviceCode, ChannelCode: channelCode,
			DeviceNameSnapshot: deviceName, ChannelNameSnapshot: channelName,
		})
	}
	return result, nil
}

func (pc *PlaybackSchemeController) resolveSlot(c *gin.Context, slot gbmodels.GbPlaybackSchemeSlot) gin.H {
	response := gin.H{
		"id": slot.ID, "slotIndex": slot.SlotIndex, "deviceCode": slot.DeviceCode, "channelCode": slot.ChannelCode,
		"deviceName": slot.DeviceNameSnapshot, "channelName": slot.ChannelNameSnapshot,
		"availability": "missing", "channelRecordId": nil, "channelStatus": nil, "audioEnabled": false,
	}
	var existing gbmodels.GbChannel
	lookup := pc.db().WithContext(c).Where("device_id = ? AND channel_id = ?", slot.DeviceCode, slot.ChannelCode).Limit(1).Find(&existing)
	if lookup.Error != nil || lookup.RowsAffected == 0 {
		return response
	}
	var visible gbmodels.GbChannel
	visibleResult := pc.db().WithContext(c).Scopes(ownerDeptScope(c)).Where("id = ?", existing.ID).Limit(1).Find(&visible)
	if visibleResult.Error != nil || visibleResult.RowsAffected == 0 {
		response["availability"] = "forbidden"
		response["deviceName"] = ""
		response["channelName"] = "无权访问"
		return response
	}
	response["channelRecordId"] = visible.ID
	response["channelStatus"] = visible.Status
	response["audioEnabled"] = visible.AudioEnabled
	if visible.Status == gbmodels.ChannelStatusOnline {
		response["availability"] = "available"
	} else {
		response["availability"] = "offline"
	}
	return response
}

func (pc *PlaybackSchemeController) ownedScheme(c *gin.Context, db *gorm.DB) (*gbmodels.GbPlaybackScheme, bool) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		pc.writeSchemeError(c, schemeError("SCHEME_NOT_FOUND", "播放方案不存在", http.StatusNotFound))
		return nil, false
	}
	var scheme gbmodels.GbPlaybackScheme
	result := db.WithContext(c).Where("id = ? AND owner_user_id = ?", uint(id), pc.GetCurrentUserID(c)).Limit(1).Find(&scheme)
	if result.Error != nil {
		pc.writeSchemeError(c, result.Error)
		return nil, false
	}
	if result.RowsAffected == 0 {
		pc.writeSchemeError(c, schemeError("SCHEME_NOT_FOUND", "播放方案不存在", http.StatusNotFound))
		return nil, false
	}
	return &scheme, true
}

func (pc *PlaybackSchemeController) schemeNameExists(c *gin.Context, db *gorm.DB, owner uint, name string, exceptID uint) bool {
	query := db.WithContext(c).Model(&gbmodels.GbPlaybackScheme{}).Where("owner_user_id = ? AND name = ?", owner, name)
	if exceptID != 0 {
		query = query.Where("id <> ?", exceptID)
	}
	var count int64
	return query.Count(&count).Error == nil && count > 0
}

func validatePlaybackSchemeName(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	if name == "" || len([]rune(name)) > 64 {
		return "", schemeError("SCHEME_NAME_INVALID", "方案名称不能为空且不能超过 64 个字符", http.StatusBadRequest)
	}
	return name, nil
}

func isPlaybackSchemeUniqueError(err error) bool {
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "unique") || strings.Contains(text, "duplicate")
}

func (pc *PlaybackSchemeController) writeSchemeError(c *gin.Context, err error) {
	var domain *playbackSchemeError
	if errors.As(err, &domain) {
		pc.Fail(c, domain.message, err, domain.status, 1, gin.H{"errorCode": domain.code})
		return
	}
	pc.Fail(c, "播放方案操作失败", err, http.StatusInternalServerError)
}

package controllers

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"uvplatform.cn/uvp-gb28181/app/controllers"
	gbdirectory "uvplatform.cn/uvp-gb28181/app/gb28181/directory"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	basemodels "uvplatform.cn/uvp-gb28181/app/models"
)

type CustomGroupController struct {
	controllers.Common
	db func() *gorm.DB
}

func NewCustomGroupController(provider ...func() *gorm.DB) *CustomGroupController {
	db := func() *gorm.DB { return app.DB() }
	if len(provider) > 0 && provider[0] != nil {
		db = provider[0]
	}
	return &CustomGroupController{db: db}
}

func (cc *CustomGroupController) Create(c *gin.Context) {
	var body struct {
		Name     string `json:"name"`
		ParentID *uint  `json:"parentId"`
	}
	if c.ShouldBindJSON(&body) != nil {
		cc.writeError(c, gbdirectory.ErrGroupNameInvalid)
		return
	}
	db := cc.db()
	actor := cc.GetCurrentUserID(c)
	owner := uint(0)
	parent := uint(0)
	if body.ParentID != nil {
		parent = *body.ParentID
	}
	if parent != 0 {
		group, err := cc.visibleGroup(c, db, parent)
		if err != nil {
			cc.writeError(c, err)
			return
		}
		owner = group.OwnerDeptID
	} else {
		var user basemodels.User
		if db.Select("dept_id").Where("id = ?", actor).Limit(1).Find(&user).RowsAffected > 0 {
			owner = user.DeptID
		}
	}
	group, err := gbdirectory.NewCustomGroupService(db).Create(c, owner, actor, parent, body.Name)
	if err != nil {
		cc.writeError(c, err)
		return
	}
	cc.audit("创建自定义分组", actor, group.ID, zap.Uint("parentId", parent))
	cc.Success(c, group)
}

func (cc *CustomGroupController) Rename(c *gin.Context) {
	group, id, ok := cc.groupFromPath(c)
	if !ok {
		return
	}
	var body struct {
		Name string `json:"name"`
	}
	if c.ShouldBindJSON(&body) != nil {
		cc.writeError(c, gbdirectory.ErrGroupNameInvalid)
		return
	}
	if err := gbdirectory.NewCustomGroupService(cc.db()).Rename(c, group.OwnerDeptID, id, body.Name); err != nil {
		cc.writeError(c, err)
		return
	}
	cc.audit("重命名自定义分组", cc.GetCurrentUserID(c), id)
	cc.Success(c, gin.H{"id": id, "name": body.Name})
}

func (cc *CustomGroupController) Move(c *gin.Context) {
	group, id, ok := cc.groupFromPath(c)
	if !ok {
		return
	}
	var body struct {
		TargetParentID *uint `json:"targetParentId"`
	}
	if c.ShouldBindJSON(&body) != nil {
		cc.writeError(c, gbdirectory.ErrGroupNotFound)
		return
	}
	target := uint(0)
	if body.TargetParentID != nil {
		target = *body.TargetParentID
		if target != 0 {
			p, err := cc.visibleGroup(c, cc.db(), target)
			if err != nil || p.OwnerDeptID != group.OwnerDeptID {
				cc.writeError(c, gbdirectory.ErrGroupNotFound)
				return
			}
		}
	}
	if err := gbdirectory.NewCustomGroupService(cc.db()).Move(c, group.OwnerDeptID, id, target); err != nil {
		cc.writeError(c, err)
		return
	}
	cc.audit("移动自定义分组", cc.GetCurrentUserID(c), id, zap.Uint("targetParentId", target))
	cc.Success(c, gin.H{"id": id, "parentId": target})
}

func (cc *CustomGroupController) Delete(c *gin.Context) {
	group, id, ok := cc.groupFromPath(c)
	if !ok {
		return
	}
	result, err := gbdirectory.NewCustomGroupService(cc.db()).Delete(c, group.OwnerDeptID, id)
	if err != nil {
		cc.writeError(c, err)
		return
	}
	cc.audit("删除自定义分组", cc.GetCurrentUserID(c), id, zap.Int("removedDeviceCount", result.RemovedDeviceCount))
	cc.Success(c, result)
}

func (cc *CustomGroupController) AddDevices(c *gin.Context)    { cc.mutateDevices(c, false) }
func (cc *CustomGroupController) RemoveDevices(c *gin.Context) { cc.mutateDevices(c, true) }

func (cc *CustomGroupController) mutateDevices(c *gin.Context, remove bool) {
	group, id, ok := cc.groupFromPath(c)
	if !ok {
		return
	}
	var body struct {
		DeviceIDs []uint `json:"deviceIds"`
	}
	if c.ShouldBindJSON(&body) != nil {
		cc.writeError(c, gbdirectory.ErrDeviceBatchInvalid)
		return
	}
	svc := gbdirectory.NewCustomMemberService(cc.db())
	requested := len(body.DeviceIDs)
	if remove {
		if err := svc.Remove(c, group.OwnerDeptID, id, body.DeviceIDs); err != nil {
			cc.writeError(c, err)
			return
		}
		cc.audit("从自定义分组移除设备", cc.GetCurrentUserID(c), id, zap.Int("requested", requested))
		cc.Success(c, gin.H{"requestedCount": requested, "removedCount": requested, "skippedCount": 0})
		return
	}
	result, err := svc.Add(c, group.OwnerDeptID, cc.GetCurrentUserID(c), id, body.DeviceIDs)
	if err != nil {
		cc.writeError(c, err)
		return
	}
	cc.audit("添加设备到自定义分组", cc.GetCurrentUserID(c), id, zap.Int("added", result.Added))
	cc.Success(c, gin.H{"requestedCount": requested, "addedCount": result.Added, "skippedCount": result.Skipped})
}

func (cc *CustomGroupController) groupFromPath(c *gin.Context) (*gbmodels.GbCustomGroup, uint, bool) {
	id64, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id64 == 0 {
		cc.writeError(c, gbdirectory.ErrGroupNotFound)
		return nil, 0, false
	}
	group, err := cc.visibleGroup(c, cc.db(), uint(id64))
	if err != nil {
		cc.writeError(c, err)
		return nil, 0, false
	}
	return group, uint(id64), true
}

func (cc *CustomGroupController) visibleGroup(c *gin.Context, db *gorm.DB, id uint) (*gbmodels.GbCustomGroup, error) {
	var group gbmodels.GbCustomGroup
	result := db.WithContext(c).Scopes(ownerDeptScope(c)).Where("id = ?", id).Limit(1).Find(&group)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, gbdirectory.ErrGroupNotFound
	}
	return &group, nil
}

func (cc *CustomGroupController) writeError(c *gin.Context, err error) {
	status := http.StatusBadRequest
	var domain *gbdirectory.DomainError
	if errors.As(err, &domain) {
		switch domain.Code {
		case "GROUP_NOT_FOUND", "DEVICE_NOT_FOUND":
			status = http.StatusNotFound
		case "GROUP_NAME_CONFLICT", "GROUP_CYCLE", "GROUP_HAS_CHILDREN":
			status = http.StatusConflict
		}
		cc.Fail(c, domain.Message, err, status, 1, gin.H{"errorCode": domain.Code, "details": domain.Details})
		return
	}
	cc.Fail(c, "自定义分组操作失败", err, http.StatusInternalServerError)
}

func (cc *CustomGroupController) audit(message string, actor, groupID uint, fields ...zap.Field) {
	fields = append(fields, zap.Uint("actorId", actor), zap.Uint("groupId", groupID))
	app.ZapLog.Info(message, fields...)
}

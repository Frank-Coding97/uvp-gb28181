package controllers

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"gorm.io/gorm"
	"uvplatform.com/uvp-gb28181/app/global/app"
	"uvplatform.com/uvp-gb28181/app/models"
	"uvplatform.com/uvp-gb28181/app/utils/common"
	"uvplatform.com/uvp-gb28181/app/utils/passwordhelper"
)

func (uc *UserController) ChangeInitialPassword(c *gin.Context) {
	var req struct {
		Password        string `json:"password"`
		ConfirmPassword string `json:"confirmPassword"`
	}
	if err := c.ShouldBindBodyWith(&req, binding.JSON); err != nil || req.Password == "" || req.Password != req.ConfirmPassword {
		uc.FailAndAbort(c, "请输入新密码，并确保两次输入一致", nil)
		return
	}
	var user models.User
	if err := app.DBContext(c.Request.Context()).First(&user, common.GetCurrentUserID(c)).Error; err != nil {
		uc.FailAndAbort(c, "读取用户失败", nil, http.StatusServiceUnavailable)
		return
	}
	if !user.MustChangePassword {
		uc.FailAndAbort(c, "初始密码已修改，请重新登录", nil)
		return
	}
	if passwordhelper.ComparePassword(user.Password, req.Password) == nil {
		uc.FailAndAbort(c, "新密码不能与初始密码相同", nil)
		return
	}
	hash, err := passwordhelper.HashPassword(req.Password)
	if err != nil {
		uc.FailAndAbort(c, "密码加密失败", nil, http.StatusInternalServerError)
		return
	}
	err = app.DBContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&models.User{}).Where("id = ? AND must_change_password = ? AND password = ?", user.ID, true, user.Password).
			Updates(map[string]interface{}{"password": hash, "must_change_password": false})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return errors.New("initial password state changed")
		}
		return uc.revokeUserSessionsTx(tx, user.ID, "initial_password_changed")
	})
	if err != nil {
		uc.FailAndAbort(c, "修改初始密码失败，请重试", nil, http.StatusServiceUnavailable)
		return
	}
	uc.SuccessWithMessage(c, "密码修改成功，请重新登录")
}

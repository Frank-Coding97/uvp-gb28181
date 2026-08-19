package controllers

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"uvplatform.cn/uvp-gb28181/app/controllers"
	"uvplatform.cn/uvp-gb28181/app/gb28181/favorites"
	"uvplatform.cn/uvp-gb28181/app/global/app"
)

type ChannelFavoriteController struct {
	controllers.Common
	db func() *gorm.DB
}

func NewChannelFavoriteController(provider ...func() *gorm.DB) *ChannelFavoriteController {
	db := func() *gorm.DB { return app.DB() }
	if len(provider) > 0 && provider[0] != nil {
		db = provider[0]
	}
	return &ChannelFavoriteController{db: db}
}

func (cc *ChannelFavoriteController) List(c *gin.Context) {
	data, err := favorites.NewService(cc.db()).List(c, cc.GetCurrentUserID(c))
	if err != nil {
		cc.writeError(c, err)
		return
	}
	cc.Success(c, gin.H{"list": data})
}

func (cc *ChannelFavoriteController) Create(c *gin.Context) {
	var req favorites.CreateRequest
	if c.ShouldBindJSON(&req) != nil {
		cc.writeError(c, &favorites.DomainError{Code: favorites.ErrGroupNameInvalid, Message: "收藏组参数不合法"})
		return
	}
	data, err := favorites.NewService(cc.db()).Create(c, cc.GetCurrentUserID(c), req)
	if err != nil {
		cc.writeError(c, err)
		return
	}
	cc.Success(c, data)
}

func (cc *ChannelFavoriteController) Append(c *gin.Context) {
	id, ok := cc.id(c)
	if !ok {
		return
	}
	var body struct {
		Channels []favorites.ChannelInput `json:"channels"`
	}
	if c.ShouldBindJSON(&body) != nil {
		cc.writeError(c, &favorites.DomainError{Code: favorites.ErrChannelInvalid, Message: "通道参数不合法"})
		return
	}
	result, err := favorites.NewService(cc.db()).Append(c, cc.GetCurrentUserID(c), id, body.Channels)
	if err != nil {
		cc.writeError(c, err)
		return
	}
	cc.Success(c, result)
}

func (cc *ChannelFavoriteController) Remove(c *gin.Context) {
	id, ok := cc.id(c)
	if !ok {
		return
	}
	var body favorites.ChannelInput
	if c.ShouldBindJSON(&body) != nil {
		cc.writeError(c, &favorites.DomainError{Code: favorites.ErrChannelInvalid, Message: "通道参数不合法"})
		return
	}
	if err := favorites.NewService(cc.db()).Remove(c, cc.GetCurrentUserID(c), id, body.DeviceCode, body.ChannelCode); err != nil {
		cc.writeError(c, err)
		return
	}
	cc.Success(c, gin.H{"removed": true})
}

func (cc *ChannelFavoriteController) Delete(c *gin.Context) {
	id, ok := cc.id(c)
	if !ok {
		return
	}
	if err := favorites.NewService(cc.db()).Delete(c, cc.GetCurrentUserID(c), id); err != nil {
		cc.writeError(c, err)
		return
	}
	cc.Success(c, gin.H{"deleted": true})
}

func (cc *ChannelFavoriteController) id(c *gin.Context) (uint, bool) {
	value, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || value == 0 {
		cc.writeError(c, &favorites.DomainError{Code: favorites.ErrGroupNotFound, Message: "收藏组不存在"})
		return 0, false
	}
	return uint(value), true
}

func (cc *ChannelFavoriteController) writeError(c *gin.Context, err error) {
	status := http.StatusBadRequest
	var domain *favorites.DomainError
	if errors.As(err, &domain) {
		switch domain.Code {
		case favorites.ErrGroupNameConflict:
			status = http.StatusConflict
		case favorites.ErrGroupNotFound, favorites.ErrChannelNotVisible:
			status = http.StatusNotFound
		}
		cc.Fail(c, domain.Message, err, status, 1, gin.H{"errorCode": domain.Code})
		return
	}
	cc.Fail(c, "收藏操作失败", err, http.StatusInternalServerError)
}

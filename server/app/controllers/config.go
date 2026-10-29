package controllers

import (
	"fmt"
	"net/http"

	"gorm.io/gorm"

	gbconfig "uvplatform.com/uvp-gb28181/app/gb28181/config"
	"uvplatform.com/uvp-gb28181/app/global/app"
	"uvplatform.com/uvp-gb28181/app/logcleanup"
	"uvplatform.com/uvp-gb28181/app/models"

	"github.com/gin-gonic/gin"
)

// ConfigController 配置控制器
// @Summary 系统配置管理API
// @Description 系统配置管理相关接口
// @Tags 配置管理
// @Accept json
// @Produce json
// @Router /config [get]
type ConfigController struct {
	Common
}

// NewConfigController 创建配置控制器
func NewConfigController() *ConfigController {
	return &ConfigController{
		Common: Common{},
	}
}

// GetConfig 获取配置信息
// @Summary 获取配置信息
// @Description 获取系统配置信息
// @Tags 配置管理
// @Accept json
// @Produce json
// @Success 200 {object} map[string]interface{} "成功返回配置信息"
// @Failure 500 {object} map[string]interface{} "服务器内部错误"
// @Router /config/get [get]
func (con ConfigController) GetConfig(ctx *gin.Context) {
	// 提取Server、HttpServer和Captcha配置
	result := make(map[string]interface{})

	// 获取Server配置
	systemConfig := make(map[string]interface{})

	systemConfig["systemLogo"] = app.ConfigYml.GetString("system.systemlogo")           // 系统LOGO图片地址
	systemConfig["systemIcon"] = app.ConfigYml.GetString("system.systemicon")           // 系统图标地址
	systemConfig["systemName"] = app.ConfigYml.GetString("system.systemname")           // 系统名称
	systemConfig["systemCopyright"] = app.ConfigYml.GetString("system.systemcopyright") // 版权声明信息
	systemConfig["systemRecordNo"] = app.ConfigYml.GetString("system.systemrecordno")   // 网站备案号
	// 初始化引导只由数据库状态决定，完成后不再下发默认凭据。
	systemConfig["defaultusername"], systemConfig["defaultpassword"] = "", ""
	var initialAdmin models.User
	if err := app.DBContext(ctx.Request.Context()).Select("id", "must_change_password").Where("id = ?", 1).First(&initialAdmin).Error; err != nil && err != gorm.ErrRecordNotFound {
		con.FailAndAbort(ctx, "读取初始化状态失败", nil, http.StatusServiceUnavailable)
		return
	}
	if initialAdmin.MustChangePassword {
		systemConfig["defaultusername"] = app.ConfigYml.GetString("server.demoaccount.defaultusername")
		systemConfig["defaultpassword"] = app.ConfigYml.GetString("server.demoaccount.defaultpassword")
	}
	ctx.Header("Cache-Control", "no-store")
	result["system"] = systemConfig // 将系统配置放入结果集
	result["logCleanup"] = logcleanup.CurrentConfig()

	// 获取Safe配置
	safeConfig := make(map[string]interface{})
	safeConfig["loginLockThreshold"] = app.ConfigYml.GetInt("safe.loginlockthreshold")  // 密码错误锁定阈值，连续登录失败次数达到该值将锁定账号，0表示不锁定
	safeConfig["loginLockExpire"] = app.ConfigYml.GetInt("safe.loginlockexpire")        // 连续登录失败次数记录的缓存时间（单位：秒）
	safeConfig["loginLockDuration"] = app.ConfigYml.GetInt("safe.loginlockduration")    // 账号锁定时长（单位：秒）
	safeConfig["minPasswordLength"] = app.ConfigYml.GetInt("safe.minpasswordlength")    // 密码最小长度要求
	safeConfig["requireSpecialChar"] = app.ConfigYml.GetBool("safe.requirespecialchar") // 密码是否必须包含特殊字符
	result["safe"] = safeConfig                                                         // 将安全配置放入结果集

	// 获取Captcha配置
	captchaConfig := make(map[string]interface{})
	captchaConfig["open"] = app.ConfigYml.GetBool("captcha.open")    // 是否开启验证码功能
	captchaConfig["length"] = app.ConfigYml.GetInt("captcha.length") // 验证码字符长度
	result["captcha"] = captchaConfig                                // 将验证码配置放入结果集

	// 返回成功响应
	con.Success(ctx, result)
}

// UpdateConfig 更新配置信息
// @Summary 更新配置信息
// @Description 更新系统配置信息
// @Tags 配置管理
// @Accept json
// @Produce json
// @Param config body map[string]interface{} true "配置信息"
// @Success 200 {object} map[string]interface{} "成功更新配置信息"
// @Failure 400 {object} map[string]interface{} "请求参数错误"
// @Failure 500 {object} map[string]interface{} "服务器内部错误"
// @Router /config/update [put]
// @Security ApiKeyAuth
func (con ConfigController) UpdateConfig(ctx *gin.Context) {
	var req models.ConfigRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		con.FailAndAbort(ctx, "参数绑定失败: "+err.Error(), err)
		return
	}
	var requestedLogCleanup logcleanup.Config
	if req.LogCleanup != nil {
		var err error
		requestedLogCleanup, err = configFromRequest(req.LogCleanup)
		if err != nil {
			con.FailAndAbort(ctx, "日志清理配置无效: "+err.Error(), err)
			return
		}
		if err = logcleanup.ValidateConfig(requestedLogCleanup); err != nil {
			con.FailAndAbort(ctx, "日志清理配置无效: "+err.Error(), err)
			return
		}
	}
	if app.ConfigYml == nil {
		con.FailAndAbort(ctx, "配置服务尚未初始化", nil, http.StatusServiceUnavailable)
		return
	}

	// Keep this update atomic with respect to cleanup task snapshots. Capture all
	// values touched by this endpoint so a failed file save restores the in-memory
	// configuration as well.
	unlock := logcleanup.LockConfig()
	previousLogCleanup := logcleanup.CurrentConfigUnlocked()
	previousValues := make(map[string]interface{})
	setValues := make(map[string]interface{})
	if req.System != nil {
		setValues["system.systemlogo"] = req.System.SystemLogo
		setValues["system.systemicon"] = req.System.SystemIcon
		setValues["system.systemname"] = req.System.SystemName
		setValues["system.systemcopyright"] = req.System.SystemCopyright
		setValues["system.systemrecordno"] = req.System.SystemRecordNo
		setValues["server.demoaccount.defaultusername"] = req.System.DefaultUsername
		setValues["server.demoaccount.defaultpassword"] = req.System.DefaultPassword
	}
	if req.Safe != nil {
		setValues["safe.loginlockthreshold"] = req.Safe.LoginLockThreshold
		setValues["safe.loginlockexpire"] = req.Safe.LoginLockExpire
		setValues["safe.loginlockduration"] = req.Safe.LoginLockDuration
		setValues["safe.minpasswordlength"] = req.Safe.MinPasswordLength
		setValues["safe.requirespecialchar"] = req.Safe.RequireSpecialChar
	}
	if req.Captcha != nil {
		setValues["captcha.open"] = req.Captcha.Open
		setValues["captcha.length"] = req.Captcha.Length
	}
	if req.LogCleanup != nil {
		setValues[gbconfig.SIPTraceRetentionDaysConfigKey] = requestedLogCleanup.SIPRetentionDays
		setValues[logcleanup.OperationRetentionDaysConfigKey] = requestedLogCleanup.OperationRetentionDays
		setValues[logcleanup.LoginRetentionDaysConfigKey] = requestedLogCleanup.LoginRetentionDays
		setValues[logcleanup.JobRetentionDaysConfigKey] = requestedLogCleanup.JobRetentionDays
		setValues[logcleanup.PlaybackRetentionDaysConfigKey] = requestedLogCleanup.PlaybackRetentionDays
		setValues[logcleanup.SchedulerRetentionDaysConfigKey] = requestedLogCleanup.SchedulerRetentionDays
		setValues[logcleanup.ConfiguredConfigKey] = true
	}
	for key := range setValues {
		previousValues[key] = app.ConfigYml.Get(key)
	}
	for key, value := range setValues {
		app.ConfigYml.Set(key, value)
	}
	saveErr := app.ConfigYml.SaveConfig()
	if saveErr != nil {
		for key, value := range previousValues {
			app.ConfigYml.Set(key, value)
		}
	}
	sipRetentionChanged := saveErr == nil && req.LogCleanup != nil &&
		requestedLogCleanup.SIPRetentionDays != previousLogCleanup.SIPRetentionDays
	unlock()
	if saveErr != nil {
		con.FailAndAbort(ctx, "保存配置文件失败", saveErr)
		return
	}
	if sipRetentionChanged {
		if err := logcleanup.ApplySIPRetention(); err != nil {
			con.FailAndAbort(ctx, "配置已保存，但 SIP 保留期尚未应用到运行中的服务", err, http.StatusServiceUnavailable)
			return
		}
	}

	// 返回成功响应
	con.SuccessWithMessage(ctx, "配置更新成功")
}

func configFromRequest(req *models.LogCleanupConfig) (logcleanup.Config, error) {
	if req.SIPRetentionDays == nil || req.OperationRetentionDays == nil || req.LoginRetentionDays == nil ||
		req.JobRetentionDays == nil || req.PlaybackRetentionDays == nil || req.SchedulerRetentionDays == nil {
		return logcleanup.Config{}, fmt.Errorf("logCleanup 必须包含全部六类保留天数字段")
	}
	return logcleanup.Config{
		SIPRetentionDays:       *req.SIPRetentionDays,
		OperationRetentionDays: *req.OperationRetentionDays,
		LoginRetentionDays:     *req.LoginRetentionDays,
		JobRetentionDays:       *req.JobRetentionDays,
		PlaybackRetentionDays:  *req.PlaybackRetentionDays,
		SchedulerRetentionDays: *req.SchedulerRetentionDays,
	}, nil
}

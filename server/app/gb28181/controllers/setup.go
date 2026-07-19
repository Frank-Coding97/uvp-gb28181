package controllers

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"uvplatform.cn/uvp-gb28181/app/controllers"
	gbsetup "uvplatform.cn/uvp-gb28181/app/gb28181/setup"
	"uvplatform.cn/uvp-gb28181/app/global/app"
)

type UpdateSIPConfigRequest struct {
	DeploymentMode      gbsetup.DeploymentMode `json:"deploymentMode"`
	ListenIP            string                 `json:"listenIp"`
	AdvertiseIP         string                 `json:"advertiseIp"`
	AdvertiseIPInferred bool                   `json:"advertiseIpInferred"`
	Port                int                    `json:"port"`
	Domain              string                 `json:"domain"`
	ServerID            string                 `json:"serverId"`
	Password            *string                `json:"password"`
}

type SetupStatusResponse struct {
	OnboardingStatus  gbsetup.OnboardingStatus `json:"onboardingStatus"`
	OnboardingVersion int                      `json:"onboardingVersion"`
	ConfigStatus      string                   `json:"configStatus"`
	Config            *gbsetup.SIPConfigView   `json:"config,omitempty"`
	Runtime           gbsetup.RuntimeSnapshot  `json:"runtime"`
	CanConfigure      bool                     `json:"canConfigure"`
	RestartRequired   bool                     `json:"restartRequired"`
}

type SetupController struct {
	controllers.Common
	db         *gorm.DB
	runtime    *gbsetup.RuntimeStatus
	interfaces gbsetup.InterfaceProvider
}

func NewSetupController(db *gorm.DB, runtime *gbsetup.RuntimeStatus, interfaces gbsetup.InterfaceProvider) *SetupController {
	if runtime == nil {
		runtime = gbsetup.NewRuntimeStatus()
	}
	return &SetupController{db: db, runtime: runtime, interfaces: interfaces}
}

// Status GET /api/gb28181/sip/setup/status
func (sc *SetupController) Status(c *gin.Context) {
	state, err := gbsetup.NewInstallationService(sc.db).Current(c.Request.Context())
	if err != nil {
		sc.Fail(c, "读取 SIP 安装状态失败", err, http.StatusInternalServerError)
		return
	}
	config, err := gbsetup.NewSIPConfigService(sc.db).Get(c.Request.Context())
	if err != nil {
		sc.Fail(c, "读取 SIP 配置失败", err, http.StatusInternalServerError)
		return
	}
	configStatus := "unconfigured"
	if config != nil {
		configStatus = "configured"
	}
	runtime := sc.runtime.Snapshot()
	sc.Success(c, SetupStatusResponse{
		OnboardingStatus:  state.Status,
		OnboardingVersion: state.OnboardingVersion,
		ConfigStatus:      configStatus,
		Config:            config,
		Runtime:           runtime,
		CanConfigure:      true,
		RestartRequired:   runtime.State == gbsetup.RuntimeRestartRequired,
	})
}

// SaveConfig PUT /api/gb28181/sip/setup/config
func (sc *SetupController) SaveConfig(c *gin.Context) {
	var request UpdateSIPConfigRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		sc.Fail(c, "SIP 配置请求格式错误", err, http.StatusBadRequest)
		return
	}
	view, err := gbsetup.NewSIPConfigService(sc.db).Save(c.Request.Context(), gbsetup.SaveSIPConfigRequest{
		DeploymentMode:      request.DeploymentMode,
		ListenIP:            request.ListenIP,
		AdvertiseIP:         request.AdvertiseIP,
		AdvertiseIPInferred: request.AdvertiseIPInferred,
		Port:                request.Port,
		Domain:              request.Domain,
		ServerID:            request.ServerID,
		Password:            request.Password,
	})
	if err != nil {
		var validation *gbsetup.ValidationError
		if errors.As(err, &validation) {
			sc.Fail(c, "SIP 配置校验失败", err, http.StatusBadRequest, 1, gin.H{"fields": validation.Fields})
			return
		}
		sc.Fail(c, "保存 SIP 配置失败", err, http.StatusInternalServerError)
		return
	}
	sc.runtime.MarkConfigSaved()
	app.ZapLog.Info("SIP 配置已保存",
		zap.Uint("operatorId", sc.GetCurrentUserID(c)),
		zap.String("deploymentMode", string(view.DeploymentMode)),
		zap.String("listenIp", view.ListenIP),
		zap.String("advertiseIp", view.AdvertiseIP),
		zap.Int("port", view.Port),
		zap.String("serverId", view.ServerID))
	sc.Success(c, gin.H{
		"config":          view,
		"restartRequired": true,
		"runtime":         sc.runtime.Snapshot(),
	})
}

// Skip POST /api/gb28181/sip/setup/skip
func (sc *SetupController) Skip(c *gin.Context) {
	service := gbsetup.NewInstallationService(sc.db)
	err := service.Skip(c.Request.Context())
	if errors.Is(err, gbsetup.ErrInvalidOnboardingTransition) {
		state, currentErr := service.Current(c.Request.Context())
		if currentErr == nil && (state.Status == gbsetup.OnboardingCompleted || state.Status == gbsetup.OnboardingSkipped) {
			sc.Success(c, gin.H{"onboardingStatus": state.Status})
			return
		}
	}
	if err != nil {
		sc.Fail(c, "暂缓 SIP 配置失败", err, http.StatusInternalServerError)
		return
	}
	state, err := service.Current(c.Request.Context())
	if err != nil {
		sc.Fail(c, "读取 SIP 安装状态失败", err, http.StatusInternalServerError)
		return
	}
	app.ZapLog.Info("SIP 首次安装引导已暂缓",
		zap.Uint("operatorId", sc.GetCurrentUserID(c)),
		zap.String("onboardingStatus", string(state.Status)))
	sc.Success(c, gin.H{"onboardingStatus": state.Status})
}

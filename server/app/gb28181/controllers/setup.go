package controllers

import (
	"context"
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

// SetupStatusResponse 精简后的 SIP 引导状态响应.
// 前端只依赖 Runtime.State 决定弹窗,不再有 onboardingStatus 四态.
// configStatus 便于前端在"未配置" vs "已配置但未运行" 场景做不同 UI 提示.
type SetupStatusResponse struct {
	ConfigStatus string                  `json:"configStatus"`
	Config       *gbsetup.SIPConfigView  `json:"config,omitempty"`
	Runtime      gbsetup.RuntimeSnapshot `json:"runtime"`
}

type NetworkInterfacesResponse struct {
	Items      []gbsetup.NetworkAddress `json:"items"`
	ScanStatus string                   `json:"scanStatus"`
	Warning    string                   `json:"warning,omitempty"`
}

// SIPReloader 抽象出保存后的热启动动作,由 bootstrap 层实现并注入,避免 controller → bootstrap 反向依赖.
type SIPReloader func() error

// SIPConfigSaver 抽象出 SIP 配置落库动作,由 bootstrap 层在首装事务中注入.
type SIPConfigSaver func(context.Context, gbsetup.SaveSIPConfigRequest) (gbsetup.SIPConfigView, error)

type SetupController struct {
	controllers.Common
	db          *gorm.DB
	runtime     *gbsetup.RuntimeStatus
	interfaces  gbsetup.InterfaceProvider
	reload      SIPReloader
	configSaver SIPConfigSaver
}

func NewSetupController(db *gorm.DB, runtime *gbsetup.RuntimeStatus, interfaces gbsetup.InterfaceProvider, reload SIPReloader) *SetupController {
	if runtime == nil {
		runtime = gbsetup.NewRuntimeStatus()
	}
	return &SetupController{db: db, runtime: runtime, interfaces: interfaces, reload: reload}
}

// SetConfigSaver configures the optional SIP configuration persistence hook.
// It must be called before the controller is registered with the router.
func (sc *SetupController) SetConfigSaver(saver SIPConfigSaver) {
	sc.configSaver = saver
}

// Status GET /api/gb28181/sip/setup/status
// 只返回配置存在性 + runtime 快照,不再有 onboarding 状态机.
// 前端根据 runtime.state === "unconfigured" 决定是否弹引导框.
func (sc *SetupController) Status(c *gin.Context) {
	config, err := gbsetup.NewSIPConfigService(sc.db).Get(c.Request.Context())
	if err != nil {
		sc.Fail(c, "读取 SIP 配置失败", err, http.StatusInternalServerError)
		return
	}
	configStatus := "unconfigured"
	if config != nil {
		configStatus = "configured"
	}
	sc.Success(c, SetupStatusResponse{
		ConfigStatus: configStatus,
		Config:       config,
		Runtime:      sc.runtime.Snapshot(),
	})
}

// SaveConfig PUT /api/gb28181/sip/setup/config
func (sc *SetupController) SaveConfig(c *gin.Context) {
	var request UpdateSIPConfigRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		sc.Fail(c, "SIP 配置请求格式错误", err, http.StatusBadRequest)
		return
	}
	requestToSave := gbsetup.SaveSIPConfigRequest{
		DeploymentMode:      request.DeploymentMode,
		ListenIP:            request.ListenIP,
		AdvertiseIP:         request.AdvertiseIP,
		AdvertiseIPInferred: request.AdvertiseIPInferred,
		Port:                request.Port,
		Domain:              request.Domain,
		ServerID:            request.ServerID,
		Password:            request.Password,
	}
	var view gbsetup.SIPConfigView
	var err error
	if sc.configSaver != nil {
		view, err = sc.configSaver(c.Request.Context(), requestToSave)
	} else {
		view, err = gbsetup.NewSIPConfigService(sc.db).Save(c.Request.Context(), requestToSave)
	}
	if err != nil {
		var validation *gbsetup.ValidationError
		if errors.As(err, &validation) {
			sc.Fail(c, "SIP 配置校验失败", err, http.StatusBadRequest, 1, gin.H{"fields": validation.Fields})
			return
		}
		sc.Fail(c, "保存 SIP 配置失败", err, http.StatusInternalServerError)
		return
	}
	app.ZapLog.Info("SIP 配置已保存",
		zap.Uint("operatorId", sc.GetCurrentUserID(c)),
		zap.String("deploymentMode", string(view.DeploymentMode)),
		zap.String("listenIp", view.ListenIP),
		zap.String("advertiseIp", view.AdvertiseIP),
		zap.Int("port", view.Port),
		zap.String("serverId", view.ServerID))

	// 保存后立即热启动 SIP 服务,让用户免于重启进程.
	// 热启动失败时 runtime state 已经被 Reloader 内部置为 failed,原因通过 snapshot 返回给前端.
	reloadOK := true
	reloadErrText := ""
	if sc.reload != nil {
		if err := sc.reload(); err != nil {
			reloadOK = false
			reloadErrText = err.Error()
			app.ZapLog.Error("SIP 保存后热启动失败", zap.Error(err))
		}
	}

	sc.Success(c, gin.H{
		"config":      view,
		"reloadedOk":  reloadOK,
		"reloadError": reloadErrText,
		"runtime":     sc.runtime.Snapshot(),
	})
}

// Skip POST /api/gb28181/sip/setup/skip
// 新架构下 skip 是前端"暂缓弹窗"信号 —— 后端不保存 skip 状态(避免引入独立状态机).
// 前端用 sessionStorage 记住"本次登录已跳过",下次登录会再次弹出提示.
// 该端点保留,只是为了记录审计日志,让运维知道用户暂缓过引导.
func (sc *SetupController) Skip(c *gin.Context) {
	app.ZapLog.Info("SIP 首次安装引导已暂缓(前端行为,后端不持久化)",
		zap.Uint("operatorId", sc.GetCurrentUserID(c)))
	sc.Success(c, gin.H{"acknowledged": true})
}

// NetworkInterfaces GET /api/gb28181/sip/setup/network-interfaces
func (sc *SetupController) NetworkInterfaces(c *gin.Context) {
	items, err := gbsetup.EnumerateNetworkAddresses(sc.interfaces)
	response := NetworkInterfacesResponse{Items: items, ScanStatus: "ok"}
	if err != nil {
		response.ScanStatus = "failed"
		response.Warning = "本机网络接口读取失败，请手动填写接入地址"
	} else if len(items) == 1 {
		response.Warning = "未发现可用的本机 IPv4 地址，请手动填写接入地址"
	}
	sc.Success(c, response)
}

package controllers

import (
	"errors"
	"net/http"
	"uvplatform.com/uvp-gb28181/app/utils/logging"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"uvplatform.com/uvp-gb28181/app/controllers"
	"uvplatform.com/uvp-gb28181/app/gb28181/netif"
	gbsetup "uvplatform.com/uvp-gb28181/app/gb28181/setup"
	"uvplatform.com/uvp-gb28181/app/global/app"
)

type UpdateSIPConfigRequest struct {
	DeploymentMode      gbsetup.DeploymentMode `json:"deploymentMode"`
	ListenIP            string                 `json:"listenIp"`
	AdvertiseIP         string                 `json:"advertiseIp"`
	AdvertiseIPInferred bool                   `json:"advertiseIpInferred"`
	HookIP              string                 `json:"hookIp"`
	SDPIP               string                 `json:"sdpIp"`
	StreamIP            string                 `json:"streamIp"`
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
	// DefaultSIPPort 是**部署层**给的引导页默认 SIP 端口（段基址 + 64）。
	// 为 0 表示部署层没告知（非容器/绿色包等形态），前端退回自己的构建期默认值。
	// ⛔ 只在 config 为空时有意义：已有配置时前端应该显示数据库里的端口，不是默认值。
	DefaultSIPPort int `json:"defaultSipPort,omitempty"`
}

type NetworkInterfacesResponse struct {
	Items      []gbsetup.NetworkAddress `json:"items"`
	ScanStatus string                   `json:"scanStatus"`
	Warning    string                   `json:"warning,omitempty"`
}

// SIPReloader 抽象出保存后的热启动动作,由 bootstrap 层实现并注入,避免 controller → bootstrap 反向依赖.
type SIPReloader func() error

type SetupController struct {
	controllers.Common
	db                    *gorm.DB
	runtime               *gbsetup.RuntimeStatus
	interfaces            gbsetup.InterfaceProvider
	reload                SIPReloader
	mediaDefaultsReloader SIPReloader
}

func NewSetupController(db *gorm.DB, runtime *gbsetup.RuntimeStatus, interfaces gbsetup.InterfaceProvider, reload SIPReloader) *SetupController {
	if runtime == nil {
		runtime = gbsetup.NewRuntimeStatus()
	}
	return &SetupController{db: db, runtime: runtime, interfaces: interfaces, reload: reload}
}

// SetPlatformMediaDefaultsReloader injects the independent ZLM media-defaults apply step.
func (sc *SetupController) SetPlatformMediaDefaultsReloader(reload SIPReloader) {
	sc.mediaDefaultsReloader = reload
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
		ConfigStatus:   configStatus,
		Config:         config,
		Runtime:        sc.runtime.Snapshot(),
		DefaultSIPPort: gbsetup.DeploymentDefaultSIPPort(),
	})
}

// SaveConfig PUT /api/gb28181/sip/setup/config
func (sc *SetupController) SaveConfig(c *gin.Context) {
	var request UpdateSIPConfigRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		sc.Fail(c, "SIP 配置请求格式错误", err, http.StatusBadRequest)
		return
	}
	// 保存时要把「当前网卡能否作为媒体地址兜底」算进来，所以复用同一份网卡 provider。
	view, err := gbsetup.NewSIPConfigService(sc.db).WithInterfaces(sc.interfaces).Save(c.Request.Context(), gbsetup.SaveSIPConfigRequest{
		DeploymentMode:      request.DeploymentMode,
		ListenIP:            request.ListenIP,
		AdvertiseIP:         request.AdvertiseIP,
		AdvertiseIPInferred: request.AdvertiseIPInferred,
		HookIP:              request.HookIP,
		SDPIP:               request.SDPIP,
		StreamIP:            request.StreamIP,
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
	app.Log(c.Request.Context()).Info("SIP 配置已保存", zap.String("event", "setup.config_saved"),
		zap.Uint("operator_id", sc.GetCurrentUserID(c)),
		zap.String("deployment_mode", string(view.DeploymentMode)),
		zap.String("listen_ip", view.ListenIP),
		zap.String("advertise_ip", view.AdvertiseIP),
		zap.Int("port", view.Port),
		zap.String("server_id", view.ServerID))

	// SIP 监听重载与媒体节点默认值收敛是两项独立操作，分别报告结果，
	// 避免媒体节点配置冲突被展示成 SIP 监听启动失败。
	reloadOK := true
	reloadErrText := ""
	if sc.reload != nil {
		if err := sc.reload(); err != nil {
			reloadOK = false
			reloadErrText = err.Error()
			app.Log(c.Request.Context()).Warn("SIP 保存后热启动失败", zap.String("event", "setup.sip_reload_failed"),
				zap.String("server_id", view.ServerID),
				logging.Error(err))
		}
	}
	response := gin.H{
		"config":      view,
		"reloadedOk":  reloadOK,
		"reloadError": reloadErrText,
		"runtime":     sc.runtime.Snapshot(),
	}
	if sc.mediaDefaultsReloader != nil {
		mediaDefaultsApplied := true
		mediaDefaultsErrText := ""
		if err := sc.mediaDefaultsReloader(); err != nil {
			mediaDefaultsApplied = false
			mediaDefaultsErrText = err.Error()
			app.Log(c.Request.Context()).Warn("SIP 配置已保存,但平台媒体默认值收敛失败",
				zap.String("event", "setup.media_defaults_reload_failed"),
				zap.String("server_id", view.ServerID),
				logging.Error(err))
		}
		response["mediaDefaultsApplied"] = mediaDefaultsApplied
		response["mediaDefaultsError"] = mediaDefaultsErrText
	}

	sc.Success(c, response)
}

// Skip POST /api/gb28181/sip/setup/skip
// 新架构下 skip 是前端"暂缓弹窗"信号 —— 后端不保存 skip 状态(避免引入独立状态机).
// 前端用 sessionStorage 记住"本次登录已跳过",下次登录会再次弹出提示.
// 该端点保留,只是为了记录审计日志,让运维知道用户暂缓过引导.
func (sc *SetupController) Skip(c *gin.Context) {
	app.Log(c.Request.Context()).Info("SIP 首次安装引导已暂缓(前端行为,后端不持久化)", zap.String("event", "setup.install_guide_skipped"),
		zap.Uint("operator_id", sc.GetCurrentUserID(c)))
	sc.Success(c, gin.H{"acknowledged": true})
}

// NetworkInterfaces GET /api/gb28181/sip/setup/network-interfaces
func (sc *SetupController) NetworkInterfaces(c *gin.Context) {
	items, err := gbsetup.EnumerateNetworkAddresses(sc.interfaces)
	response := NetworkInterfacesResponse{Items: items, ScanStatus: "ok"}
	switch {
	case err != nil:
		response.ScanStatus = "failed"
		response.Warning = "本机网络接口读取失败，请手动填写接入地址"
	case len(items) == 1:
		response.Warning = "未发现可用的本机 IPv4 地址，请手动填写接入地址"
	default:
		// 容器（Docker 默认 bridge 网络）里只有容器自己的 eth0，一张虚拟网卡都看不见，
		// 向导会把容器内地址当推荐地址推荐出去 —— 设备根本路由不到，只是单独提示。
		response.Warning = gbsetup.ContainerAddressWarning(items, netif.InContainer())
	}
	sc.Success(c, response)
}

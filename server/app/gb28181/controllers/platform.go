package controllers

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"uvplatform.cn/uvp-gb28181/app/controllers"
	gbsetup "uvplatform.cn/uvp-gb28181/app/gb28181/setup"
	globalapp "uvplatform.cn/uvp-gb28181/app/global/app"
)

// PlatformInfo 是本级 GB28181 平台对外接入参数。
type PlatformInfo struct {
	Version         string                  `json:"version"`
	Enabled         bool                    `json:"enabled"`
	ServerID        string                  `json:"serverId"`
	Domain          string                  `json:"domain"`
	SIPIP           string                  `json:"sipIp"`
	SIPIPs          []string                `json:"sipIps"`
	SIPPort         int                     `json:"sipPort"`
	Transport       []string                `json:"transport"`
	PasswordMasked  string                  `json:"passwordMasked"`
	RegisterURI     string                  `json:"registerUri"`
	ListenIP        string                  `json:"listenIp"`
	AdvertiseIP     string                  `json:"advertiseIp"`
	DeploymentMode  gbsetup.DeploymentMode  `json:"deploymentMode,omitempty"`
	ConfigStatus    string                  `json:"configStatus"`
	Runtime         gbsetup.RuntimeSnapshot `json:"runtime"`
	RestartRequired bool                    `json:"restartRequired"`
}

// PlatformController 提供本级平台只读接入信息。
type PlatformController struct {
	controllers.Common
	db              *gorm.DB
	runtime         *gbsetup.RuntimeStatus
	enabled         bool
	transport       []string
	networkProvider gbsetup.InterfaceProvider
}

func NewPlatformController() *PlatformController {
	return &PlatformController{runtime: gbsetup.NewRuntimeStatus()}
}

func NewConfiguredPlatformController(db *gorm.DB, runtime *gbsetup.RuntimeStatus, enabled bool, transport []string) *PlatformController {
	if runtime == nil {
		runtime = gbsetup.NewRuntimeStatus()
	}
	return &PlatformController{db: db, runtime: runtime, enabled: enabled, transport: transport}
}

// Info GET /api/gb28181/sip/platform
func (pc *PlatformController) Info(c *gin.Context) {
	if pc.db == nil {
		pc.Fail(c, "SIP 平台信息服务尚未装配", nil, http.StatusServiceUnavailable)
		return
	}
	config, err := gbsetup.NewSIPConfigService(pc.db).Get(c.Request.Context())
	if err != nil {
		pc.Fail(c, "读取 SIP 平台信息失败", err, http.StatusInternalServerError)
		return
	}
	runtime := pc.runtime.Snapshot()
	if config == nil {
		pc.Success(c, PlatformInfo{
			Version: globalapp.AppVersion.Version, Enabled: pc.enabled, Transport: pc.transport, ConfigStatus: "unconfigured",
			Runtime: runtime, RestartRequired: runtime.State == gbsetup.RuntimeRestartRequired,
		})
		return
	}
	addresses, _ := gbsetup.ActiveSIPAddresses(*config, pc.networkProvider)
	sipIP := ""
	if len(addresses) > 0 {
		sipIP = addresses[0]
	}
	pc.Success(c, PlatformInfo{
		Version: globalapp.AppVersion.Version, Enabled: pc.enabled, ServerID: config.ServerID, Domain: config.Domain,
		SIPIP: sipIP, SIPIPs: addresses, SIPPort: config.Port, Transport: pc.transport,
		PasswordMasked: maskStoredPassword(config.HasPassword),
		RegisterURI:    registerURI(config.ServerID, config.Domain, sipIP, config.Port),
		ListenIP:       config.ListenIP, AdvertiseIP: config.AdvertiseIP,
		DeploymentMode: config.DeploymentMode, ConfigStatus: "configured",
		Runtime: runtime, RestartRequired: runtime.State == gbsetup.RuntimeRestartRequired,
	})
}

func registerURI(serverID, domain, ip string, port int) string {
	if serverID == "" || domain == "" || ip == "" || port <= 0 {
		return ""
	}
	return "sip:" + serverID + "@" + ip + ":" + strconv.Itoa(port)
}

func maskStoredPassword(hasPassword bool) string {
	if !hasPassword {
		return ""
	}
	return "******"
}

func maskPassword(password string) string {
	if password == "" {
		return ""
	}
	if len(password) <= 2 {
		return strings.Repeat("*", len(password))
	}
	return password[:1] + strings.Repeat("*", len(password)-2) + password[len(password)-1:]
}

package controllers

import (
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"uvplatform.cn/uvp-gb28181/app/controllers"
	gbconfig "uvplatform.cn/uvp-gb28181/app/gb28181/config"
)

// PlatformInfo 是本级 GB28181 平台对外接入参数。
type PlatformInfo struct {
	Enabled        bool     `json:"enabled"`
	ServerID       string   `json:"serverId"`
	Domain         string   `json:"domain"`
	SIPIP          string   `json:"sipIp"`
	SIPPort        int      `json:"sipPort"`
	Transport      []string `json:"transport"`
	PasswordMasked string   `json:"passwordMasked"`
	RegisterURI    string   `json:"registerUri"`
}

// PlatformController 提供本级平台只读接入信息。
type PlatformController struct {
	controllers.Common
}

func NewPlatformController() *PlatformController {
	return &PlatformController{}
}

// Info GET /api/gb28181/sip/platform
func (pc *PlatformController) Info(c *gin.Context) {
	cfg := gbconfig.Load()
	pc.Success(c, PlatformInfo{
		Enabled:        cfg.Enabled,
		ServerID:       cfg.SIP.ServerID,
		Domain:         cfg.SIP.Domain,
		SIPIP:          cfg.SIP.IP,
		SIPPort:        cfg.SIP.Port,
		Transport:      cfg.SIP.Transport,
		PasswordMasked: maskPassword(cfg.SIP.Password),
		RegisterURI:    registerURI(cfg.SIP.ServerID, cfg.SIP.Domain, cfg.SIP.IP, cfg.SIP.Port),
	})
}

func registerURI(serverID, domain, ip string, port int) string {
	if serverID == "" || domain == "" || ip == "" || port <= 0 {
		return ""
	}
	return "sip:" + serverID + "@" + ip + ":" + strconv.Itoa(port)
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

package controllers

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strconv"
	"time"

	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/models"
	"uvplatform.cn/uvp-gb28181/app/service"

	"uvplatform.cn/uvp-gb28181/app/utils/captchahelper"
	"uvplatform.cn/uvp-gb28181/app/utils/common"
	"uvplatform.cn/uvp-gb28181/app/utils/passwordhelper"

	"github.com/gin-gonic/gin"
	useragent "github.com/mssola/user_agent"
	"gorm.io/gorm"
)

// LoginRequest 登录请求结构

type AuthController struct {
	Common
	sessions   authSessionLifecycle
	tokens     app.TokenServiceInterface
	sessionTTL func() time.Duration
}

type authSessionLifecycle interface {
	CreateLogin(context.Context, *models.User, service.LoginMetadata, app.TokenServiceInterface, time.Duration) (*service.SessionTokenPair, error)
	RotateRefresh(context.Context, string, app.TokenServiceInterface) (*service.SessionTokenPair, error)
	Revoke(context.Context, string, string, *uint) (bool, error)
}

// NewAuthController 创建认证控制器
func NewAuthController() *AuthController {
	return &AuthController{
		Common:     Common{},
		sessionTTL: configuredSessionTTL,
	}
}

func newAuthControllerWithDependencies(sessions authSessionLifecycle, tokens app.TokenServiceInterface, sessionTTL time.Duration) *AuthController {
	return &AuthController{Common: Common{}, sessions: sessions, tokens: tokens, sessionTTL: func() time.Duration { return sessionTTL }}
}

func (ac *AuthController) authSessions() authSessionLifecycle {
	if ac.sessions != nil {
		return ac.sessions
	}
	if sessions, ok := app.SessionValidator.(authSessionLifecycle); ok {
		return sessions
	}
	return nil
}

func (ac *AuthController) tokenService() app.TokenServiceInterface {
	if ac.tokens != nil {
		return ac.tokens
	}
	return app.TokenService
}

func configuredSessionTTL() time.Duration {
	return app.ConfigYml.GetDuration("token.jwttokenrefreshexpire") * time.Second
}

// Login 用户登录
// @Summary 用户登录
// @Description 用户登录获取访问令牌
// @Tags 认证
// @Accept json
// @Produce json
// @Param loginReq body models.LoginRequest true "登录请求参数"
// @Success 200 {object} map[string]interface{} "成功返回访问令牌"
// @Failure 400 {object} map[string]interface{} "请求参数错误"
// @Failure 401 {object} map[string]interface{} "用户名或密码错误"
// @Router /login [post]
func (ac *AuthController) Login(c *gin.Context) {
	var req models.LoginRequest
	if err := req.Validate(c); err != nil {
		ac.FailAndAbort(c, err.Error(), err)
	}

	// 根据用户名查找用户
	user := models.NewUser()
	err := user.Find(c, func(d *gorm.DB) *gorm.DB {
		return d.Where("username = ?", req.Username)
	})
	if err != nil {
		ac.FailAndAbort(c, "用户查询错误", err)
	}

	if user.IsEmpty() {
		ac.FailAndAbort(c, "用户不存在", nil)
	}
	if user.Status != 1 {
		ac.FailAndAbort(c, "用户未启用", nil)
	}

	// 获取安全配置
	loginLockThreshold := app.ConfigYml.GetInt("safe.loginlockthreshold")
	loginLockExpire := app.ConfigYml.GetInt("safe.loginlockexpire")
	loginLockDuration := app.ConfigYml.GetInt("safe.loginlockduration")

	// 如果启用了登录锁定功能
	if loginLockThreshold > 0 {
		// 检查账户是否被锁定
		lockKey := "account_locked:" + req.Username
		if locked, _ := app.Cache.Exists(context.Background(), lockKey); locked > 0 {
			ac.FailAndAbort(c, "账户已被锁定，请稍后再试", nil)
			return
		}

		// 验证密码
		if err = passwordhelper.ComparePassword(user.Password, req.Password); err != nil {
			// 密码错误，增加失败次数
			failCountKey := "login_fail_count:" + req.Username

			// 获取当前失败次数
			var failCount int
			if countStr, err := app.Cache.Get(context.Background(), failCountKey); err == nil && countStr != "" {
				failCount, _ = strconv.Atoi(countStr)
			}

			// 增加失败次数
			failCount++

			// 更新失败次数，设置过期时间
			app.Cache.Set(context.Background(), failCountKey, strconv.Itoa(failCount), time.Duration(loginLockExpire)*time.Second)

			// 检查是否达到锁定阈值
			if failCount >= loginLockThreshold {
				// 锁定账户
				app.Cache.Set(context.Background(), lockKey, "1", time.Duration(loginLockDuration)*time.Second)
				ac.FailAndAbort(c, "密码错误次数过多，账户已被锁定", nil)
				return
			}

			// 返回密码错误，并提示剩余尝试次数
			remainingAttempts := loginLockThreshold - failCount
			ac.FailAndAbort(c, "密码错误，剩余尝试次数: "+strconv.Itoa(remainingAttempts), nil)
			return
		}

		// 密码正确，清除失败次数
		failCountKey := "login_fail_count:" + req.Username
		app.Cache.Del(context.Background(), failCountKey)
	} else {
		// 未启用登录锁定功能，使用原有逻辑
		// 验证密码
		if err = passwordhelper.ComparePassword(user.Password, req.Password); err != nil {
			ac.FailAndAbort(c, "密码错误", err)
		}
	}

	// 会话落库成功后才向客户端返回 token。
	user.Password = ""
	sessions, tokens := ac.authSessions(), ac.tokenService()
	if sessions == nil || tokens == nil {
		ac.FailAndAbort(c, "认证会话服务不可用", service.ErrSessionStore, http.StatusServiceUnavailable)
	}
	pair, err := sessions.CreateLogin(c.Request.Context(), user, loginMetadata(c), tokens, ac.sessionTTL())
	if err != nil {
		ac.FailAndAbort(c, "创建登录会话失败", err, http.StatusServiceUnavailable)
	}
	claims, err := tokens.ParseToken(pair.AccessToken)
	if err != nil {
		ac.FailAndAbort(c, "解析token失败", err)
	}
	claims1, err := tokens.ParseRefreshToken(pair.RefreshToken)
	if err != nil {
		ac.FailAndAbort(c, "解析refreshToken失败", err)
	}

	ac.Success(c, gin.H{
		"accessToken":         pair.AccessToken,
		"accessTokenExpires":  claims.ExpiresAt.Unix(),
		"refreshToken":        pair.RefreshToken,
		"refreshTokenExpires": claims1.ExpiresAt.Unix(),
	})
}

// RefreshToken 刷新访问令牌
// @Summary 刷新访问令牌
// @Description 使用刷新令牌获取新的访问令牌
// @Tags 认证
// @Accept json
// @Produce json
// @Param refreshReq body models.RefreshRequest true "刷新令牌请求参数"
// @Success 200 {object} map[string]interface{} "成功返回新的访问令牌"
// @Failure 400 {object} map[string]interface{} "请求参数错误"
// @Failure 401 {object} map[string]interface{} "刷新令牌无效或过期"
// @Router /refreshToken [post]
func (ac *AuthController) RefreshToken(c *gin.Context) {
	// 首先尝试从header中获取refreshToken
	refreshToken := c.GetHeader("RefreshToken")
	if refreshToken == "" {
		// 如果header中没有，尝试从body中获取
		var req models.RefreshRequest
		if err := c.ShouldBind(&req); err != nil {
			ac.FailAndAbort(c, "refreshToken不能为空", err)
		}
		refreshToken = req.RefreshToken
	}

	sessions, tokens := ac.authSessions(), ac.tokenService()
	if sessions == nil || tokens == nil {
		ac.FailAndAbort(c, "认证会话服务不可用", service.ErrSessionStore, http.StatusServiceUnavailable)
	}
	pair, err := sessions.RotateRefresh(c.Request.Context(), refreshToken, tokens)
	if err != nil {
		status := http.StatusUnauthorized
		if errors.Is(err, service.ErrSessionStore) {
			status = http.StatusServiceUnavailable
		}
		ac.FailAndAbort(c, "refresh token刷新失败", err, status)
	}
	claims1, err := tokens.ParseToken(pair.AccessToken)
	if err != nil {
		ac.FailAndAbort(c, "refresh token解析失败", err)
	}
	newRefreshClaims, err := tokens.ParseRefreshToken(pair.RefreshToken)
	if err != nil {
		ac.FailAndAbort(c, "解析新refresh token失败", err)
	}

	ac.Success(c, gin.H{
		"accessToken":         pair.AccessToken,
		"accessTokenExpires":  claims1.ExpiresAt.Unix(),
		"refreshToken":        pair.RefreshToken,
		"refreshTokenExpires": newRefreshClaims.ExpiresAt.Unix(),
	})
}

// Logout 用户登出
// @Summary 用户登出
// @Description 用户登出，撤销access token和refresh token
// @Tags 认证
// @Accept json
// @Produce json
// @Success 200 {object} map[string]interface{} "成功登出"
// @Failure 401 {object} map[string]interface{} "用户未登录"
// @Router /users/logout [post]
func (ac *AuthController) Logout(c *gin.Context) {
	// 从上下文中获取用户信息
	claims := common.GetClaims(c)
	if claims == nil {
		ac.FailAndAbort(c, "用户未登录", nil)
		return
	}

	// 兼容已开启 access-token cache 的部署,但会话失效以数据库 sid 为准。
	tokenString, err := common.GetAccessToken(c)
	if err == nil && tokenString != "" {
		_ = ac.tokenService().RevokeTokenWithCache(tokenString)
	}
	sessions := ac.authSessions()
	if sessions == nil {
		ac.FailAndAbort(c, "认证会话服务不可用", service.ErrSessionStore, http.StatusServiceUnavailable)
	}
	_, err = sessions.Revoke(c.Request.Context(), claims.SID, "logout", nil)
	if err != nil {
		ac.FailAndAbort(c, "登出失败", err, http.StatusServiceUnavailable)
	}

	ac.Success(c, gin.H{
		"message": "登出成功",
	})
}

func loginMetadata(c *gin.Context) service.LoginMetadata {
	rawUA := c.Request.UserAgent()
	ua := useragent.New(rawUA)
	browser, _ := ua.Browser()
	osName := ua.OS()
	return service.LoginMetadata{
		ClientIP: c.ClientIP(), LoginLocation: loginLocation(c.ClientIP()), UserAgent: rawUA,
		Browser: browser, OS: osName,
	}
}

func loginLocation(rawIP string) string {
	ip := net.ParseIP(rawIP)
	if ip != nil && (ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast()) {
		return "内网"
	}
	return "未知"
}

// GetVerifyImgString 获取验证码图片字符串
// @Summary 获取验证码图片字符串
// @Description 获取验证码图片字符串，返回验证码ID和base64图片
// @Tags 认证
// @Produce json
// @Success 200 {object} map[string]interface{} "成功返回验证码ID和base64图片"
// @Failure 500 {object} map[string]interface{} "生成验证码失败"
// @Router /captcha/verify [get]
func (ac *AuthController) GetVerifyImgString(c *gin.Context) {
	idKeyC, base64stringC, err := captchahelper.GetCaptchaHelper().GetVerifyImgString()
	if err != nil {
		ac.FailAndAbort(c, "生成验证码失败", err)
		return
	}

	ac.Success(c, gin.H{
		"captchaId": idKeyC,
		"image":     base64stringC,
	})
}

package controllers

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"uvplatform.com/uvp-gb28181/app/global/app"
	"uvplatform.com/uvp-gb28181/app/models"
	"uvplatform.com/uvp-gb28181/app/service"

	"uvplatform.com/uvp-gb28181/app/utils/captchahelper"
	"uvplatform.com/uvp-gb28181/app/utils/common"
	"uvplatform.com/uvp-gb28181/app/utils/logging"
	"uvplatform.com/uvp-gb28181/app/utils/passwordhelper"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// loginInvalidCredentialMessage 是「账号不存在」与「口令错误」共用的统一话术。
//
// ⛔ 不要拆成两句（例如"用户不存在"/"密码错误"）：那是标准的用户名枚举入口，
// 配合按用户名计的锁定额度还能被反过来用来锁别人账号。
// 需要区分真实原因的场景一律看登录日志的 failure_reason（sys_dict 里有 user_not_found /
// password_incorrect 两项，仅供管理员查看）。
const loginInvalidCredentialMessage = "用户名或密码错误"

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
		ac.recordLogin(c, nil, req.Username, service.LoginResultFailure, service.LoginFailureServerError)
		ac.FailAndAbort(c, err.Error(), err)
	}

	// 根据用户名查找用户
	user := models.NewUser()
	err := user.Find(c.Request.Context(), func(d *gorm.DB) *gorm.DB {
		return d.Where("username = ?", req.Username)
	})
	if err != nil {
		ac.recordLogin(c, nil, req.Username, service.LoginResultFailure, service.LoginFailureServerError)
		ac.FailAndAbort(c, "用户查询错误", err)
	}

	// ⛔⛔ 账号不存在与口令错误必须返回**同一句话术**（loginInvalidCredentialMessage）。
	// 回"用户不存在"等于直接告诉攻击者"这个用户名有效"，配合下面按用户名计的锁定阈值，
	// 还能被反向利用去锁别人的账号。真实原因只进登录日志（LoginFailureUserNotFound /
	// LoginFailurePasswordIncorrect），审计侧仍然分得清，接口侧不区分。
	if user.IsEmpty() {
		// 抹平时间差：账号不存在时也走一遍等价耗时的 bcrypt，否则
		// "<1ms 返回"与"~60ms 返回"本身就是可测量的枚举通道（不用看任何文案）。
		passwordhelper.CompareDummy(req.Password)
		ac.recordLogin(c, nil, req.Username, service.LoginResultFailure, service.LoginFailureUserNotFound)
		ac.FailAndAbort(c, loginInvalidCredentialMessage, nil)
	}

	// 获取安全配置
	loginLockThreshold := app.ConfigYml.GetInt("safe.loginlockthreshold")
	loginLockExpire := app.ConfigYml.GetInt("safe.loginlockexpire")
	loginLockDuration := app.ConfigYml.GetInt("safe.loginlockduration")

	// 如果启用了登录锁定功能
	// Preserve the existing independent lock-accounting lifetime while carrying log scope.
	lockContext := context.WithoutCancel(c.Request.Context())
	if loginLockThreshold > 0 {
		// 检查账户是否被锁定
		lockKey := "account_locked:" + req.Username
		if locked, _ := app.Cache.Exists(lockContext, lockKey); locked > 0 {
			ac.recordLogin(c, user, req.Username, service.LoginResultFailure, service.LoginFailureAccountLocked)
			ac.FailAndAbort(c, "账户已被锁定，请稍后再试", nil)
			return
		}

		// 验证密码
		if err = passwordhelper.ComparePassword(user.Password, req.Password); err != nil {
			// 密码错误，增加失败次数
			failCountKey := "login_fail_count:" + req.Username

			// 获取当前失败次数
			var failCount int
			if countStr, err := app.Cache.Get(lockContext, failCountKey); err == nil && countStr != "" {
				failCount, _ = strconv.Atoi(countStr)
			}

			// 增加失败次数
			failCount++

			// 更新失败次数，设置过期时间
			if cacheErr := app.Cache.Set(lockContext, failCountKey, strconv.Itoa(failCount), time.Duration(loginLockExpire)*time.Second); cacheErr != nil {
				app.Log(lockContext).Warn("记录登录失败次数失败", zap.String("event", "auth.login_failure_count_cache_failed"), logging.Error(cacheErr))
			}

			// 检查是否达到锁定阈值
			if failCount >= loginLockThreshold {
				// 锁定账户
				if cacheErr := app.Cache.Set(lockContext, lockKey, "1", time.Duration(loginLockDuration)*time.Second); cacheErr != nil {
					app.Log(lockContext).Warn("写入登录锁定状态失败", zap.String("event", "auth.login_lock_cache_failed"), logging.Error(cacheErr))
				}
				ac.recordLogin(c, user, req.Username, service.LoginResultFailure, service.LoginFailureAccountLocked)
				ac.FailAndAbort(c, "密码错误次数过多，账户已被锁定", nil)
				return
			}

			// 统一话术：不再回"剩余尝试次数" —— 该数字只在账号存在时才出现，
			// 本身就是一枚存在性探针。锁定行为本身保持不变。
			ac.recordLogin(c, user, req.Username, service.LoginResultFailure, service.LoginFailurePasswordIncorrect)
			ac.FailAndAbort(c, loginInvalidCredentialMessage, nil)
			return
		}

		// 密码正确，清除失败次数
		failCountKey := "login_fail_count:" + req.Username
		if cacheErr := app.Cache.Del(lockContext, failCountKey); cacheErr != nil {
			app.Log(lockContext).Warn("清除登录失败次数失败", zap.String("event", "auth.login_failure_count_delete_failed"), logging.Error(cacheErr))
		}
	} else {
		// 未启用登录锁定功能，使用原有逻辑
		// 验证密码
		if err = passwordhelper.ComparePassword(user.Password, req.Password); err != nil {
			ac.recordLogin(c, user, req.Username, service.LoginResultFailure, service.LoginFailurePasswordIncorrect)
			ac.FailAndAbort(c, loginInvalidCredentialMessage, nil)
		}
	}

	// ⛔ 状态检查必须**在口令校验之后**：放在前面的话，随便输一个用户名就能通过
	// "用户未启用"得知该账号存在（连口令都不用知道）。口令正确的本人仍然看得到确切原因。
	if user.Status != 1 {
		ac.recordLogin(c, user, req.Username, service.LoginResultFailure, service.LoginFailureUserDisabled)
		ac.FailAndAbort(c, "用户未启用", nil)
	}

	// 会话落库成功后才向客户端返回 token。
	user.Password = ""
	sessions, tokens := ac.authSessions(), ac.tokenService()
	if sessions == nil || tokens == nil {
		ac.recordLogin(c, user, req.Username, service.LoginResultFailure, service.LoginFailureSessionCreate)
		ac.FailAndAbort(c, "认证会话服务不可用", service.ErrSessionStore, http.StatusServiceUnavailable)
	}
	pair, err := sessions.CreateLogin(c.Request.Context(), user, loginMetadata(c), tokens, ac.sessionTTL())
	if err != nil || pair == nil {
		if err == nil {
			err = service.ErrSessionStore
		}
		ac.recordLogin(c, user, req.Username, service.LoginResultFailure, service.LoginFailureSessionCreate)
		ac.FailAndAbort(c, "创建登录会话失败", err, http.StatusServiceUnavailable)
	}
	claims, err := tokens.ParseToken(pair.AccessToken)
	if err != nil {
		ac.recordLogin(c, user, req.Username, service.LoginResultFailure, service.LoginFailureServerError)
		ac.FailAndAbort(c, "解析token失败", err)
	}
	claims1, err := tokens.ParseRefreshToken(pair.RefreshToken)
	if err != nil {
		ac.recordLogin(c, user, req.Username, service.LoginResultFailure, service.LoginFailureServerError)
		ac.FailAndAbort(c, "解析refreshToken失败", err)
	}

	ac.recordLogin(c, user, req.Username, service.LoginResultSuccess, "")
	ac.Success(c, gin.H{
		"accessToken":         pair.AccessToken,
		"accessTokenExpires":  claims.ExpiresAt.Unix(),
		"refreshToken":        pair.RefreshToken,
		"refreshTokenExpires": claims1.ExpiresAt.Unix(),
		"mustChangePassword":  user.MustChangePassword,
	})
}

func (ac *AuthController) recordLogin(c *gin.Context, user *models.User, username, result, reason string) {
	var userID *uint
	if user != nil && user.ID != 0 {
		id := user.ID
		userID = &id
	}
	event := app.LoginLogEvent{UserID: userID, Username: username, Result: result, FailureReason: reason}
	metadata := service.LoginMetadataWithContext(c.Request.Context(), c.ClientIP(), c.Request.UserAgent())
	event.IP, event.Location, event.UserAgent = metadata.ClientIP, metadata.LoginLocation, metadata.UserAgent
	event.Browser, event.OS = metadata.Browser, metadata.OS
	service.RecordLoginAttempt(c.Request.Context(), app.LoginLogRecorder, event)
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
	return service.LoginMetadataWithContext(c.Request.Context(), c.ClientIP(), c.Request.UserAgent())
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

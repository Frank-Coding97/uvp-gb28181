package middleware

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"time"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/models"
	"uvplatform.cn/uvp-gb28181/app/utils/common"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

const sensitiveOperationContextKey = "operation_log_sensitive_metadata"
const operationTypeContextKey = "operation_log_type"

// MarkSensitiveOperation keeps response payloads out of the operation-log capture buffer.
// Metadata must identify the resource and purpose without containing the sensitive value.
func MarkSensitiveOperation(c *gin.Context, metadata map[string]any) {
	if c != nil {
		c.Set(sensitiveOperationContextKey, metadata)
	}
}

// SensitiveOperationMetadata returns a copy for narrowly scoped audit tests
// and middleware integrations. Sensitive response payloads remain unavailable.
func SensitiveOperationMetadata(c *gin.Context) (map[string]any, bool) {
	if c == nil {
		return nil, false
	}
	metadata, ok := c.Get(sensitiveOperationContextKey)
	if !ok {
		return nil, false
	}
	values, ok := metadata.(map[string]any)
	if !ok {
		return nil, false
	}
	copy := make(map[string]any, len(values))
	for key, value := range values {
		copy[key] = value
	}
	return copy, true
}

// MarkDeleteOperation records a POST-based batch endpoint as a delete action.
func MarkDeleteOperation(c *gin.Context) {
	if c != nil {
		c.Set(operationTypeContextKey, models.OperationDelete)
	}
}

// OperationLogMiddleware 操作日志中间件
func OperationLogMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 跳过不需要记录日志的请求
		if shouldSkipLog(c) {
			c.Next()
			return
		}

		startTime := time.Now()

		// 复制请求体用于记录:审计副本有硬上限,超大请求只记元数据,
		// 防止单个大请求制造多份内存副本拖垮进程。
		// 业务侧始终拿到完整请求体:超限时把已读前缀与剩余流拼接回去
		var requestBody []byte
		if c.Request.Body != nil {
			const maxAuditBodyBytes = 64 << 10
			requestBody, _ = io.ReadAll(io.LimitReader(c.Request.Body, maxAuditBodyBytes+1))
			c.Request.Body = io.NopCloser(io.MultiReader(bytes.NewReader(requestBody), c.Request.Body))
			if len(requestBody) > maxAuditBodyBytes {
				requestBody = nil
			}
		}

		// 创建自定义的ResponseWriter来捕获响应
		writer := &responseWriter{body: bytes.NewBuffer(nil), ResponseWriter: c.Writer, context: c}
		c.Writer = writer

		defer func() {
			// 在请求 goroutine 内同步构造不可变日志记录:异步 goroutine 继续读
			// Gin Context 会在 Context 回收复用时产生竞态/跨请求错配
			record := buildOperationLogRecord(c, startTime, requestBody, writer.body.Bytes())
			if record != nil {
				go persistOperationLog(record)
			}
		}()

		c.Next()
	}
}

// responseWriter 自定义ResponseWriter用于捕获响应数据
type responseWriter struct {
	gin.ResponseWriter
	body    *bytes.Buffer
	context *gin.Context
}

func (w *responseWriter) Write(b []byte) (int, error) {
	if _, sensitive := w.context.Get(sensitiveOperationContextKey); !sensitive {
		w.body.Write(b)
	}
	return w.ResponseWriter.Write(b)
}

// shouldSkipLog 判断是否需要跳过日志记录
func shouldSkipLog(c *gin.Context) bool {
	// ZLM expands its complete INI into this callback, including api.secret.
	// Skip the exact endpoint before reading the body; handler-time redaction is
	// too late because this middleware captures requests before c.Next().
	if c.Request.URL.Path == "/index/hook/on_server_started" {
		return true
	}

	// 跳过静态文件、健康检查等请求
	skipPaths := []string{
		"/swagger/",
		"/favicon.ico",
		"/health",
		"/metrics",
		"/api/refreshToken",            // 刷新token
		"/api/captcha/id",              // 生成验证码ID
		"/api/captcha/image",           // 获取验证码图片
		"/api/config/get",              // 获取配置信息
		"/api/users/session/heartbeat", // 会话续活
	}

	path := c.Request.URL.Path
	for _, skipPath := range skipPaths {
		if strings.Contains(path, skipPath) {
			return true
		}
	}

	return false
}

// buildOperationLogRecord 同步构造操作日志记录(只在请求 goroutine 内调用)
func buildOperationLogRecord(c *gin.Context, startTime time.Time, requestBody, responseBody []byte) *models.SysOperationLog {
	duration := time.Since(startTime).Milliseconds()

	// 获取用户信息
	var userID uint
	var username string
	operationType := getOperationType(c)

	// 尝试从JWT token获取用户信息
	claims := common.GetClaims(c)
	if claims != nil {
		userID = claims.UserID
		username = claims.Username
	} else {
		// 如果是登录操作，尝试从请求体中获取用户名
		if c.Request.URL.Path == "/api/login" && c.Request.Method == "POST" {
			// 解析登录请求体获取用户名
			var loginReq struct {
				Username string `json:"username"`
			}
			if len(requestBody) > 0 {
				if err := json.Unmarshal(requestBody, &loginReq); err == nil && loginReq.Username != "" {
					username = loginReq.Username
					// 标记为登录操作
					operationType = models.OperationLogin
				}
			}
		}
	}

	// 构建操作日志
	log := &models.SysOperationLog{
		UserID:      userID,
		Username:    username,
		Module:      getOperationModule(c),
		Operation:   operationType,
		Method:      c.Request.Method,
		Path:        c.Request.URL.Path,
		IP:          c.ClientIP(),
		UserAgent:   c.Request.UserAgent(),
		RequestData: operationLogRequestData(c, requestBody),
		//ResponseData: sanitizeResponseData(responseBody),
		StatusCode: c.Writer.Status(),
		Duration:   duration,
		ErrorMsg:   getErrorMessage(c, responseBody),
		Location:   getLocationByIP(c.ClientIP()),
	}

	return log
}

// persistOperationLog 异步持久化日志记录(不接触 Gin Context)
func persistOperationLog(log *models.SysOperationLog) {
	if err := app.DB().Create(log).Error; err != nil {
		app.ZapLog.Error("记录操作日志失败", zap.Error(err))
	}
}

func operationLogRequestData(c *gin.Context, requestBody []byte) string {
	if metadata, ok := c.Get(sensitiveOperationContextKey); ok {
		encoded, err := json.Marshal(metadata)
		if err == nil {
			return string(encoded)
		}
		return "{\"sensitive\":true}"
	}
	return sanitizeRequestData(requestBody)
}

// getOperationModule 获取操作模块。
// 前缀按具体度降序排列:例如 /gb28181/device-mgmt 必须先于 /gb28181/device,
// /gb28181/playback-schemes 必须先于 /gb28181/play(Contains 子串匹配)。
func getOperationModule(c *gin.Context) string {
	path := c.Request.URL.Path

	modulePrefixes := []struct {
		prefix string
		module string
	}{
		// 国标模块(命名对齐前端菜单)
		{"/gb28181/device-mgmt", "GB28181设备管理"},
		{"/gb28181/device", "GB28181设备管理"},
		{"/gb28181/playback-schemes", "GB28181多屏播放"},
		{"/gb28181/play", "GB28181实时点播"},
		{"/gb28181/cloud-recordings", "GB28181云端录像"},
		{"/gb28181/alarms", "GB28181告警管理"},
		{"/gb28181/cascade", "GB28181级联管理"},
		{"/gb28181/zlm", "GB28181流媒体管理"},
		{"/gb28181/security", "GB28181接入安全"},
		{"/gb28181/sip/service-config", "GB28181服务配置"},
		{"/gb28181/sip/setup", "GB28181初始化配置"},
		{"/gb28181/sip/dashboard", "GB28181信令看板"},
		{"/gb28181/sip/platform", "GB28181SIP接入信息"},
		{"/gb28181/sip/qr", "GB28181扫码接入"},
		{"/gb28181/sip-traces", "GB28181SIP日志"},
		// 系统模块
		{"/login", "认证管理"},
		{"/users", "用户管理"},
		{"/sysMenu", "菜单管理"},
		{"/sysRole", "角色管理"},
		{"/sysDepartment", "部门管理"},
		{"/sysDict", "字典管理"},
		{"/sysApi", "API管理"},
		{"/sysAffix", "文件管理"},
		{"/config", "系统配置"},
		{"/sysOperationLog", "操作日志管理"},
		{"/sysLoginLog", "登录日志管理"},
		{"/sysOnlineUser", "在线用户管理"},
	}

	for _, entry := range modulePrefixes {
		if strings.Contains(path, entry.prefix) {
			return entry.module
		}
	}
	return "其他"
}

// getOperationType 获取操作类型
func getOperationType(c *gin.Context) string {
	if operationType, ok := c.Get(operationTypeContextKey); ok {
		if value, valid := operationType.(string); valid && value != "" {
			return value
		}
	}
	method := c.Request.Method
	switch method {
	case "POST":
		return models.OperationCreate
	case "PUT", "PATCH":
		return models.OperationUpdate
	case "DELETE":
		return models.OperationDelete
	case "GET":
		return models.OperationQuery
	default:
		return "unknown"
	}
}

// getErrorMessage 获取错误信息
func getErrorMessage(c *gin.Context, responseBody []byte) string {
	if c.Writer.Status() >= 400 {
		// 首先尝试从上下文中获取错误信息
		if err, exists := c.Get("error"); exists {
			return err.(error).Error()
		}
		// 如果上下文中没有错误信息，尝试解析响应体
		if len(responseBody) > 0 {
			// 尝试解析JSON响应体
			var response map[string]interface{}
			if err := json.Unmarshal(responseBody, &response); err == nil {
				// 根据项目中的响应格式获取错误信息（使用message字段）
				if msg, ok := response["message"].(string); ok && msg != "" {
					return msg
				}
			}
		}
		return "请求处理失败"
	}
	return ""
}

// sanitizeNested 递归脱敏嵌套对象/数组中的敏感键
func sanitizeNested(value interface{}) interface{} {
	switch nested := value.(type) {
	case map[string]interface{}:
		for key, item := range nested {
			switch strings.ToLower(key) {
			case "password", "newpassword", "oldpassword", "token", "accesstoken", "apikey", "secret", "apisecret":
				nested[key] = "***"
			default:
				nested[key] = sanitizeNested(item)
			}
		}
	case []interface{}:
		for i, item := range nested {
			nested[i] = sanitizeNested(item)
		}
	}
	return value
}

// getLocationByIP 根据IP获取地理位置（简化实现）
func getLocationByIP(ip string) string {
	// 这里可以集成第三方IP地理位置服务
	// 简化实现：返回空字符串
	return ""
}

// sanitizeRequestData 对请求数据进行脱敏处理
func sanitizeRequestData(data []byte) string {
	if len(data) == 0 {
		return ""
	}

	// 如果是JSON数据，尝试脱敏敏感字段
	if json.Valid(data) {
		// 按敏感键递归脱敏:密码/一次性 token/secret 等凭据不得落审计日志
		// (QR 兑换端点以 token 为唯一凭据,落库等于泄露接入码)。
		// 用 interface{} 反序列化以同时支持根对象与根数组
		var jsonData interface{}
		if err := json.Unmarshal(data, &jsonData); err == nil {
			sanitized := sanitizeNested(jsonData)

			// 重新序列化
			if encoded, err := json.Marshal(sanitized); err == nil {
				return string(encoded)
			}
		}
	}

	// 如果不是JSON，直接返回原始数据（限制长度）
	if len(data) > 10000 {
		return string(data[:10000]) + "...(truncated)"
	}
	return string(data)
}

// sanitizeResponseData 对响应数据进行脱敏处理
func sanitizeResponseData(data []byte) string {
	if len(data) == 0 {
		return ""
	}

	// 限制响应数据长度
	if len(data) > 5000 {
		return string(data[:5000]) + "...(truncated)"
	}
	return string(data)
}

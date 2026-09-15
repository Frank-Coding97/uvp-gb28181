package controllers

import (
	"errors"
	"net/http"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/global/consts"
	"uvplatform.cn/uvp-gb28181/app/utils/common"
	"uvplatform.cn/uvp-gb28181/app/utils/logging"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

type Common struct {
}

// failHTTPStatus 复刻 response.Fail 取状态码的规则（data[0] 若是 int 即状态码，默认 400），
// 只为给日志选等级，不改变响应行为。
func failHTTPStatus(data []interface{}) int {
	if len(data) > 0 {
		if code, ok := data[0].(int); ok {
			return code
		}
	}
	return http.StatusBadRequest
}

// logFailure 记录一次请求失败。**等级按状态码分两支**（C03.③）：
//   - 4xx（默认 400）→ INFO。请求被拒是系统按设计做对了："设备不存在""预置位编号不合法"
//     "订阅有效期超范围"都不需要任何人做任何事，与 `casbin.permission.denied`
//     （403 → INFO）同判据。本出口 881 处调用里 799 处（90.7%）走这一支
//     —— 703 处不传状态码取默认 400，96 处显式 4xx。
//   - 5xx（82 处，9.3%）→ ERROR。这才是我方内部故障。
//
// 不做这个区分的代价不是"多打了日志"：ERROR 是值班立即介入的信号，让 799 个输入校验
// 顶着 ERROR 出现会把整个 ERROR 桶贬值（当日 70 条 ERROR 里 2 条就是它）。
// 同一出口两种语义无法共用一个 event 名（`event_level_divergence` 要求事件名等级唯一），
// 故拆成两个名字 —— 顺带让"我方 HTTP 故障"第一次可以直接按 event 名筛出来。
// ⚠️ 静态口径下本文件只有 2 个调用点，运行时它覆盖 881 处；别用指标数字估这次改动的收益。
func logFailure(ctx *gin.Context, status int, err error) {
	logger := app.Log(ctx.Request.Context())
	if status >= http.StatusInternalServerError {
		logger.Error("请求失败(服务端)", zap.String("event", "http.operation_failed"),
			zap.String("route", ctx.FullPath()), zap.String("method", ctx.Request.Method),
			zap.String("source_ip", ctx.ClientIP()),
			logging.Error(err))
		return
	}
	logger.Info("请求被拒", zap.String("event", "http.operation_rejected"),
		zap.String("route", ctx.FullPath()), zap.String("method", ctx.Request.Method),
		zap.String("source_ip", ctx.ClientIP()),
		logging.Error(err))
}

// Fail 返回失败响应，支持可变参数：第一个参数为HTTP状态码（默认400）, 第二个参数为业务状态码, 第三个参数为响应数据
func (c Common) Fail(ctx *gin.Context, msg string, err error, data ...interface{}) {
	logFailure(ctx, failHTTPStatus(data), err)
	app.Response.Fail(ctx, msg, data...)
}

// FailAndAbort 失败并自动终止执行，无需手动 return ，支持可变参数：第一个参数为HTTP状态码（默认400）, 第二个参数为业务状态码, 第三个参数为响应数据
func (c Common) FailAndAbort(ctx *gin.Context, msg string, err error, data ...interface{}) {
	logFailure(ctx, failHTTPStatus(data), err)
	app.Response.Fail(ctx, msg, data...)
	if err != nil {
		ctx.Set("error", err)
	} else {
		ctx.Set("error", errors.New(msg))
	}
	// 使用 Gin 的 Abort 方法终止请求处理链
	ctx.Abort()
	// 使用 panic 终止当前函数执行
	panic(consts.RequestAborted)
}

// Success 返回成功响应，支持可变参数：第一个参数为响应数据，第二个参数为消息, 第三个参数为业务逻辑状态码
func (c Common) Success(ctx *gin.Context, data ...interface{}) {
	app.Response.Success(ctx, data...)
}

// SuccessWithMessage 返回成功响应并指定消息，支持可变参数：第一个参数为响应数据，第二个参数为业务逻辑状态码
func (c Common) SuccessWithMessage(ctx *gin.Context, msg string, data ...interface{}) {
	if len(data) > 0 {
		code := 0
		if len(data) > 1 {
			if codeValue, ok := data[1].(int); ok {
				code = codeValue
			}
		}
		app.Response.Success(ctx, data[0], msg, code)
	} else {
		app.Response.Success(ctx, nil, msg)
	}
}

// GetAccessToken 获取access token
func (c Common) GetAccessToken(ctx *gin.Context) (string, error) {
	return common.GetAccessToken(ctx)
}

// GetClaims 从上下文获取 Claims
func (c Common) GetClaims(ctx *gin.Context) *app.Claims {
	return common.GetClaims(ctx)
}

// GetCurrentUserID 获取当前用户ID
func (c Common) GetCurrentUserID(ctx *gin.Context) uint {
	return common.GetCurrentUserID(ctx)
}

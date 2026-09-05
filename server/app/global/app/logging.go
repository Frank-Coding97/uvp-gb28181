package app

import (
	"context"
	"go.uber.org/zap"
	"uvplatform.cn/uvp-gb28181/app/utils/logging"
)

func Log(ctx context.Context) *zap.Logger { return logging.FromContext(ctx, ZapLog) }

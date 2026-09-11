package app

import (
	"context"
	"go.uber.org/zap"
	"gorm.io/gorm"
	"uvplatform.cn/uvp-gb28181/app/utils/logging"
)

func Log(ctx context.Context) *zap.Logger { return logging.FromContext(ctx, ZapLog) }

func DBContext(ctx context.Context, dialect ...string) *gorm.DB {
	if ctx == nil {
		ctx = context.Background()
	}
	return DB(dialect...).WithContext(ctx)
}

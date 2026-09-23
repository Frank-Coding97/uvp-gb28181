package policyhelperfield

import (
	"go.uber.org/zap"

	"uvplatform.cn/uvp-gb28181/app/utils/logging"
)

func unsafeField(payload any) zap.Field {
	return zap.Any("payload", payload)
}

func Run(logger *zap.Logger, payload any, err error) {
	logger.Info("helper field", zap.String("event", "fixture.helper"), unsafeField(payload))
	logger.Error("safe error", zap.String("event", "fixture.error"), logging.Error(err))
}

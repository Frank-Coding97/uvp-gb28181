package policylegacyduplicate

import "go.uber.org/zap"

func Historical(logger *zap.Logger) {
	logger.Info("historic message")
	logger.Info("historic message")
}

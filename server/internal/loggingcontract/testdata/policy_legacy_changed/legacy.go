package policylegacychanged

import "go.uber.org/zap"

func Historical(logger *zap.Logger) {
	logger.Info("changed historic message")
}

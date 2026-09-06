package policyclean

import (
	"context"
	"errors"
	"fmt"
	"go.uber.org/zap"
	"log/slog"
	"strings"
)

const eventName = "fixture.clean"

type Service struct {
	logger  *zap.Logger
	slogger *slog.Logger
}

func (s *Service) Run(err error, payload any) error {
	if err != nil {
		s.logger.Error("operation failed", zap.String("event", eventName), zap.Error(err))
	}
	_ = payload
	var builder strings.Builder
	_, _ = fmt.Fprintf(&builder, "business value: %s", "safe")
	return errors.New("business failure: " + err.Error())
}

func (s *Service) Slog(ctx context.Context) {
	s.slogger.InfoContext(ctx, "slog operation", "event", "fixture.slog")
}

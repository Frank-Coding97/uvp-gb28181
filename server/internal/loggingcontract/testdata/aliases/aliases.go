package aliases

import (
	fmtalias "fmt"
	zapalias "go.uber.org/zap"
	stdlog "log"
	slogalias "log/slog"
	"os"
	appalias "uvplatform.cn/uvp-gb28181/app/global/app"
)

const (
	staticPrefix  = "static:"
	staticSuffix  = "message"
	staticMessage = staticPrefix + staticSuffix
)

type Service struct {
	logger *zapalias.Logger
}

func NewService() *Service {
	logger, _ := zapalias.NewDevelopment()
	return &Service{logger: logger}
}

func NewConfiguredLogger() *zapalias.Logger {
	return zapalias.Must(zapalias.NewProduction())
}

func NewExampleLogger() *zapalias.Logger {
	return zapalias.NewExample()
}

func (s *Service) Serve(injected *zapalias.Logger, payload any) {
	appalias.ZapLog.Info(staticMessage)
	injected.Warn("injected logger")
	s.logger.Error("service logger", zapalias.Error(fmtalias.Errorf("payload: %v", payload)))
	stdlog.Print("stdlib logger")
	slogalias.Default().Info("slog logger")
	fmtalias.Fprintln(os.Stdout, "stdout bypass")
	zapalias.Any("payload", payload)
}

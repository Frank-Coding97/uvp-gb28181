package policybad

import (
	"context"
	fmtalias "fmt"
	zapalias "go.uber.org/zap"
	"log"
	"log/slog"
	"os"
)

func Bad(message string, payload any, err error) {
	logger, _ := zapalias.NewProduction()
	logger.Info(message, zapalias.Any("payload", payload), zapalias.String("error", err.Error()))
	logger.Warn("missing event", zapalias.String("kind", "fixture"))
	slog.InfoContext(context.Background(), message, slog.Any("payload", payload))
	log.Printf(message)
	fmtalias.Fprint(os.Stdout, message)
	os.Stdout.WriteString(message)
}

package violations

import (
	"errors"
	"fmt"
	"go.uber.org/zap"
	"os"
)

func Bad(value string) error {
	fmt.Fprint(os.Stdout, value)
	logger := zap.NewNop()
	logger.Info("raw payload", zap.Any("payload", value))
	return errors.New(value)
}

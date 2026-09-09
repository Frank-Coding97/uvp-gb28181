package messages

import (
	"errors"
	"fmt"
	"go.uber.org/zap"
	"log"
)

const (
	messagePrefix = "prefix"
	messageSuffix = "suffix"
	messageConst  = messagePrefix + messageSuffix
)

func Stable() {
	logger := zap.NewNop()
	logger.Info(messageConst)
	logger.Info("left" + "right")
	log.Printf("static format %s", "value")
}

func Dynamic(message string) error {
	logger := zap.NewNop()
	logger.Info(message)
	log.Print(message)
	fmt.Print(message)
	return errors.New(message)
}

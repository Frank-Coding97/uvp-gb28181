//go:build !windows

package main

import (
	"errors"
	"io"

	"uvplatform.cn/uvp-gb28181/internal/standalone"
)

func readRecoveryConfirmationInput(_ standalone.RecoveryConfirmationInfo, _ io.Writer) (standalone.RecoveryConfirmationInput, error) {
	return standalone.RecoveryConfirmationInput{}, errors.New("本机确认仅支持 Windows 控制台")
}

//go:build !windows

package main

import (
	"errors"
	"uvplatform.cn/uvp-gb28181/internal/standalone"
)

func checkAccountACL(standalone.Paths) (any, error) {
	return nil, errors.New("account ACL acceptance requires Windows")
}

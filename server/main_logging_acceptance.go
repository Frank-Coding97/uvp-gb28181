//go:build logging_acceptance

package main

import "uvplatform.cn/uvp-gb28181/internal/loggingacceptance"

func init() {
	loggingacceptance.Register()
}

//go:build logging_acceptance

package main

import "uvplatform.com/uvp-gb28181/internal/loggingacceptance"

func init() {
	loggingacceptance.Register()
}

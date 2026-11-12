package main

import "time"

// Keep the process and its dependencies alive until the SAME runtime drains.
// A timeout is not permission to return from main or discard database state.
// Each stop attempt supplies its own bounded context; never overlap attempts.
func waitForSIPShutdown(stop func() error, report func(error), retryDelay time.Duration) {
	for {
		if err := stop(); err != nil {
			report(err)
			time.Sleep(retryDelay)
			continue
		}
		return
	}
}

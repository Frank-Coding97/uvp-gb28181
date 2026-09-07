package bootstrap

import (
	"fmt"
	"sync"

	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/scheduler"
)

// runtimeJobsHooks keeps the startup sequence injectable for the bootstrap
// gate tests. Production uses the existing scheduler constructors and
// registration functions below.
type runtimeJobsHooks struct {
	newScheduler       func() app.JobSchedulerInterf
	registerExecutors  func()
	registerSystemJobs func() error
	loadJobsFromDB     func() error
}

var (
	runtimeJobsMu         sync.Mutex
	runtimeJobsStarted    bool
	runtimeJobsStartHooks = runtimeJobsHooks{
		newScheduler:      newScheduler,
		registerExecutors: scheduler.RegisterExecutors,
		registerSystemJobs: func() error {
			return scheduler.RegisterSystemJobs(app.DB())
		},
		loadJobsFromDB: func() error {
			scheduler.LoadJobsFromDB()
			return nil
		},
	}
)

// StartRuntimeJobs starts the scheduler and loads persisted jobs exactly once.
// A failed startup leaves the previous scheduler in place and can be retried
// by the caller after fixing the startup condition.
func StartRuntimeJobs() error {
	runtimeJobsMu.Lock()
	defer runtimeJobsMu.Unlock()

	if runtimeJobsStarted {
		return nil
	}
	hooks := runtimeJobsStartHooks
	if hooks.newScheduler == nil {
		return fmt.Errorf("runtime jobs scheduler constructor is nil")
	}
	if hooks.registerExecutors == nil {
		return fmt.Errorf("runtime jobs executor registration is nil")
	}
	if hooks.registerSystemJobs == nil {
		return fmt.Errorf("runtime jobs system registration is nil")
	}
	if hooks.loadJobsFromDB == nil {
		return fmt.Errorf("runtime jobs database loader is nil")
	}

	previousScheduler := app.JobScheduler
	runtimeScheduler := hooks.newScheduler()
	if runtimeScheduler == nil {
		return fmt.Errorf("runtime jobs scheduler is nil")
	}
	app.JobScheduler = runtimeScheduler

	cleanup := func() {
		runtimeScheduler.Stop()
		app.JobScheduler = previousScheduler
	}

	hooks.registerExecutors()
	if err := hooks.registerSystemJobs(); err != nil {
		cleanup()
		return fmt.Errorf("register runtime system jobs: %w", err)
	}
	if err := hooks.loadJobsFromDB(); err != nil {
		cleanup()
		return fmt.Errorf("load runtime jobs: %w", err)
	}

	runtimeJobsStarted = true
	return nil
}

// startLegacyRuntimeJobs preserves the legacy startup timing. Explicit
// standalone startup waits for the installation phase to complete before the
// root startup path calls StartRuntimeJobs.
func startLegacyRuntimeJobs() error {
	runtimeJobsMu.Lock()
	standalone := standalonePaths.Explicit
	runtimeJobsMu.Unlock()
	if standalone {
		return nil
	}
	return StartRuntimeJobs()
}

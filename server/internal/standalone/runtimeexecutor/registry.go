// Package runtimeexecutor coordinates plugin executor registration with the
// delayed creation of the standalone runtime scheduler.
package runtimeexecutor

import (
	"sync"

	"uvplatform.cn/uvp-gb28181/app/global/app"
)

// Registrar adds one or more executors to the supplied runtime scheduler.
type Registrar func(app.JobSchedulerInterf)

var (
	registryMu        sync.Mutex
	pendingRegistrars []Registrar
	appliedRegistrars int
	activeScheduler   app.JobSchedulerInterf
	registryCommitted bool
)

// Register queues a registrar until a runtime scheduler is available. Once
// the runtime has committed successfully, later registrations are applied
// immediately to that scheduler.
func Register(registrar Registrar) {
	if registrar == nil {
		return
	}

	registryMu.Lock()
	if registryCommitted && activeScheduler != nil {
		scheduler := activeScheduler
		registryMu.Unlock()
		registrar(scheduler)
		return
	}
	pendingRegistrars = append(pendingRegistrars, registrar)
	registryMu.Unlock()
}

// Apply invokes all currently queued registrars against scheduler. Registrars
// remain retained until Commit so a failed runtime startup can retry them.
func Apply(scheduler app.JobSchedulerInterf) {
	if scheduler == nil {
		return
	}

	registryMu.Lock()
	activeScheduler = scheduler
	registryCommitted = false
	appliedRegistrars = 0
	registryMu.Unlock()
	applyPending(scheduler)
}

// Commit makes scheduler the active target and drops the retained queue. The
// final drain closes the race where a registrar is queued between Apply and
// this call.
func Commit(scheduler app.JobSchedulerInterf) {
	if scheduler == nil {
		return
	}

	for {
		registryMu.Lock()
		if appliedRegistrars < len(pendingRegistrars) {
			registrars := append([]Registrar(nil), pendingRegistrars[appliedRegistrars:]...)
			appliedRegistrars = len(pendingRegistrars)
			registryMu.Unlock()
			for _, registrar := range registrars {
				registrar(scheduler)
			}
			continue
		}
		// Check for pending registrations and publish the active scheduler under
		// the same lock. A concurrent Register therefore either joins the final
		// drain above or observes the committed scheduler and runs immediately.
		pendingRegistrars = nil
		appliedRegistrars = 0
		activeScheduler = scheduler
		registryCommitted = true
		registryMu.Unlock()
		return
	}
}

// Rollback abandons the current scheduler while preserving queued registrars
// for the next startup attempt.
func Rollback() {
	registryMu.Lock()
	activeScheduler = nil
	appliedRegistrars = 0
	registryCommitted = false
	registryMu.Unlock()
}

func applyPending(scheduler app.JobSchedulerInterf) {
	for {
		registryMu.Lock()
		if appliedRegistrars >= len(pendingRegistrars) {
			registryMu.Unlock()
			return
		}
		registrars := append([]Registrar(nil), pendingRegistrars[appliedRegistrars:]...)
		appliedRegistrars = len(pendingRegistrars)
		registryMu.Unlock()

		for _, registrar := range registrars {
			registrar(scheduler)
		}
	}
}

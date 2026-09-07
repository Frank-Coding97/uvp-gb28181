// Package launcher coordinates readiness and ownership for the Windows package.
package launcher

import (
	"context"
	"errors"
	"fmt"
	"time"
)

type State string

const (
	Stopped       State = "Stopped"
	Preflight     State = "Preflight"
	RedisReady    State = "RedisReady"
	DatabaseReady State = "DatabaseReady"
	BackendReady  State = "BackendReady"
	MediaReady    State = "MediaReady"
	Ready         State = "Ready"
	Stopping      State = "Stopping"
	Failed        State = "Failed"
)

type Status struct {
	ManagementURL   string `json:"management_url,omitempty"`
	State           State  `json:"state"`
	SIPState        string `json:"sip_state,omitempty"`
	BusinessReady   bool   `json:"business_ready"`
	BusinessReason  string `json:"business_reason,omitempty"`
	PreviousUnclean bool   `json:"previous_unclean,omitempty"`
}

// BusinessStatus is the authenticated backend business-readiness snapshot
// observed after all launcher components are ready.
type BusinessStatus struct {
	SIPState       string
	BusinessReady  bool
	BusinessReason string
}

// Each step completes only after its concrete readiness checks. Cleanup owns
// only the handles created by this invocation, never processes found by PID.
type Steps struct {
	Preflight func(context.Context) error
	Redis     func(context.Context) error
	Database  func(context.Context) error
	Backend   func(context.Context) (string, error)
	Media     func(context.Context) error
	Exits     <-chan error
	Stop      func(context.Context) error
	Cleanup   func() error
	// ObserveBusiness starts an observer after Ready has been published. The
	// returned channel must close after ctx is canceled.
	ObserveBusiness func(context.Context) <-chan BusinessStatus
}

func Run(ctx context.Context, steps Steps, notify func(Status)) (result error) {
	status := Status{State: Stopped}
	graceful := false
	var stopObserver func() error
	stopBusinessObserver := func() error {
		if stopObserver == nil {
			return nil
		}
		stop := stopObserver
		stopObserver = nil
		return stop()
	}
	publish := func(state State) {
		status.State = state
		// Business readiness additionally requires the T19 node/Hook contract.
		// Component readiness alone must never advertise device readiness.
		status.BusinessReady = false
		if state == Failed {
			status.BusinessReason = "status_unavailable"
		}
		if state == Ready && status.BusinessReason == "" {
			status.BusinessReason = businessReasonForSIPState(status.SIPState)
		}
		if notify != nil {
			notify(status)
		}
	}
	defer func() {
		result = errors.Join(result, stopBusinessObserver())
		if steps.Cleanup != nil {
			result = errors.Join(result, steps.Cleanup())
		}
		if result != nil {
			publish(Failed)
		} else if graceful {
			publish(Stopped)
		}
	}()
	check := func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		select {
		case err, ok := <-steps.Exits:
			if !ok || err == nil {
				return errors.New("component exited unexpectedly")
			}
			return err
		default:
			return nil
		}
	}
	if err := check(); err != nil {
		return err
	}
	publish(Preflight)
	ordered := []struct {
		name  string
		ready State
		run   func(context.Context) error
	}{
		{"preflight", Preflight, steps.Preflight},
		{"redis", RedisReady, steps.Redis},
		{"database", DatabaseReady, steps.Database},
		{"backend", BackendReady, func(ctx context.Context) error {
			if steps.Backend == nil {
				return errors.New("backend readiness step is missing")
			}
			var err error
			status.SIPState, err = steps.Backend(ctx)
			return err
		}},
		{"media", MediaReady, steps.Media},
	}
	for _, step := range ordered {
		if err := check(); err != nil {
			return err
		}
		if step.run == nil {
			return fmt.Errorf("%s step is missing", step.name)
		}
		if err := step.run(ctx); err != nil {
			return fmt.Errorf("%s: %w", step.name, err)
		}
		if err := check(); err != nil {
			return err
		}
		if step.ready != Preflight {
			publish(step.ready)
		}
	}
	publish(Ready)
	var businessUpdates <-chan BusinessStatus
	if steps.ObserveBusiness != nil {
		observeCtx, cancelObserve := context.WithCancel(ctx)
		businessUpdates = steps.ObserveBusiness(observeCtx)
		stopObserver = func() error {
			return waitBusinessObserver(cancelObserve, businessUpdates)
		}
	}
	for {
		select {
		case update, ok := <-businessUpdates:
			if !ok {
				businessUpdates = nil
				continue
			}
			if update.BusinessReason == "" && !update.BusinessReady {
				update.BusinessReason = businessReasonForSIPState(update.SIPState)
			}
			if status.SIPState == update.SIPState && status.BusinessReady == update.BusinessReady && status.BusinessReason == update.BusinessReason {
				continue
			}
			status.SIPState = update.SIPState
			status.BusinessReady = update.BusinessReady
			status.BusinessReason = update.BusinessReason
			if notify != nil {
				notify(status)
			}
		case <-ctx.Done():
			observerErr := stopBusinessObserver()
			if steps.Stop == nil {
				return errors.Join(observerErr, ctx.Err())
			}
			publish(Stopping)
			stopCtx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			err := steps.Stop(stopCtx)
			cancel()
			if err != nil {
				return errors.Join(observerErr, fmt.Errorf("graceful stop: %w", err))
			}
			if observerErr != nil {
				return observerErr
			}
			graceful = true
			return nil
		case err, ok := <-steps.Exits:
			if !ok || err == nil {
				return errors.Join(stopBusinessObserver(), errors.New("component exited unexpectedly"))
			}
			return errors.Join(stopBusinessObserver(), err)
		}
	}
}

const businessObserverStopTimeout = 2 * time.Second

func waitBusinessObserver(cancel context.CancelFunc, updates <-chan BusinessStatus) error {
	if cancel == nil {
		return errors.New("business readiness observer cancel function is missing")
	}
	cancel()
	if updates == nil {
		return nil
	}
	deadline := time.NewTimer(businessObserverStopTimeout)
	defer deadline.Stop()
	for {
		select {
		case _, ok := <-updates:
			if !ok {
				return nil
			}
		case <-deadline.C:
			return errors.New("business readiness observer did not stop")
		}
	}
}

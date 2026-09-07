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
	PreviousUnclean bool   `json:"previous_unclean,omitempty"`
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
}

func Run(ctx context.Context, steps Steps, notify func(Status)) (result error) {
	status := Status{State: Stopped}
	graceful := false
	publish := func(state State) {
		status.State = state
		// Business readiness additionally requires the T19 node/Hook contract.
		// Component readiness alone must never advertise device readiness.
		status.BusinessReady = false
		if notify != nil {
			notify(status)
		}
	}
	defer func() {
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
	select {
	case <-ctx.Done():
		if steps.Stop == nil {
			return ctx.Err()
		}
		publish(Stopping)
		stopCtx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		if err := steps.Stop(stopCtx); err != nil {
			return fmt.Errorf("graceful stop: %w", err)
		}
		graceful = true
		return nil
	case err, ok := <-steps.Exits:
		if !ok || err == nil {
			return errors.New("component exited unexpectedly")
		}
		return err
	}
}

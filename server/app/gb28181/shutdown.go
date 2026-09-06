package gb28181

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
)

func shutdownComponentError(component string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", component, err)
}

type shutdownStep struct {
	name  string
	stop  func(context.Context) error
	eager bool
}

type shutdownGeneration struct {
	stopContext context.Context
	done        chan struct{}

	mu       sync.Mutex
	steps    []shutdownGenerationStep
	finalErr error
	finished bool
}

type shutdownGenerationStep struct {
	shutdownStep
	done      chan struct{}
	started   bool
	completed bool
	err       error
}

type shutdownWaitError struct {
	cause      error
	unfinished []string
}

func (e *shutdownWaitError) Error() string {
	return fmt.Sprintf("shutdown incomplete; unfinished components: %s: %v", strings.Join(e.unfinished, ", "), e.cause)
}

func (e *shutdownWaitError) Unwrap() error {
	return e.cause
}

func newShutdownGeneration(ctx context.Context, signal func(), steps []shutdownStep) *shutdownGeneration {
	if ctx == nil {
		ctx = context.Background()
	}

	generation := &shutdownGeneration{
		stopContext: ctx,
		done:        make(chan struct{}),
		steps:       make([]shutdownGenerationStep, len(steps)),
	}
	for i, step := range steps {
		generation.steps[i] = shutdownGenerationStep{
			shutdownStep: step,
			done:         make(chan struct{}),
		}
	}

	if signal != nil {
		signal()
	}
	for i := range generation.steps {
		if generation.steps[i].eager {
			generation.start(i)
		}
	}
	go generation.coordinate()
	return generation
}

func (g *shutdownGeneration) Done() <-chan struct{} {
	return g.done
}

func (g *shutdownGeneration) Wait(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}

	select {
	case <-g.done:
		return g.result()
	case <-ctx.Done():
		select {
		case <-g.done:
			return g.result()
		default:
		}
		return g.waitError(ctx.Err())
	}
}

func (g *shutdownGeneration) coordinate() {
	deadlineExpired := g.stopContext.Err() != nil
	for i := range g.steps {
		g.start(i)
		if deadlineExpired {
			continue
		}
		if !g.waitStep(i) {
			deadlineExpired = true
		}
	}

	for i := range g.steps {
		<-g.steps[i].done
	}

	var failures []error
	g.mu.Lock()
	for i := range g.steps {
		if g.steps[i].err != nil {
			failures = append(failures, shutdownComponentError(g.steps[i].name, g.steps[i].err))
		}
	}
	g.finalErr = errors.Join(failures...)
	g.finished = true
	close(g.done)
	g.mu.Unlock()
}

func (g *shutdownGeneration) start(index int) {
	g.mu.Lock()
	step := &g.steps[index]
	if step.started {
		g.mu.Unlock()
		return
	}
	step.started = true
	if step.stop == nil {
		step.completed = true
		close(step.done)
		g.mu.Unlock()
		return
	}
	stop := step.stop
	stopContext := g.stopContext
	g.mu.Unlock()

	go func() {
		err := stop(stopContext)
		g.mu.Lock()
		step.err = err
		step.completed = true
		close(step.done)
		g.mu.Unlock()
	}()
}

func (g *shutdownGeneration) waitStep(index int) bool {
	select {
	case <-g.steps[index].done:
		return true
	default:
	}
	select {
	case <-g.steps[index].done:
		return true
	case <-g.stopContext.Done():
		select {
		case <-g.steps[index].done:
			return true
		default:
			return false
		}
	}
}

func (g *shutdownGeneration) result() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.finalErr
}

func (g *shutdownGeneration) waitError(cause error) error {
	g.mu.Lock()
	if g.finished {
		err := g.finalErr
		g.mu.Unlock()
		return err
	}
	unfinished := make([]string, 0, len(g.steps))
	for i := range g.steps {
		if !g.steps[i].completed {
			unfinished = append(unfinished, g.steps[i].name)
		}
	}
	g.mu.Unlock()
	return &shutdownWaitError{cause: cause, unfinished: unfinished}
}

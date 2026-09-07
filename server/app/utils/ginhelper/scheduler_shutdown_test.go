package ginhelper

import (
	"context"
	"testing"
	"time"

	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/scheduler"
	"uvplatform.cn/uvp-gb28181/app/utils/schedulerhelper"
)

type drainingScheduler struct {
	app.JobSchedulerInterf
	results chan *schedulerhelper.JobResult
}

func (s *drainingScheduler) GetResults() <-chan *schedulerhelper.JobResult { return s.results }
func (s *drainingScheduler) Shutdown(context.Context) error                { close(s.results); return nil }
func TestShutdownSchedulerClosesProducerBeforeWaitingForConsumer(t *testing.T) {
	previous := app.JobScheduler
	defer func() { app.JobScheduler = previous }()
	app.JobScheduler = &drainingScheduler{results: make(chan *schedulerhelper.JobResult)}
	scheduler.StartResultHandler()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := ShutdownScheduler(ctx); err != nil {
		t.Fatal(err)
	}
}

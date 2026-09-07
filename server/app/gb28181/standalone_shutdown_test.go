package gb28181

import (
	"context"
	"errors"
	"testing"

	gbtalk "uvplatform.cn/uvp-gb28181/app/gb28181/talk"
)

type quiescingSIPRuntime struct {
	fakeSIPRuntimeServer
	drainErr error
}

func (s *quiescingSIPRuntime) DrainRequests(context.Context) error { return s.drainErr }

func TestQuiesceDrainFailurePreservesHandlerDependencies(t *testing.T) {
	oldSIP, oldTalk := sipServer, talkSvc
	defer func() { sipServer, talkSvc = oldSIP, oldTalk }()
	events := []string{}
	blocked := &quiescingSIPRuntime{fakeSIPRuntimeServer: fakeSIPRuntimeServer{events: &events}, drainErr: context.DeadlineExceeded}
	sipServer = blocked
	talkSvc = &gbtalk.Service{}
	originalTalk := talkSvc
	if err := Quiesce(context.Background()); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("quiesce err=%v", err)
	}
	if sipServer != blocked || talkSvc != originalTalk || len(events) != 0 {
		t.Fatal("dependencies cleared before admitted SIP handlers finished")
	}
}

package handler

import (
	"sync"

	"uvplatform.cn/uvp-gb28181/app/gb28181/metrics"
)

// fakeRecorder 测试用,只记录 Begin/End 调用,供断言
type fakeRecorder struct {
	mu       sync.Mutex
	begins   []metrics.Transaction
	ends     []fakeEnd
	pairOpen map[string]metrics.Transaction // callID:cseq → tx(用于回放配对结果)
}

type fakeEnd struct {
	CallID     string
	CSeq       string
	StatusCode int
	Success    bool
}

func newFakeRecorder() *fakeRecorder {
	return &fakeRecorder{pairOpen: make(map[string]metrics.Transaction)}
}

func (f *fakeRecorder) Begin(t metrics.Transaction) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.begins = append(f.begins, t)
	f.pairOpen[t.CallID+":"+t.CSeq] = t
}

func (f *fakeRecorder) End(callID, cseq string, statusCode int, success bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ends = append(f.ends, fakeEnd{CallID: callID, CSeq: cseq, StatusCode: statusCode, Success: success})
	delete(f.pairOpen, callID+":"+cseq)
}

func (f *fakeRecorder) Begins() []metrics.Transaction {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]metrics.Transaction, len(f.begins))
	copy(out, f.begins)
	return out
}

func (f *fakeRecorder) Ends() []fakeEnd {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]fakeEnd, len(f.ends))
	copy(out, f.ends)
	return out
}

func (f *fakeRecorder) LastEnd() (fakeEnd, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.ends) == 0 {
		return fakeEnd{}, false
	}
	return f.ends[len(f.ends)-1], true
}

var _ metrics.Recorder = (*fakeRecorder)(nil)

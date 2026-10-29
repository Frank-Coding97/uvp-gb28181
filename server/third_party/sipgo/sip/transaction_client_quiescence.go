package sip

// Quiesced closes after termination has stopped admitting new local work,
// every admitted Init/FSM/callback has returned, and the termination callback
// and connection-reference release attempt have returned. Unlike Done, this is
// a local work-lifetime boundary, NOT remote dialog closure or socket-close
// success. Work spawned independently by user callbacks is not covered.
//
// A callback may request Terminate, but must never wait on its own Quiesced
// channel. Waiters must live outside the callback/transaction work they await.
// This concrete extension deliberately does not widen ClientTransaction: an
// arbitrary implementation's Done must not be mistaken for this guarantee.
func (tx *ClientTx) Quiesced() <-chan struct{} { return tx.quiesced }

// lifeMu is a leaf lock: never acquire mu/fsmMu or invoke callbacks, timers,
// I/O or waits while holding it. The reverse fsmMu -> lifeMu short check is
// permitted. Admission and closure use the same lock; no WaitGroup Add/Wait
// race is possible when a previously queued response arrives during shutdown.
func (tx *ClientTx) beginWork() bool {
	tx.lifeMu.Lock()
	defer tx.lifeMu.Unlock()
	if tx.quiescing {
		return false
	}
	tx.activeWork++
	return true
}

func (tx *ClientTx) endWork() {
	tx.lifeMu.Lock()
	defer tx.lifeMu.Unlock()
	tx.activeWork--
	tx.closeQuiescedLocked()
}

func (tx *ClientTx) isQuiescing() bool {
	tx.lifeMu.Lock()
	defer tx.lifeMu.Unlock()
	return tx.quiescing
}

func (tx *ClientTx) beginTermination() bool {
	tx.lifeMu.Lock()
	defer tx.lifeMu.Unlock()
	if tx.quiescing {
		return false
	}
	tx.quiescing = true
	// Keep the external Terminate winner alive through its final FSM error
	// update, even if delete's cleanup tail has already returned.
	tx.activeWork++
	return true
}

func (tx *ClientTx) beginClosing() {
	tx.lifeMu.Lock()
	tx.quiescing = true
	tx.lifeMu.Unlock()
}

func (tx *ClientTx) finishCleanupTail() {
	tx.lifeMu.Lock()
	defer tx.lifeMu.Unlock()
	tx.cleanupTailDone = true
	tx.closeQuiescedLocked()
}

func (tx *ClientTx) closeQuiescedLocked() {
	if tx.quiescing && tx.cleanupTailDone && tx.activeWork == 0 && !tx.quiescedClosed {
		tx.quiescedClosed = true
		close(tx.quiesced)
	}
}

func (tx *ClientTx) withClientFSM(work func()) {
	if !tx.beginWork() {
		return
	}
	defer tx.endWork()
	tx.runClientFSM(work)
}

func (tx *ClientTx) runClientFSM(work func()) {
	tx.fsmMu.Lock()
	defer tx.fsmMu.Unlock()
	// A response may have entered before termination and waited for fsmMu.
	if tx.isQuiescing() {
		return
	}
	work()
}

// Shadow the embedded baseTx methods for client work only. Keeping the token
// through spinFsmUnsafe also covers passUpRetransmission's temporary FSM unlock.
func (tx *ClientTx) spinFsm(in fsmInput) {
	tx.withClientFSM(func() { tx.spinFsmUnsafe(in) })
}

func (tx *ClientTx) spinFsmWithResponse(in fsmInput, response *Response) {
	tx.withClientFSM(func() { tx.fsmResp = response; tx.spinFsmUnsafe(in) })
}

func (tx *ClientTx) spinFsmWithError(in fsmInput, err error) {
	tx.withClientFSM(func() { tx.fsmErr = err; tx.spinFsmUnsafe(in) })
}

func (tx *ClientTx) spinFsmWithErrorAsync(in fsmInput, err error) {
	// Register before scheduling, so a queued internal error continuation is
	// still owned when the originating write returns and shutdown races it.
	if !tx.beginWork() {
		return
	}
	go func() {
		defer tx.endWork()
		tx.runClientFSM(func() { tx.fsmErr = err; tx.spinFsmUnsafe(in) })
	}()
}

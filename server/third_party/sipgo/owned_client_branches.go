package sipgo

import "github.com/emiago/sipgo/sip"

const maxOwnedInviteBranches = 8

// A snapshot of observed 2xx branches, NOT proof that all remote branches have
// been observed. Even Incomplete=false and local Quiesced do not establish
// coverage after transaction removal, transport loss or process restart.
type OwnedInviteBranchSnapshot struct {
	Responses  []*sip.Response
	Incomplete bool
}

func cloneOwnedBranchResponse(r *sip.Response) *sip.Response {
	copy := r.Clone()
	copy.SetBody(append([]byte(nil), r.Body()...))
	detachOwnedPrimitiveHeaders(copy.Headers(), copy.ReplaceHeader)
	return copy
}

// Installed before Start. It records without waiting for SQL, an application
// consumer or ACK permission. Invalid/conflicting/overflow input never evicts
// accepted material and leaves a sticky incomplete flag.
func (o *OwnedClientInvite) observeBranch(response *sip.Response, retransmission bool) {
	if response == nil || response.StatusCode < 200 || response.StatusCode >= 300 {
		return
	}
	valid := len(response.Body()) <= 65536 && len(response.String()) <= 65536 && validOwnedInviteResponse(o.session.InviteRequest, response)
	o.mu.Lock()
	defer o.mu.Unlock()
	target, incomplete, changes := &o.branches, &o.branchesIncomplete, o.branchChanges
	if o.branchesFrozen {
		target, incomplete, changes = &o.quarantine, &o.quarantineIncomplete, o.quarantineChanges
	}
	changed := false
	if !valid {
		changed, *incomplete = !*incomplete, true
	} else {
		tag := ownedSingleTag(response.To().Params)
		found := false
		for _, old := range append(append([]*sip.Response(nil), o.branches...), o.quarantine...) {
			if ownedSingleTag(old.To().Params) != tag {
				continue
			}
			found = true
			if !sameOwnedInviteResponse(old, response) {
				changed, *incomplete = !*incomplete, true
			}
			break
		}
		if !found {
			if len(o.branches)+len(o.quarantine) == maxOwnedInviteBranches {
				changed, *incomplete = !*incomplete, true
			} else {
				*target = append(*target, cloneOwnedBranchResponse(response))
				changed = true
			}
		}
		// Preserve the old unsupported-response notification API. It is only a
		// hint; a full channel never loses the retained snapshot above.
		if retransmission && changed && !o.branchesFrozen && len(o.branches) > 1 {
			select {
			case o.unmatched <- cloneOwnedBranchResponse(response):
			default:
			}
		}
	}
	if changed {
		select {
		case changes <- struct{}{}:
		default:
		}
	}
}

func (o *OwnedClientInvite) ObservedBranches() OwnedInviteBranchSnapshot {
	o.mu.Lock()
	defer o.mu.Unlock()
	out := OwnedInviteBranchSnapshot{Incomplete: o.branchesIncomplete}
	for _, response := range o.branches {
		out.Responses = append(out.Responses, cloneOwnedBranchResponse(response))
	}
	return out
}

// BranchChanges is a coalesced wakeup, not a response queue. Always reload the
// retained snapshot; an empty notification channel says nothing about coverage.
func (o *OwnedClientInvite) BranchChanges() <-chan struct{} { return o.branchChanges }

// QuarantinedBranches contains only new facts after the active inventory was
// frozen. These observations never extend a previously authorized cleanup plan.
func (o *OwnedClientInvite) QuarantinedBranches() OwnedInviteBranchSnapshot {
	o.mu.Lock()
	defer o.mu.Unlock()
	out := OwnedInviteBranchSnapshot{Incomplete: o.quarantineIncomplete}
	for _, response := range o.quarantine {
		out.Responses = append(out.Responses, cloneOwnedBranchResponse(response))
	}
	return out
}

func (o *OwnedClientInvite) QuarantineChanges() <-chan struct{} { return o.quarantineChanges }

func (o *OwnedClientInvite) ObservationDone() <-chan struct{} {
	if o.responseObservation == nil {
		return nil
	}
	return o.responseObservation.Done()
}

// CloseObservation joins the private response observer without closing the
// shared UA or sending SIP. Stop and join original work first. Retained branch
// snapshots remain available and are marked incomplete by observation loss.
func (o *OwnedClientInvite) CloseObservation() {
	if o.responseObservation != nil {
		o.responseObservation.Close()
	}
}

type ownedInviteResponseSink struct{ owner *OwnedClientInvite }

func (s ownedInviteResponseSink) CaptureResponse(response *sip.Response) {
	s.owner.observeBranch(response, true)
}

func (s ownedInviteResponseSink) ObservationLost() {
	o := s.owner
	o.mu.Lock()
	defer o.mu.Unlock()
	incomplete, changes := &o.branchesIncomplete, o.branchChanges
	if o.branchesFrozen {
		incomplete, changes = &o.quarantineIncomplete, o.quarantineChanges
	}
	*incomplete = true
	select {
	case changes <- struct{}{}:
	default:
	}
}

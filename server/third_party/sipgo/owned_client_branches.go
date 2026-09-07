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
	changed := false
	if !valid {
		changed, o.branchesIncomplete = !o.branchesIncomplete, true
	} else {
		tag := ownedSingleTag(response.To().Params)
		found := false
		for _, old := range o.branches {
			if ownedSingleTag(old.To().Params) != tag {
				continue
			}
			found = true
			if !sameOwnedInviteResponse(old, response) {
				changed, o.branchesIncomplete = !o.branchesIncomplete, true
			}
			break
		}
		if !found {
			if len(o.branches) == maxOwnedInviteBranches {
				changed, o.branchesIncomplete = !o.branchesIncomplete, true
			} else {
				o.branches = append(o.branches, cloneOwnedBranchResponse(response))
				changed = true
			}
		}
		// Preserve the old unsupported-response notification API. It is only a
		// hint; a full channel never loses the retained snapshot above.
		if retransmission && changed {
			select {
			case o.unmatched <- cloneOwnedBranchResponse(response):
			default:
			}
		}
	}
	if changed {
		select {
		case o.branchChanges <- struct{}{}:
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

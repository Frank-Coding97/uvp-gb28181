package router

import "testing"

func TestRouteInitialRequiresTargetIdentitiesDomainAndFlow(t *testing.T) {
	candidate := initialCandidate(1, true, "local-1")
	request := InitialRequest{
		Method:   MethodMessage,
		Target:   SIPIdentity{ID: "local-1", Domain: "3402000000"},
		Local:    SIPIdentity{ID: "local-1", Domain: "3402000000"},
		Upstream: SIPIdentity{ID: "upstream-1", Domain: "3402000000"},
		Flow:     InboundFlow{LocalIP: "192.0.2.20", LocalPort: 5061, RemoteIP: "192.0.2.10", RemotePort: 5060, Transport: "udp"},
	}

	decision := RouteInitial(request, []InitialCandidate{candidate})
	if decision.Kind != DecisionCascade || decision.PlatformID != 1 {
		t.Fatalf("decision=%+v, want cascade platform 1", decision)
	}

	for name, mutate := range map[string]func(*InitialRequest){
		"target domain":     func(r *InitialRequest) { r.Target.Domain = "other-domain" },
		"local identity":    func(r *InitialRequest) { r.Local.ID = "other" },
		"upstream identity": func(r *InitialRequest) { r.Upstream.ID = "other" },
		"upstream domain":   func(r *InitialRequest) { r.Upstream.Domain = "other-domain" },
		"flow":              func(r *InitialRequest) { r.Flow.RemoteIP = "192.0.2.99" },
	} {
		t.Run(name, func(t *testing.T) {
			got := request
			mutate(&got)
			decision := RouteInitial(got, []InitialCandidate{candidate})
			if decision.Kind != DecisionReject || decision.Status != 403 {
				t.Fatalf("decision=%+v, want 403 reject", decision)
			}
		})
	}
}

func TestRouteInitialFallsBackOnlyWhenTargetIsNotCascadeOwned(t *testing.T) {
	candidate := initialCandidate(1, true, "local-1")
	message := RouteInitial(InitialRequest{Method: MethodMessage, Target: SIPIdentity{ID: "device-1", Domain: "3402000000"}}, []InitialCandidate{candidate})
	if message.Kind != DecisionDeviceFallback {
		t.Fatalf("message decision=%+v, want device fallback", message)
	}

	invite := RouteInitial(InitialRequest{Method: MethodInvite, Target: SIPIdentity{ID: "channel-1", Domain: "3402000000"}}, []InitialCandidate{candidate})
	if invite.Kind != DecisionBroadcastFallback {
		t.Fatalf("invite decision=%+v, want broadcast fallback", invite)
	}
}

func TestRouteInitialRejectsDisabledAndAmbiguousCandidates(t *testing.T) {
	request := InitialRequest{
		Method:   MethodInvite,
		Target:   SIPIdentity{ID: "channel-1", Domain: "3402000000"},
		Local:    SIPIdentity{ID: "local-1", Domain: "3402000000"},
		Upstream: SIPIdentity{ID: "upstream-1", Domain: "3402000000"},
		Flow:     InboundFlow{LocalIP: "192.0.2.20", LocalPort: 5061, RemoteIP: "192.0.2.10", RemotePort: 5060, Transport: "UDP"},
	}

	disabled := initialCandidate(1, false, "channel-1")
	if decision := RouteInitial(request, []InitialCandidate{disabled}); decision.Kind != DecisionReject || decision.Reason != RejectDisabledPlatform {
		t.Fatalf("disabled decision=%+v, want disabled reject", decision)
	}

	first := initialCandidate(1, true, "channel-1")
	second := initialCandidate(2, true, "channel-1")
	if decision := RouteInitial(request, []InitialCandidate{first, second}); decision.Kind != DecisionReject || decision.Reason != RejectAmbiguous {
		t.Fatalf("ambiguous decision=%+v, want ambiguity reject", decision)
	}
}

func TestRouteDialogRequiresExactEstablishedDialog(t *testing.T) {
	dialog := DialogRoute{PlatformID: 7, Key: DialogKey{CallID: "call-1", LocalTag: "local-tag", RemoteTag: "remote-tag"}, InviteCSeq: 42}
	for _, method := range []Method{MethodAck, MethodBye} {
		t.Run(string(method), func(t *testing.T) {
			decision := RouteDialog(DialogRequest{Method: method, Key: dialog.Key}, []DialogRoute{dialog})
			if decision.Kind != DecisionCascade || decision.PlatformID != 7 {
				t.Fatalf("decision=%+v, want cascade platform 7", decision)
			}
		})
	}

	// CANCEL belongs to the initial INVITE transaction, whose request commonly
	// has no UAS To tag yet. It must still be exact by Call-ID, From tag and CSeq.
	decision := RouteDialog(DialogRequest{Method: MethodCancel, Key: DialogKey{CallID: "call-1", RemoteTag: "remote-tag"}, CSeq: 42}, []DialogRoute{dialog})
	if decision.Kind != DecisionCascade || decision.PlatformID != 7 {
		t.Fatalf("cancel decision=%+v, want cascade platform 7", decision)
	}

	request := DialogRequest{Method: MethodBye, Key: dialog.Key}
	request.Key.RemoteTag = "wrong-tag"
	decision = RouteDialog(request, []DialogRoute{dialog})
	if decision.Kind != DecisionReject || decision.Status != 481 || decision.Reason != RejectDialogNotFound {
		t.Fatalf("missing dialog decision=%+v, want 481 dialog reject", decision)
	}
}

func TestRouteDialogRejectsCancelWithWrongCSeq(t *testing.T) {
	dialog := DialogRoute{PlatformID: 7, Key: DialogKey{CallID: "call-1", LocalTag: "local-tag", RemoteTag: "remote-tag"}, InviteCSeq: 42}
	decision := RouteDialog(DialogRequest{Method: MethodCancel, Key: DialogKey{CallID: "call-1", RemoteTag: "remote-tag"}, CSeq: 43}, []DialogRoute{dialog})
	if decision.Kind != DecisionReject || decision.Status != 481 || decision.Reason != RejectDialogNotFound {
		t.Fatalf("cancel decision=%+v, want 481 dialog reject", decision)
	}
}

func TestRouteDialogRejectsAmbiguousAndUnsupportedMethods(t *testing.T) {
	key := DialogKey{CallID: "call-1", LocalTag: "local-tag", RemoteTag: "remote-tag"}
	decision := RouteDialog(DialogRequest{Method: MethodAck, Key: key}, []DialogRoute{{PlatformID: 1, Key: key}, {PlatformID: 2, Key: key}})
	if decision.Kind != DecisionReject || decision.Reason != RejectAmbiguous {
		t.Fatalf("ambiguous decision=%+v, want ambiguity reject", decision)
	}

	decision = RouteDialog(DialogRequest{Method: MethodMessage, Key: key}, []DialogRoute{{PlatformID: 1, Key: key}})
	if decision.Kind != DecisionReject || decision.Status != 405 {
		t.Fatalf("unsupported decision=%+v, want 405 reject", decision)
	}
}

func initialCandidate(platformID uint64, enabled bool, targetID string) InitialCandidate {
	return InitialCandidate{
		PlatformID: platformID,
		Enabled:    enabled,
		Target:     SIPIdentity{ID: targetID, Domain: "3402000000"},
		Local:      SIPIdentity{ID: "local-1", Domain: "3402000000"},
		Upstream:   SIPIdentity{ID: "upstream-1", Domain: "3402000000"},
		Flow:       InboundFlow{LocalIP: "192.0.2.20", LocalPort: 5061, RemoteIP: "192.0.2.10", RemotePort: 5060, Transport: "UDP"},
	}
}

func TestSameIdentityRoutesByUpstreamPort(t *testing.T) {
	a := initialCandidate(1, true, "local-1")
	b := a
	b.PlatformID = 2
	a.Flow.RemotePort = 15060
	b.Flow.RemotePort = 16060
	for _, candidate := range []InitialCandidate{a, b} {
		request := InitialRequest{Method: MethodMessage, Target: candidate.Target, Local: candidate.Local, Upstream: candidate.Upstream, Flow: candidate.Flow}
		got := RouteInitial(request, []InitialCandidate{a, b})
		if got.Kind != DecisionCascade || got.PlatformID != candidate.PlatformID {
			t.Fatalf("wrong platform: %+v", got)
		}
	}
}

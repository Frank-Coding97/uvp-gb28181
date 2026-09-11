// Package router classifies inbound SIP traffic without parsing SIP or MANSCDP.
package router

import "strings"

type Method string

const (
	MethodMessage Method = "MESSAGE"
	MethodInvite  Method = "INVITE"
	MethodAck     Method = "ACK"
	MethodBye     Method = "BYE"
	MethodCancel  Method = "CANCEL"
)

type SIPIdentity struct {
	ID     string
	Domain string
}

// InboundFlow identifies the received SIP transport flow. It deliberately
// contains both endpoints: an IP address alone is never authorization.
type InboundFlow struct {
	LocalIP    string
	LocalPort  int
	RemoteIP   string
	RemotePort int
	Transport  string
}

type InitialRequest struct {
	Method   Method
	Target   SIPIdentity
	Local    SIPIdentity
	Upstream SIPIdentity
	Flow     InboundFlow
}

// InitialCandidate is an already resolved platform target. For MESSAGE the
// target is the platform LocalID; for INVITE it is a published resource ID.
// The caller owns projection lookups and SIP-header extraction.
type InitialCandidate struct {
	PlatformID uint64
	Enabled    bool
	Target     SIPIdentity
	Local      SIPIdentity
	Upstream   SIPIdentity
	Flow       InboundFlow
}

type DialogKey struct {
	CallID    string
	LocalTag  string
	RemoteTag string
}

type DialogRequest struct {
	Method Method
	Key    DialogKey
	CSeq   uint64
}

type DialogRoute struct {
	PlatformID uint64
	Key        DialogKey
	InviteCSeq uint64
}

type DecisionKind string

const (
	DecisionCascade           DecisionKind = "cascade"
	DecisionDeviceFallback    DecisionKind = "device_fallback"
	DecisionBroadcastFallback DecisionKind = "broadcast_fallback"
	DecisionReject            DecisionKind = "reject"
)

type RejectReason string

const (
	RejectUnauthorized      RejectReason = "unauthorized"
	RejectDisabledPlatform  RejectReason = "disabled_platform"
	RejectAmbiguous         RejectReason = "ambiguous"
	RejectDialogNotFound    RejectReason = "dialog_not_found"
	RejectMethodUnsupported RejectReason = "method_unsupported"
)

type Decision struct {
	Kind       DecisionKind
	PlatformID uint64
	Status     int
	Reason     RejectReason
}

// RouteInitial routes only initial MESSAGE and INVITE requests. It does not
// parse request bodies or load projection records.
func RouteInitial(request InitialRequest, candidates []InitialCandidate) Decision {
	switch normalizeMethod(request.Method) {
	case MethodMessage, MethodInvite:
	default:
		return reject(405, RejectMethodUnsupported)
	}

	targetOwned := false
	matching := make([]InitialCandidate, 0, 1)
	disabledMatches := false
	for _, candidate := range candidates {
		if sameID(request.Target.ID, candidate.Target.ID) {
			targetOwned = true
		}
		if !sameInitialIdentity(request, candidate) {
			continue
		}
		if !candidate.Enabled {
			disabledMatches = true
			continue
		}
		matching = append(matching, candidate)
	}

	if len(matching) > 1 {
		return reject(403, RejectAmbiguous)
	}
	if len(matching) == 1 {
		return Decision{Kind: DecisionCascade, PlatformID: matching[0].PlatformID}
	}
	if disabledMatches {
		return reject(403, RejectDisabledPlatform)
	}
	if targetOwned {
		return reject(403, RejectUnauthorized)
	}
	if normalizeMethod(request.Method) == MethodMessage {
		return Decision{Kind: DecisionDeviceFallback}
	}
	return Decision{Kind: DecisionBroadcastFallback}
}

// RouteDialog routes ACK and BYE by their complete dialog key. CANCEL is an
// early-dialog transaction and is matched by Call-ID, remote tag and its
// initial INVITE CSeq. A missing route is explicit 481 rather than a
// best-effort fallback.
func RouteDialog(request DialogRequest, dialogs []DialogRoute) Decision {
	method := normalizeMethod(request.Method)
	switch method {
	case MethodAck, MethodBye, MethodCancel:
	default:
		return reject(405, RejectMethodUnsupported)
	}

	matches := make([]DialogRoute, 0, 1)
	for _, dialog := range dialogs {
		if matchesDialogRequest(method, request, dialog) {
			matches = append(matches, dialog)
		}
	}
	if len(matches) == 0 {
		return reject(481, RejectDialogNotFound)
	}
	if len(matches) > 1 {
		return reject(403, RejectAmbiguous)
	}
	return Decision{Kind: DecisionCascade, PlatformID: matches[0].PlatformID}
}

func matchesDialogRequest(method Method, request DialogRequest, dialog DialogRoute) bool {
	if method == MethodCancel {
		return request.CSeq > 0 && request.CSeq == dialog.InviteCSeq &&
			sameID(request.Key.CallID, dialog.Key.CallID) &&
			sameID(request.Key.RemoteTag, dialog.Key.RemoteTag)
	}
	return sameDialogKey(request.Key, dialog.Key)
}

func sameInitialIdentity(request InitialRequest, candidate InitialCandidate) bool {
	return sameIdentity(request.Target, candidate.Target) &&
		sameIdentity(request.Local, candidate.Local) &&
		sameIdentity(request.Upstream, candidate.Upstream) &&
		sameFlow(request.Flow, candidate.Flow)
}

func sameIdentity(left, right SIPIdentity) bool {
	return sameID(left.ID, right.ID) && sameDomain(left.Domain, right.Domain)
}

func sameFlow(left, right InboundFlow) bool {
	return sameHost(left.LocalIP, right.LocalIP) &&
		left.LocalPort == right.LocalPort &&
		sameHost(left.RemoteIP, right.RemoteIP) &&
		left.RemotePort == right.RemotePort &&
		strings.EqualFold(strings.TrimSpace(left.Transport), strings.TrimSpace(right.Transport))
}

func sameDialogKey(left, right DialogKey) bool {
	return sameID(left.CallID, right.CallID) &&
		sameID(left.LocalTag, right.LocalTag) &&
		sameID(left.RemoteTag, right.RemoteTag)
}

func sameID(left, right string) bool {
	return strings.TrimSpace(left) != "" && strings.TrimSpace(left) == strings.TrimSpace(right)
}

func sameDomain(left, right string) bool {
	return strings.TrimSpace(left) != "" && strings.EqualFold(strings.TrimSpace(left), strings.TrimSpace(right))
}

func sameHost(left, right string) bool {
	return strings.TrimSpace(left) != "" && strings.EqualFold(strings.TrimSpace(left), strings.TrimSpace(right))
}

func normalizeMethod(method Method) Method {
	return Method(strings.ToUpper(strings.TrimSpace(string(method))))
}

func reject(status int, reason RejectReason) Decision {
	return Decision{Kind: DecisionReject, Status: status, Reason: reason}
}

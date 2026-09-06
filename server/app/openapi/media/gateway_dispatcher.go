package media

import (
	"context"
	"encoding/json"
	"io"
	"strings"

	"uvplatform.cn/uvp-gb28181/app/openapi/auth"
)

const (
	maxGatewayTicketBytes = 1024
	gatewayTicketVersion  = uint8(1)
)

// GatewayDispatcher is the narrow in-process handoff from auth admission to
// the live application. Its ticket is a value-owned envelope, not a cache key
// or a second authentication token.
type GatewayDispatcher struct {
	application *LiveApplication
}

var _ auth.MediaDispatcher = (*GatewayDispatcher)(nil)

type gatewayTicketEnvelope struct {
	Version uint8               `json:"version"`
	Target  auth.MediaTarget    `json:"target"`
	Ticket  QualificationTicket `json:"ticket"`
}

// NewGatewayDispatcher constructs the fixed application adapter. The auth
// gateway owns this instance for the process lifetime; there is no runtime
// setter or alternate ticket source.
func NewGatewayDispatcher(application *LiveApplication) *GatewayDispatcher {
	return &GatewayDispatcher{application: application}
}

// Ready is a dependency/configuration check only. It is not a node or media
// runtime qualification result.
func (dispatcher *GatewayDispatcher) Ready() bool {
	return dispatcher != nil && dispatcher.application != nil && dispatcher.application.Ready()
}

// Prepare obtains one fresh qualification ticket without starting media or
// touching admission, quota, nonce, or grant state.
func (dispatcher *GatewayDispatcher) Prepare(ctx context.Context, target auth.MediaTarget) (auth.MediaTicket, error) {
	if !dispatcher.Ready() {
		return "", ErrLiveApplicationUnavailable
	}
	ticket, err := dispatcher.application.Preflight(ctx, target.DeviceID, target.ChannelID, target.Protocol)
	if err != nil {
		return "", ErrLiveApplicationUnavailable
	}
	envelope := gatewayTicketEnvelope{
		Version: gatewayTicketVersion,
		Target:  target,
		Ticket:  ticket,
	}
	raw, err := json.Marshal(envelope)
	if err != nil || len(raw) > maxGatewayTicketBytes {
		return "", ErrLiveApplicationUnavailable
	}
	return auth.MediaTicket(string(raw)), nil
}

// Apply verifies the internal target binding before handing the immutable
// ticket to LiveApplication. Any malformed or rebound admitted request uses
// the same bounded FailUnboundGrant compensation path as application failure.
func (dispatcher *GatewayDispatcher) Apply(ctx context.Context, request auth.MediaAdmittedRequest) (auth.MediaAuthorization, error) {
	if dispatcher == nil || dispatcher.application == nil {
		return auth.MediaAuthorization{}, ErrLiveApplicationUnavailable
	}
	envelope, err := decodeGatewayTicket(request.Ticket)
	if err != nil || envelope.Version != gatewayTicketVersion || envelope.Target != request.Target || envelope.Ticket.Protocol != request.Target.Protocol || !validApplicationProtocol(request.Target.Protocol) {
		return dispatcher.failAdmission(ctx, request)
	}

	data, err := dispatcher.application.Apply(ctx, ApplyRequest{
		ClientID:  request.ClientID,
		GrantID:   request.GrantID,
		DeviceID:  request.Target.DeviceID,
		ChannelID: request.Target.ChannelID,
		Ticket:    envelope.Ticket,
	})
	if err != nil {
		return auth.MediaAuthorization{}, err
	}
	return auth.MediaAuthorization{
		AuthorizationID: data.AuthorizationID,
		Protocol:        data.Protocol,
		URL:             data.URL,
		ExpiresAt:       data.ExpiresAt,
	}, nil
}

func decodeGatewayTicket(raw auth.MediaTicket) (gatewayTicketEnvelope, error) {
	if len(raw) == 0 || len(raw) > maxGatewayTicketBytes {
		return gatewayTicketEnvelope{}, ErrLiveApplicationUnavailable
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	var envelope gatewayTicketEnvelope
	if err := decoder.Decode(&envelope); err != nil {
		return gatewayTicketEnvelope{}, ErrLiveApplicationUnavailable
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return gatewayTicketEnvelope{}, ErrLiveApplicationUnavailable
	}
	return envelope, nil
}

func (dispatcher *GatewayDispatcher) failAdmission(ctx context.Context, request auth.MediaAdmittedRequest) (auth.MediaAuthorization, error) {
	if dispatcher == nil || dispatcher.application == nil {
		return auth.MediaAuthorization{}, ErrLiveApplicationUnavailable
	}
	applyRequest := ApplyRequest{
		ClientID:  request.ClientID,
		GrantID:   request.GrantID,
		DeviceID:  request.Target.DeviceID,
		ChannelID: request.Target.ChannelID,
	}
	if !validApplyIdentifiers(applyRequest) {
		return auth.MediaAuthorization{}, ErrLiveApplicationUnavailable
	}
	_, err := dispatcher.application.failApply(ctx, applyRequest)
	return auth.MediaAuthorization{}, err
}

package ptz

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"uvplatform.com/uvp-gb28181/app/gb28181/manscdp"
	"uvplatform.com/uvp-gb28181/app/gb28181/protocol"
	runtimeptz "uvplatform.com/uvp-gb28181/app/gb28181/ptz"
	"uvplatform.com/uvp-gb28181/app/openapi/auth"
)

const (
	LiveKeyframeScope = "live:keyframe"
	PTZDragZoomScope  = "ptz:zoom:drag"
)

type dragZoomInput struct {
	Direction string               `json:"direction"`
	Region    *dragZoomRegionInput `json:"region"`
}

type dragZoomRegionInput struct {
	Length    *int `json:"length"`
	Width     *int `json:"width"`
	MidPointX *int `json:"midPointX"`
	MidPointY *int `json:"midPointY"`
	LengthX   *int `json:"lengthX"`
	LengthY   *int `json:"lengthY"`
}

func (input *dragZoomRegionInput) region() (*manscdp.DragZoomRegion, error) {
	if input == nil || input.Length == nil || input.Width == nil || input.MidPointX == nil || input.MidPointY == nil || input.LengthX == nil || input.LengthY == nil {
		return nil, auth.ErrPTZInvalid
	}
	return &manscdp.DragZoomRegion{Length: *input.Length, Width: *input.Width, MidPointX: *input.MidPointX, MidPointY: *input.MidPointY, LengthX: *input.LengthX, LengthY: *input.LengthY}, nil
}

func extraControlClientPrefix(clientID int64) string {
	return fmt.Sprintf("openapi-client:%d:", clientID)
}
func extraControlKey(request auth.PTZRequest) string {
	sum := sha256.Sum256([]byte(request.Scope + "\x00" + request.IdempotencyKey))
	return extraControlClientPrefix(request.ClientID) + hex.EncodeToString(sum[:])
}
func isExtraControlAction(action string) bool {
	return action == "iframe" || action == "drag_zoom_in" || action == "drag_zoom_out"
}
func extraControlOwnedBy(request auth.PTZRequest, key string) bool {
	return request.ClientID > 0 && strings.HasPrefix(key, extraControlClientPrefix(request.ClientID))
}

func (d *Dispatcher) extraControl(ctx context.Context, request auth.PTZRequest) (any, error) {
	if request.ClientID <= 0 || !validControlIdempotencyKey(request.IdempotencyKey) {
		return nil, auth.ErrPTZInvalid
	}
	action := "iframe"
	protocolAction := protocol.ActionIFrame
	var region *manscdp.DragZoomRegion
	direction := manscdp.DragZoomIn
	if request.Scope == LiveKeyframeScope {
		var input struct{}
		if decodeStrictBody(request.Body, &input) != nil {
			return nil, auth.ErrPTZInvalid
		}
	} else {
		var input dragZoomInput
		if decodeStrictBody(request.Body, &input, "direction", "region") != nil || input.Region == nil {
			return nil, auth.ErrPTZInvalid
		}
		switch input.Direction {
		case "in":
			action = "drag_zoom_in"
		case "out":
			action = "drag_zoom_out"
			direction = manscdp.DragZoomOut
		default:
			return nil, auth.ErrPTZInvalid
		}
		protocolAction = protocol.ActionDragZoom
		var regionErr error
		region, regionErr = input.Region.region()
		if regionErr != nil {
			return nil, regionErr
		}
		// Reuse the protocol builder's geometry validation before enqueuing an
		// operation. Invalid boxes must not produce either a row or SIP traffic.
		if _, err := manscdp.BuildDragZoomControlWithProfile(protocol.ProfileFor(protocol.Version2016), request.ChannelID, 1, manscdp.DragZoomCommand{Direction: direction, Region: *region}); err != nil {
			return nil, auth.ErrPTZInvalid
		}
	}
	target, _, err := d.resolveTarget(ctx, request)
	if err != nil {
		return nil, mapTargetError(err)
	}
	payload := map[string]any{"action": action}
	if region != nil {
		payload["region"] = *region
	}
	operation, err := d.service.Execute(ctx, target, runtimeptz.Command{
		CmdType: manscdp.CmdDeviceControl, Action: action, IdempotencyKey: extraControlKey(request),
		Profile: target.Profile, TargetScope: "channel", TargetCode: target.ChannelCode,
		ResponseRequired: target.Profile.ResponseFor(protocolAction).ResponseRequired, MaxAttempts: 1,
		ActorDeptID: request.OwnerDeptID, Payload: payload,
		Build: func(sn int) ([]byte, error) {
			if region == nil {
				return manscdp.BuildIFrameControlWithProfile(target.Profile, target.ChannelCode, sn)
			}
			return manscdp.BuildDragZoomControlWithProfile(target.Profile, target.ChannelCode, sn, manscdp.DragZoomCommand{Direction: direction, Region: *region})
		},
	})
	if err != nil {
		return nil, mapOperationError(err)
	}
	return controlOperationResponse(operation), nil
}

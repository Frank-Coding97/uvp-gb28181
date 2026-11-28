package ptz

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
	"uvplatform.com/uvp-gb28181/app/openapi/auth"
	"uvplatform.com/uvp-gb28181/app/openapi/resource"
)

func extraControlRequest(scope, key, body string) auth.PTZRequest {
	return auth.PTZRequest{Scope: scope, ClientID: 41, OwnerDeptID: 10, DataScope: resource.DataScopeDepartment, DeviceID: ptzTestDevice, ChannelID: ptzTestChannel, IdempotencyKey: key, Body: []byte(body)}
}
func TestExtraControlsUseRealProtocolAndClientOwnedIdempotency(t *testing.T) {
	sender := &dispatcherSender{}
	db, d := newDispatcherFixture(t, sender)
	ctx := context.Background()
	keyframe := extraControlRequest("live:keyframe", "same-key", `{}`)
	data, err := d.Handle(ctx, keyframe)
	require.NoError(t, err)
	view := data.(controlOperationView)
	require.Equal(t, "iframe", view.Action)
	require.Equal(t, "sent_result_unconfirmed", view.Result)
	require.False(t, view.DeviceAcknowledged)
	require.False(t, view.ResponseRequired)
	require.Contains(t, string(sender.body), "<IFameCmd>Send</IFameCmd>")
	var op gbmodels.GbPTZOperation
	require.NoError(t, db.Where("operation_id = ?", view.OperationID).First(&op).Error)
	require.True(t, strings.HasPrefix(op.IdempotencyKey, "openapi-client:41:"))
	repeat, err := d.Handle(ctx, keyframe)
	require.NoError(t, err)
	require.Equal(t, view.OperationID, repeat.(controlOperationView).OperationID)
	require.Equal(t, 1, sender.calls)
	keyframe.ClientID = 42
	other, err := d.Handle(ctx, keyframe)
	require.NoError(t, err)
	require.NotEqual(t, view.OperationID, other.(controlOperationView).OperationID)
	require.Equal(t, 2, sender.calls)
	read := keyframe
	read.Scope = auth.PTZOperationReadScope
	read.OperationID = view.OperationID
	_, err = d.Handle(ctx, read)
	require.ErrorIs(t, err, auth.ErrPTZNotFound)
	read.ClientID = 41
	_, err = d.Handle(ctx, read)
	require.NoError(t, err)
	require.NoError(t, db.Table("gb_device").Where("device_id = ?", ptzTestDevice).Update("effective_version", "2022").Error)
	keyframe.ClientID = 41
	keyframe.IdempotencyKey = "profile-2022"
	_, err = d.Handle(ctx, keyframe)
	require.NoError(t, err)
	require.Contains(t, string(sender.body), "<IFrameCmd>Send</IFrameCmd>")
	drag := extraControlRequest("ptz:zoom:drag", "drag-1", `{"direction":"in","region":{"length":1920,"width":1080,"midPointX":960,"midPointY":540,"lengthX":640,"lengthY":360}}`)
	data, err = d.Handle(ctx, drag)
	require.NoError(t, err)
	require.Equal(t, "drag_zoom_in", data.(controlOperationView).Action)
	require.Contains(t, string(sender.body), "<DragZoomIn>")
	require.Contains(t, string(sender.body), "<MidPointX>960</MidPointX>")
	require.False(t, data.(controlOperationView).DeviceAcknowledged)
	calls := sender.calls
	_, err = d.Handle(ctx, drag)
	require.NoError(t, err)
	require.Equal(t, calls, sender.calls)
	drag.Body = []byte(strings.Replace(string(drag.Body), `"in"`, `"out"`, 1))
	_, err = d.Handle(ctx, drag)
	require.ErrorIs(t, err, auth.ErrPTZConflict)
	require.Equal(t, calls, sender.calls)
	drag.IdempotencyKey = "drag-out"
	_, err = d.Handle(ctx, drag)
	require.NoError(t, err)
	require.Contains(t, string(sender.body), "<DragZoomOut>")
}
func TestExtraControlsRejectMalformedRegionScopeAndMissingIdentity(t *testing.T) {
	sender := &dispatcherSender{}
	db, d := newDispatcherFixture(t, sender)
	ctx := context.Background()
	for _, body := range []string{`{}`, `{"direction":"in","region":{"length":10,"width":10,"midPointY":1,"lengthX":1,"lengthY":1}}`, `{"direction":"in","region":{"length":10,"width":10,"midPointX":null,"midPointY":1,"lengthX":1,"lengthY":1}}`, `{"direction":"in"}`, `{"direction":"out","region":null}`, `{"direction":"left","region":{"length":10,"width":10,"midPointX":5,"midPointY":5,"lengthX":2,"lengthY":2}}`, `{"direction":"in","region":{"length":10,"width":10,"midPointX":9,"midPointY":5,"lengthX":4,"lengthY":2}}`, `{"direction":"in","region":{"length":10,"width":10,"midPointX":5,"midPointY":5,"lengthX":2,"lengthY":2,"extra":1}}`, `{"direction":"in","region":{"length":10,"width":10,"midPointX":5,"midPointY":5,"lengthX":2,"lengthY":2,"lengthX":3}}`} {
		_, err := d.Handle(ctx, extraControlRequest("ptz:zoom:drag", "invalid", body))
		require.ErrorIs(t, err, auth.ErrPTZInvalid, body)
	}
	for _, body := range []string{`null`, `{"unknown":true}`, `{"region":{}}`} {
		_, err := d.Handle(ctx, extraControlRequest("live:keyframe", "invalid", body))
		require.ErrorIs(t, err, auth.ErrPTZInvalid)
	}
	req := extraControlRequest("live:keyframe", "", `{}`)
	_, err := d.Handle(ctx, req)
	require.ErrorIs(t, err, auth.ErrPTZInvalid)
	req.IdempotencyKey = "valid"
	req.ClientID = 0
	_, err = d.Handle(ctx, req)
	require.ErrorIs(t, err, auth.ErrPTZInvalid)
	req.ClientID = 41
	req.OwnerDeptID = 20
	_, err = d.Handle(ctx, req)
	require.ErrorIs(t, err, auth.ErrPTZNotFound)
	require.Zero(t, sender.calls)
	req.OwnerDeptID = 10
	require.NoError(t, db.Table("gb_channel").Where("channel_id = ?", ptzTestChannel).Update("owner_dept_id", 20).Error)
	_, err = d.Handle(ctx, req)
	require.ErrorIs(t, err, auth.ErrPTZNotFound)
	require.Zero(t, sender.calls)
}

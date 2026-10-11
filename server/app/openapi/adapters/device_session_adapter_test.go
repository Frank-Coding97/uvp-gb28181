package adapters

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	catalogruntime "uvplatform.com/uvp-gb28181/app/openapi/catalog/runtime"
	devicesessions "uvplatform.com/uvp-gb28181/app/openapi/device_sessions"
	"uvplatform.com/uvp-gb28181/app/openapi/resource"
)

const (
	deviceSessionTestDevice  = "34020000002000000010"
	deviceSessionTestChannel = "37011200001310000010"
	deviceSessionTestID      = "a4376e54-bc6f-4d99-8ae2-b25de0cf58a4"
)

type deviceSessionServiceStub struct {
	createRequest devicesessions.CaptureRequest
	getClientID   int64
	imageClientID int64
	imageBytes    []byte
	createErr     error
	getErr        error
	imageErr      error
}

func (stub *deviceSessionServiceStub) CreateCapture(_ context.Context, request devicesessions.CaptureRequest) (devicesessions.CaptureSession, error) {
	stub.createRequest = request
	return devicesessions.CaptureSession{SessionID: deviceSessionTestID}, stub.createErr
}

func (stub *deviceSessionServiceStub) GetCapture(_ context.Context, clientID int64, _ resource.DepartmentScope, _, _, _ string) (devicesessions.CaptureSession, error) {
	stub.getClientID = clientID
	return devicesessions.CaptureSession{SessionID: deviceSessionTestID}, stub.getErr
}

func (stub *deviceSessionServiceStub) ReadImage(_ context.Context, clientID int64, _ resource.DepartmentScope, _, _, _, _ string) (devicesessions.ImageContent, error) {
	stub.imageClientID = clientID
	return devicesessions.ImageContent{FileName: "frame.jpg", ContentType: "image/jpeg", MD5: "abc123", Bytes: stub.imageBytes}, stub.imageErr
}

func TestDeviceSessionAdapterCreateUsesVerifiedClientAndSignedBody(t *testing.T) {
	service := &deviceSessionServiceStub{}
	adapter := newDeviceSessionAdapters(service)[0]
	body := []byte(`{"snapNum":2,"interval":3}`)
	request := Request{
		ClientID: 91, IdempotencyKey: "capture-once",
		ResourceScope: resource.DepartmentScope{OwnerDeptID: 10, DataScope: resource.DataScopeDepartment},
	}
	data, err := adapter.Execute(context.Background(), catalogruntime.Invocation{
		Scope: CaptureCreateScope, Method: "POST", Params: map[string]string{"deviceId": deviceSessionTestDevice, "channelId": deviceSessionTestChannel},
		Body: body, Value: request,
	})
	require.NoError(t, err)
	require.Equal(t, deviceSessionTestID, data.(devicesessions.CaptureSession).SessionID)
	require.Equal(t, int64(91), service.createRequest.ClientID)
	require.Equal(t, "capture-once", service.createRequest.IdempotencyKey)
	require.Equal(t, 2, service.createRequest.SnapNum)
	require.Equal(t, 3, service.createRequest.Interval)
	require.Equal(t, deviceSessionTestDevice, service.createRequest.DeviceID)
	require.Equal(t, deviceSessionTestChannel, service.createRequest.ChannelID)
}

func TestDeviceSessionAdapterRejectsMalformedCreateBeforeCallingService(t *testing.T) {
	service := &deviceSessionServiceStub{}
	adapter := newDeviceSessionAdapters(service)[0]
	data, err := adapter.Execute(context.Background(), catalogruntime.Invocation{
		Scope: CaptureCreateScope, Method: "POST", Params: map[string]string{"deviceId": deviceSessionTestDevice, "channelId": deviceSessionTestChannel},
		Body:  []byte(`{"snapNum":1,"interval":1,"extra":true}`),
		Value: Request{ClientID: 91, IdempotencyKey: "capture-once", ResourceScope: resource.DepartmentScope{OwnerDeptID: 10, DataScope: resource.DataScopeDepartment}},
	})
	require.ErrorIs(t, err, ErrInvalidRequest)
	require.Nil(t, data)
	require.Zero(t, service.createRequest.ClientID)
}

func TestDeviceSessionAdapterScopesSessionReadsAndEncodesImageContent(t *testing.T) {
	scope := resource.DepartmentScope{OwnerDeptID: 10, DataScope: resource.DataScopeDepartment}
	service := &deviceSessionServiceStub{imageBytes: []byte{0xff, 0xd8, 0xff, 0xd9}}
	request := Request{ClientID: 91, ResourceScope: scope}
	params := map[string]string{"deviceId": deviceSessionTestDevice, "channelId": deviceSessionTestChannel, "sessionId": deviceSessionTestID}

	readAdapter := newDeviceSessionAdapters(service)[1]
	_, err := readAdapter.Execute(context.Background(), catalogruntime.Invocation{Scope: CaptureReadScope, Method: "GET", Params: params, Value: request})
	require.NoError(t, err)
	require.Equal(t, int64(91), service.getClientID)

	imageAdapter := newDeviceSessionAdapters(service)[2]
	params["fileName"] = "frame.jpg"
	data, err := imageAdapter.Execute(context.Background(), catalogruntime.Invocation{Scope: CaptureImageReadScope, Method: "GET", Params: params, Value: request})
	require.NoError(t, err)
	var image struct {
		FileName      string `json:"fileName"`
		ContentType   string `json:"contentType"`
		Size          int    `json:"size"`
		MD5           string `json:"md5"`
		ContentBase64 string `json:"contentBase64"`
	}
	raw, err := json.Marshal(data)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(raw, &image))
	require.Equal(t, "frame.jpg", image.FileName)
	require.Equal(t, "image/jpeg", image.ContentType)
	require.Equal(t, len(service.imageBytes), image.Size)
	require.Equal(t, base64.StdEncoding.EncodeToString(service.imageBytes), image.ContentBase64)
	require.Equal(t, int64(91), service.imageClientID)
}

func TestDeviceSessionAdapterNormalizesDomainResourceFailures(t *testing.T) {
	service := &deviceSessionServiceStub{getErr: devicesessions.ErrNotFound}
	adapter := newDeviceSessionAdapters(service)[1]
	_, err := adapter.Execute(context.Background(), catalogruntime.Invocation{
		Scope: CaptureReadScope, Method: "GET",
		Params: map[string]string{"deviceId": deviceSessionTestDevice, "channelId": deviceSessionTestChannel, "sessionId": deviceSessionTestID},
		Value:  Request{ClientID: 91, ResourceScope: resource.DepartmentScope{OwnerDeptID: 10, DataScope: resource.DataScopeDepartment}},
	})
	require.ErrorIs(t, err, ErrNotFound)
}

func TestDeviceSessionAdapterPreservesConflictOfflineAndUnsupportedErrors(t *testing.T) {
	tests := []struct {
		name      string
		domainErr error
		want      error
	}{
		{name: "idempotency conflict", domainErr: devicesessions.ErrConflict, want: ErrConflict},
		{name: "offline target", domainErr: devicesessions.ErrDeviceOffline, want: ErrDeviceOffline},
		{name: "unsupported protocol", domainErr: devicesessions.ErrCapabilityNotSupported, want: ErrCapabilityNotSupported},
		{name: "unsupported stop", domainErr: devicesessions.ErrStopUnsupported, want: ErrCapabilityNotSupported},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service := &deviceSessionServiceStub{createErr: test.domainErr}
			adapter := newDeviceSessionAdapters(service)[0]
			_, err := adapter.Execute(context.Background(), catalogruntime.Invocation{
				Scope: CaptureCreateScope, Method: "POST",
				Params: map[string]string{"deviceId": deviceSessionTestDevice, "channelId": deviceSessionTestChannel},
				Body:   []byte(`{"snapNum":1,"interval":1}`),
				Value:  Request{ClientID: 91, IdempotencyKey: "capture-errors", ResourceScope: resource.DepartmentScope{OwnerDeptID: 10, DataScope: resource.DataScopeDepartment}},
			})
			require.ErrorIs(t, err, test.want)
			require.Equal(t, ErrorCode(test.want), ErrorCode(err))
			require.Equal(t, HTTPStatus(test.want), HTTPStatus(err))
		})
	}
}

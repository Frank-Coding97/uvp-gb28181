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

	libraryOptions  devicesessions.LibraryListOptions
	libraryDeviceID string
	libraryChannel  string
	libraryIDs      []uint64
	libraryErr      error
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

func (stub *deviceSessionServiceStub) ListLibrary(_ context.Context, _ resource.DepartmentScope, deviceID, channelID string, options devicesessions.LibraryListOptions) (devicesessions.LibraryPage, error) {
	stub.libraryDeviceID, stub.libraryChannel, stub.libraryOptions = deviceID, channelID, options
	return devicesessions.LibraryPage{Total: 1, Page: 1, PageSize: 20, Items: []devicesessions.LibraryImage{{SnapshotID: 7, FileName: "frame.jpg"}}}, stub.libraryErr
}

func (stub *deviceSessionServiceStub) ReadLibraryImage(_ context.Context, _ resource.DepartmentScope, deviceID, channelID string, snapshotID uint64) (devicesessions.ImageContent, error) {
	stub.libraryDeviceID, stub.libraryChannel = deviceID, channelID
	stub.libraryIDs = []uint64{snapshotID}
	return devicesessions.ImageContent{FileName: "frame.jpg", ContentType: "image/jpeg", MD5: "abc123", Bytes: stub.imageBytes}, stub.libraryErr
}

func (stub *deviceSessionServiceStub) DeleteLibraryImages(_ context.Context, _ resource.DepartmentScope, deviceID, channelID string, ids []uint64) (devicesessions.LibraryDeleteResult, error) {
	stub.libraryDeviceID, stub.libraryChannel, stub.libraryIDs = deviceID, channelID, ids
	return devicesessions.LibraryDeleteResult{Deleted: len(ids)}, stub.libraryErr
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

func TestDeviceSessionLibraryAdaptersBindChannelScopeAndFilters(t *testing.T) {
	scope := resource.DepartmentScope{OwnerDeptID: 10, DataScope: resource.DataScopeDepartment}
	service := &deviceSessionServiceStub{}
	params := map[string]string{"deviceId": deviceSessionTestDevice, "channelId": deviceSessionTestChannel}
	adaptersList := newDeviceSessionAdapters(service)

	listData, err := adaptersList[3].Execute(context.Background(), catalogruntime.Invocation{
		Scope: LibraryReadScope, Method: "GET", Params: params,
		Query: map[string]string{"page": "2", "pageSize": "50", "from": "2026-10-20T08:00:00Z", "to": "2026-10-20T09:00:00Z", "source": "device"},
		Value: Request{ClientID: 91, ResourceScope: scope},
	})
	require.NoError(t, err)
	require.Equal(t, deviceSessionTestDevice, service.libraryDeviceID)
	require.Equal(t, deviceSessionTestChannel, service.libraryChannel)
	require.Equal(t, 2, service.libraryOptions.Page)
	require.Equal(t, 50, service.libraryOptions.PageSize)
	require.Equal(t, "device", service.libraryOptions.Source)
	require.Equal(t, int64(1), listData.(devicesessions.LibraryPage).Total)

	// 列表是 GET，不得携带请求体；pageSize 上限由适配器自己再挡一次。
	_, err = adaptersList[3].Execute(context.Background(), catalogruntime.Invocation{
		Scope: LibraryReadScope, Method: "GET", Params: params, Body: []byte(`{}`),
		Value: Request{ClientID: 91, ResourceScope: scope},
	})
	require.ErrorIs(t, err, ErrInvalidRequest)
	_, err = adaptersList[3].Execute(context.Background(), catalogruntime.Invocation{
		Scope: LibraryReadScope, Method: "GET", Params: params,
		Query: map[string]string{"pageSize": "101"}, Value: Request{ClientID: 91, ResourceScope: scope},
	})
	require.ErrorIs(t, err, ErrInvalidRequest)

	// 取图：snapshotId 是库主键（十进制正整数），不是 20 位国标编码。
	contentParams := map[string]string{"deviceId": deviceSessionTestDevice, "channelId": deviceSessionTestChannel, "snapshotId": "42"}
	imageData, err := adaptersList[4].Execute(context.Background(), catalogruntime.Invocation{
		Scope: LibraryImageReadScope, Method: "GET", Params: contentParams, Value: Request{ClientID: 91, ResourceScope: scope},
	})
	require.NoError(t, err)
	require.Equal(t, []uint64{42}, service.libraryIDs)
	raw, err := json.Marshal(imageData)
	require.NoError(t, err)
	var image struct {
		FileName      string `json:"fileName"`
		ContentType   string `json:"contentType"`
		Size          int    `json:"size"`
		MD5           string `json:"md5"`
		ContentBase64 string `json:"contentBase64"`
	}
	require.NoError(t, json.Unmarshal(raw, &image))
	require.Equal(t, "frame.jpg", image.FileName)
	for _, invalid := range []string{"", "0", "abc", "34020000001320000001"} {
		contentParams["snapshotId"] = invalid
		_, err := adaptersList[4].Execute(context.Background(), catalogruntime.Invocation{
			Scope: LibraryImageReadScope, Method: "GET", Params: contentParams, Value: Request{ClientID: 91, ResourceScope: scope},
		})
		require.ErrorIs(t, err, ErrInvalidRequest, "snapshotId %q must be rejected", invalid)
	}
}

func TestDeviceSessionLibraryDeleteRequiresExplicitConfirmation(t *testing.T) {
	service := &deviceSessionServiceStub{}
	adapter := newDeviceSessionAdapters(service)[5]
	params := map[string]string{"deviceId": deviceSessionTestDevice, "channelId": deviceSessionTestChannel}
	value := Request{ClientID: 91, ResourceScope: resource.DepartmentScope{OwnerDeptID: 10, DataScope: resource.DataScopeDepartment}}

	// ⛔ 未确认 ⇒ 什么都不删。这条是"不可恢复操作"的唯一防线，删掉它就是允许一句
	// 手滑删掉整个通道的取证图。
	for _, body := range []string{
		`{"ids":[1]}`,
		`{"ids":[1],"confirmed":false}`,
		`{"ids":[],"confirmed":true}`,
		`{"ids":[1],"confirmed":true,"extra":1}`,
	} {
		_, err := adapter.Execute(context.Background(), catalogruntime.Invocation{
			Scope: LibraryDeleteScope, Method: "DELETE", Params: params, Body: []byte(body), Value: value,
		})
		require.ErrorIs(t, err, ErrInvalidRequest, "body %s must be rejected", body)
	}
	require.Empty(t, service.libraryIDs)

	ids := make([]uint64, 0, maxLibraryDeleteIDs+1)
	for i := 1; i <= maxLibraryDeleteIDs+1; i++ {
		ids = append(ids, uint64(i))
	}
	tooMany, err := json.Marshal(map[string]any{"ids": ids, "confirmed": true})
	require.NoError(t, err)
	_, err = adapter.Execute(context.Background(), catalogruntime.Invocation{
		Scope: LibraryDeleteScope, Method: "DELETE", Params: params, Body: tooMany, Value: value,
	})
	require.ErrorIs(t, err, ErrInvalidRequest)

	data, err := adapter.Execute(context.Background(), catalogruntime.Invocation{
		Scope: LibraryDeleteScope, Method: "DELETE", Params: params,
		Body: []byte(`{"ids":[11,12],"confirmed":true}`), Value: value,
	})
	require.NoError(t, err)
	require.Equal(t, []uint64{11, 12}, service.libraryIDs)
	require.Equal(t, 2, data.(devicesessions.LibraryDeleteResult).Deleted)

	// scope / method 不可互换：读码不能删图。
	_, err = adapter.Execute(context.Background(), catalogruntime.Invocation{
		Scope: LibraryReadScope, Method: "DELETE", Params: params,
		Body: []byte(`{"ids":[11],"confirmed":true}`), Value: value,
	})
	require.ErrorIs(t, err, ErrInvalidRequest)
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

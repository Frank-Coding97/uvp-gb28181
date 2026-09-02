package devicecapture

import (
	"bytes"
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRegistryCorrelatesUploadAndSnapshotNotify(t *testing.T) {
	registry := NewRegistry(t.TempDir())
	session := registry.Create(CreateRequest{OwnerID: "12", ChannelID: "31", ChannelCode: "34020000001320000001", DeviceCode: "34020000002000000001", SnapNum: 1, Interval: 1})
	jpeg := []byte{0xff, 0xd8, 0x01, 0x02, 0xff, 0xd9}
	file, err := registry.Upload(session.UploadToken, "shot-1.jpg", "image/jpeg", bytes.NewReader(jpeg))
	require.NoError(t, err)
	require.FileExists(t, file.Path)

	err = registry.OnSnapshotNotify(context.Background(), session.DeviceCode, []byte(`<?xml version="1.0" encoding="UTF-8"?><Notify><CmdType>Notify</CmdType><SubCmd>SnapShot</SubCmd><SN>9</SN><DeviceID>34020000001320000001</DeviceID><SessionID>`+session.ID+`</SessionID><SnapShotID>shot-1</SnapShotID><Time>2026-08-30T23:30:00+08:00</Time><StoragePath>http://localhost/shot-1.jpg</StoragePath></Notify>`))
	require.NoError(t, err)
	got, ok := registry.GetForOwner(session.ID, "12")
	require.True(t, ok)
	require.Equal(t, StateCompleted, got.State)
	require.Len(t, got.Files, 1)
	require.Equal(t, 1, got.NotifiedCount)
	require.NoError(t, os.Remove(file.Path))
}

func TestRegistryRejectsInvalidJPEGAndCapability(t *testing.T) {
	registry := NewRegistry(t.TempDir())
	session := registry.Create(CreateRequest{OwnerID: "12", ChannelID: "31", ChannelCode: "channel", DeviceCode: "device", SnapNum: 1, Interval: 1})
	_, err := registry.Upload("wrong", "shot.jpg", "image/jpeg", bytes.NewReader([]byte{0xff, 0xd8, 0xff, 0xd9}))
	require.ErrorIs(t, err, ErrNotFound)
	_, err = registry.Upload(session.UploadToken, "shot.jpg", "image/jpeg", bytes.NewReader([]byte("not-jpeg")))
	require.ErrorIs(t, err, ErrInvalidImage)
}

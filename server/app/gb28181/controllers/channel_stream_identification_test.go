package controllers_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
)

func TestChannelStreamIdentificationUpdate(t *testing.T) {
	f := newRecordQueryFixture(t, 10)
	f.router.PATCH("/channel/:id/stream-transport", f.controller.UpdateChannelStreamTransport)
	path := "/channel/" + uintStr(f.channel.ID) + "/stream-transport"
	for _, body := range []string{`{}`, `{"streamIdentification":null}`, `{"streamIdentification":"stream:2"}`, `{"streamIdentification":"stream:0\r\na=x"}`, `{"streamIdentification":"stream:0","streamTransport":"UDP"}`} {
		require.Equal(t, http.StatusBadRequest, f.request(http.MethodPatch, path, body).Code, body)
	}
	for _, value := range []string{"streamMode:SUB", ""} {
		result := f.request(http.MethodPatch, path, `{"streamIdentification":"`+value+`"}`)
		require.Equal(t, http.StatusOK, result.Code, result.Body.String())
		var row gbmodels.GbChannel
		require.NoError(t, f.db.First(&row, f.channel.ID).Error)
		require.Equal(t, value, row.StreamIdentification)
		require.Equal(t, "existing-live-stream", row.StreamID)
		require.Nil(t, row.CurrentStreamIdentification)
	}
	result := f.request(http.MethodPatch, path, `{"streamTransport":"UDP"}`)
	require.Equal(t, http.StatusOK, result.Code, result.Body.String())
	var row gbmodels.GbChannel
	require.NoError(t, f.db.First(&row, f.channel.ID).Error)
	require.Equal(t, "UDP", row.StreamTransport)
}

func TestChannelStreamIdentificationRespectsScope(t *testing.T) {
	f := newRecordQueryFixture(t, 20)
	f.router.PATCH("/channel/:id/stream-transport", f.controller.UpdateChannelStreamTransport)
	path := "/channel/" + uintStr(f.channel.ID) + "/stream-transport"
	require.Equal(t, http.StatusBadRequest, f.request(http.MethodPatch, path, `{"streamIdentification":"stream:1"}`).Code)
	var row gbmodels.GbChannel
	require.NoError(t, f.db.First(&row, f.channel.ID).Error)
	require.Empty(t, row.StreamIdentification)
}

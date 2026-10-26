package recording

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"uvplatform.com/uvp-gb28181/app/gb28181/models"
	"uvplatform.com/uvp-gb28181/app/gb28181/zlm/node"
)

// 回放流（pb-*）、下载流、对讲流都会流经 on_stream_changed，但它们不归任何 GB
// 通道。查不到通道必须安静返回，不能因为「找不到通道」把 hook 打成失败。
func TestObserveStreamIgnoresStreamWithoutChannel(t *testing.T) {
	repo, _ := seedRecordingChannel(t, true)
	client := &fakeRecorderClient{}
	service := newRecordingService(repo, client, fakeLocation{nodeID: 2, ok: true}, fakeRegistry{item: &node.Node{ID: 2}})

	require.NoError(t, service.ObserveStream(context.Background(), "pb-d3dd8752eceac5be", true))
	require.NoError(t, service.ObserveStream(context.Background(), "pb-d3dd8752eceac5be", false))
	require.NoError(t, service.ObserveStream(context.Background(), "", true))
	require.Zero(t, client.startCalls.Load())
	require.Zero(t, client.stopCalls.Load())
}

// 流断过又回来（设备重推 / 上级重新拉流）时，开关还开着就得重新录。
// 这条覆盖的是「上一次会话已 stopped」不被幂等短路挡住的情况。
func TestObserveStreamResumesRecordingAfterStreamReturns(t *testing.T) {
	repo, channel := seedRecordingChannel(t, true)
	channel.StreamID = "stream-resume"
	require.NoError(t, repo.db.Model(channel).Update("stream_id", channel.StreamID).Error)
	_, err := repo.SetDesired(context.Background(), channel.ID, true)
	require.NoError(t, err)
	client := &fakeRecorderClient{}
	service := newRecordingService(repo, client, fakeLocation{nodeID: 2, ok: true}, fakeRegistry{item: &node.Node{ID: 2}})

	require.NoError(t, service.ObserveStream(context.Background(), channel.StreamID, true))
	require.EqualValues(t, 1, client.startCalls.Load())

	require.NoError(t, service.ObserveStream(context.Background(), channel.StreamID, false))
	stored, err := repo.GetChannel(context.Background(), channel.ID)
	require.NoError(t, err)
	require.Equal(t, models.CloudRecordingStateWaiting, stored.CloudRecordingState)

	require.NoError(t, service.ObserveStream(context.Background(), channel.StreamID, true))
	require.EqualValues(t, 2, client.startCalls.Load())
	stored, err = repo.GetChannel(context.Background(), channel.ID)
	require.NoError(t, err)
	require.Equal(t, models.CloudRecordingStateRecording, stored.CloudRecordingState)
}

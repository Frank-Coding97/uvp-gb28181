package recording

import (
	"context"

	"uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/workrecording"
)

// GuardRecorderClient fences every legacy MP4 write against durable work
// ownership. The gate must be the same instance used by the work Recorder.
func GuardRecorderClient(client RecorderClient, gate *workrecording.MP4MutationGate, nodeID int64, lookup func(context.Context, string) (*models.GbChannel, error)) RecorderClient {
	return &guardedRecorderClient{client: client, gate: gate, nodeID: nodeID, lookup: lookup}
}

type guardedRecorderClient struct {
	client RecorderClient
	gate   *workrecording.MP4MutationGate
	nodeID int64
	lookup func(context.Context, string) (*models.GbChannel, error)
}

func (c *guardedRecorderClient) IsRecording(ctx context.Context, vhost, app, stream string) (bool, error) {
	var active bool
	err := c.mutate(ctx, vhost, app, stream, func(operationCtx context.Context) error {
		var err error
		active, err = c.client.IsRecording(operationCtx, vhost, app, stream)
		return err
	})
	return active, err
}

func (c *guardedRecorderClient) mutate(ctx context.Context, vhost, app, stream string, fn func(context.Context) error) error {
	if c.client == nil || c.gate == nil || c.lookup == nil {
		return workrecording.ErrInvalidRequest
	}
	channel, err := c.lookup(ctx, stream)
	if err != nil {
		return err
	}
	var channelID uint
	if channel != nil {
		channelID = channel.ID
	}
	return c.gate.LegacyMutation(ctx, channelID, workrecording.MediaTarget{NodeID: c.nodeID, VHost: vhost, App: app, Stream: stream}, fn)
}
func (c *guardedRecorderClient) StartRecord(ctx context.Context, vhost, app, stream string, maxSecond int) error {
	return c.mutate(ctx, vhost, app, stream, func(operationCtx context.Context) error {
		return c.client.StartRecord(operationCtx, vhost, app, stream, maxSecond)
	})
}
func (c *guardedRecorderClient) StopRecord(ctx context.Context, vhost, app, stream string) error {
	return c.mutate(ctx, vhost, app, stream, func(operationCtx context.Context) error { return c.client.StopRecord(operationCtx, vhost, app, stream) })
}

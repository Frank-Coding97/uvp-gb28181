package uac

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"uvplatform.cn/uvp-gb28181/app/global/app"
)

func TestWithSIPCommandTimeoutUsesConfiguredDefault(t *testing.T) {
	previous := app.ConfigYml
	t.Cleanup(func() { app.ConfigYml = previous })
	app.ConfigYml = nil

	ctx, cancel := withSIPCommandTimeout(context.Background())
	defer cancel()
	deadline, ok := ctx.Deadline()
	require.True(t, ok)
	require.InDelta(t, 10, time.Until(deadline).Seconds(), 0.5)
}

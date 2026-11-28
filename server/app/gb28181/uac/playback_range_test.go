package uac

import (
	"context"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestPlaybackRangeAdapterCarriesSMPTEAndEndInActualINFO(t *testing.T) {
	dialog := establishedPlaybackDialog()
	u, _ := newPlaybackTestUAC(dialog)
	metadata, err := u.InvitePlayback(context.Background(), validPlaybackInvite())
	require.NoError(t, err)
	adapter := NewPlaybackAdapter(u)
	value := "smpte=0:00:12:15-0:00:30:00"
	require.NoError(t, adapter.ActionRange(context.Background(), metadata.CallID, value, time.Minute))
	require.Len(t, dialog.requests, 1)
	require.Contains(t, string(dialog.requests[0].Body()), "Range: "+value+"\r\n")
	require.NotContains(t, string(dialog.requests[0].Body()), "npt=")
	require.Error(t, adapter.ActionRange(context.Background(), metadata.CallID, "npt=1-61", time.Minute))
	require.Len(t, dialog.requests, 1, "invalid range must not produce SIP traffic")
	dialog.responseStatus = 500
	require.Error(t, adapter.ActionRange(context.Background(), metadata.CallID, "npt=1-30", time.Minute))
}

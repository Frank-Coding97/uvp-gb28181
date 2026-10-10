package traffic

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestAccumulatorObserveAbsoluteUsesPositiveDeltaOnly(t *testing.T) {
	acc := Accumulator{}
	session := Session{LastTotalBytes: 100}

	delta, reset, err := acc.ObserveAbsolute(&session, 130, time.Unix(2, 0))
	require.NoError(t, err)
	require.False(t, reset)
	require.EqualValues(t, 30, delta)
	require.EqualValues(t, 130, session.LastTotalBytes)

	delta, reset, err = acc.ObserveAbsolute(&session, 130, time.Unix(3, 0))
	require.NoError(t, err)
	require.False(t, reset)
	require.Zero(t, delta)
}

func TestAccumulatorSettleAddsOnlyFinalTailAndIsIdempotent(t *testing.T) {
	acc := Accumulator{}
	session := Session{LastTotalBytes: 130, State: SessionActive}

	delta, err := acc.SettleAbsolute(&session, 134, time.Unix(4, 0), 4)
	require.NoError(t, err)
	require.EqualValues(t, 4, delta)
	require.Equal(t, SessionSettled, session.State)
	require.EqualValues(t, 134, session.SettledTotalBytes)

	delta, err = acc.SettleAbsolute(&session, 134, time.Unix(5, 0), 4)
	require.NoError(t, err)
	require.Zero(t, delta)
}

func TestAccumulatorTreatsLowerAbsoluteValueAsNewLifecycle(t *testing.T) {
	acc := Accumulator{}
	session := Session{LastTotalBytes: 130, State: SessionActive}

	delta, reset, err := acc.ObserveAbsolute(&session, 5, time.Unix(6, 0))
	require.NoError(t, err)
	require.True(t, reset)
	require.EqualValues(t, 5, delta)
	require.EqualValues(t, 5, session.LastTotalBytes)
	require.Equal(t, SessionActive, session.State)
}

func TestAccumulatorRejectsNilSession(t *testing.T) {
	_, _, err := (Accumulator{}).ObserveAbsolute(nil, 1, time.Now())
	require.Error(t, err)
}

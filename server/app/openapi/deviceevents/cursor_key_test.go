package deviceevents

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDeriveCursorKeyIsStableAndPurposeSpecific(t *testing.T) {
	root := bytes.Repeat([]byte{0x5a}, 32)
	first, err := DeriveCursorKey(root)
	require.NoError(t, err)
	require.Len(t, first, 32)
	require.NotEqual(t, root, first)

	repeated, err := DeriveCursorKey(root)
	require.NoError(t, err)
	require.Equal(t, first, repeated)

	other, err := DeriveCursorKey(bytes.Repeat([]byte{0x6b}, 32))
	require.NoError(t, err)
	require.NotEqual(t, first, other)
}

func TestDeriveCursorKeyRejectsMissingOrWeakMasterKey(t *testing.T) {
	for _, root := range [][]byte{nil, {}, []byte("short")} {
		_, err := DeriveCursorKey(root)
		require.ErrorIs(t, err, ErrUnavailable)
	}
}

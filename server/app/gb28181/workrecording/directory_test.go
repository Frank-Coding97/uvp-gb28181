package workrecording

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestWorkDirectoryUsesJobIdentityAndResolvesOnNode(t *testing.T) {
	const id = "8d9b6784-df9a-4486-84ce-3e6eedb18280"
	relative, err := WorkDirectory("./www", id)
	require.NoError(t, err)
	require.Equal(t, "www/work-recordings/"+id, relative)
	root, err := ResolvedWorkDirectory("/opt/zlm/www/work-recordings/"+id+"/record/rtp/stream/", id)
	require.NoError(t, err)
	require.Equal(t, "/opt/zlm/www/work-recordings/"+id, root)
	win, err := ResolvedWorkDirectory(`C:\zlm\www\work-recordings\`+id+`\record\rtp\stream\`, id)
	require.NoError(t, err)
	require.Equal(t, "C:/zlm/www/work-recordings/"+id, win)
	_, err = WorkDirectory("./www", "../other")
	require.ErrorIs(t, err, ErrInvalidRequest)
	_, err = ResolvedWorkDirectory("/opt/zlm/www/record/rtp/stream/", id)
	require.ErrorIs(t, err, ErrAttributionUnknown)
	_, err = ResolvedWorkDirectory("relative/work-recordings/"+id+"/record/rtp/stream/", id)
	require.ErrorIs(t, err, ErrAttributionUnknown)
}
func TestWorkDirectoryRejectsEmptyRootAndMismatchedOwner(t *testing.T) {
	_, err := WorkDirectory("", "8d9b6784-df9a-4486-84ce-3e6eedb18280")
	require.ErrorIs(t, err, ErrInvalidRequest)
	require.False(t, ownsWorkDirectory("/tmp/work-recordings/a", "b"))
	require.False(t, ownsWorkDirectory("/tmp/work-recordings/a/../b", "a"))
	require.True(t, ownsWorkDirectory("/tmp/work-recordings/a", "a"))
}

func TestWorkDirectoryRejectsNestedJobNamespaces(t *testing.T) {
	const id = "8d9b6784-df9a-4486-84ce-3e6eedb18280"
	_, err := WorkDirectory("/srv/work-recordings/another", id)
	require.ErrorIs(t, err, ErrInvalidRequest)
	_, err = ResolvedWorkDirectory("/srv/work-recordings/another/work-recordings/"+id+"/record/rtp/s/", id)
	require.ErrorIs(t, err, ErrAttributionUnknown)
}

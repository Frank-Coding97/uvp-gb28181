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

// The shape below is what a real ZLM node answers when asked where it would
// record a stream with no custom path: <root>/<record.appName>/<app>/<stream>/.
func TestRecordRootFromProbeStripsTheStreamTail(t *testing.T) {
	root, err := RecordRootFromProbe("/opt/media/bin/www/record/rtp/0200000000/", "record", "rtp", "0200000000")
	require.NoError(t, err)
	require.Equal(t, "/opt/media/bin/www", root)

	// A node that never had record.appName written down still uses ZLM's default.
	root, err = RecordRootFromProbe("/srv/zlm/www/record/rtp/stream/", "", "rtp", "stream")
	require.NoError(t, err)
	require.Equal(t, "/srv/zlm/www", root)

	root, err = RecordRootFromProbe(`C:\zlm\www\record\rtp\stream\`, "record", "rtp", "stream")
	require.NoError(t, err)
	require.Equal(t, "C:/zlm/www", root)
}

func TestRecordRootFromProbeRefusesToGuess(t *testing.T) {
	// The answer is about a different stream, so it says nothing about our root.
	_, err := RecordRootFromProbe("/srv/zlm/www/record/rtp/other/", "record", "rtp", "stream")
	require.ErrorIs(t, err, ErrAttributionUnknown)
	// A relative answer can never own a job directory.
	_, err = RecordRootFromProbe("www/record/rtp/stream/", "record", "rtp", "stream")
	require.ErrorIs(t, err, ErrAttributionUnknown)
	// Stripping the tail would leave nothing behind.
	_, err = RecordRootFromProbe("/record/rtp/stream/", "record", "rtp", "stream")
	require.ErrorIs(t, err, ErrAttributionUnknown)
	_, err = RecordRootFromProbe("", "record", "rtp", "stream")
	require.ErrorIs(t, err, ErrAttributionUnknown)

	for _, tc := range []struct{ app, stream string }{{"", "stream"}, {"rtp", ""}, {"r/tp", "stream"}, {"rtp", "st/ream"}} {
		_, err := RecordRootFromProbe("/srv/zlm/www/record/rtp/stream/", "record", tc.app, tc.stream)
		require.ErrorIs(t, err, ErrInvalidRequest, tc.app+"/"+tc.stream)
	}
}

func TestAbsoluteNodePathDistinguishesUsableRecordRoots(t *testing.T) {
	for _, value := range []string{"/opt/media/bin/www", `C:\zlm\www`, "C:/zlm/www", "//host/share"} {
		require.True(t, AbsoluteNodePath(value), value)
	}
	for _, value := range []string{"", "  ", "./www/record", "www/record", "record"} {
		require.False(t, AbsoluteNodePath(value), value)
	}
}

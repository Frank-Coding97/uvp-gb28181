package recording

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuildFileKeyUsesNodeAndEntireUTF8Path(t *testing.T) {
	const path = "/record/cam.mp4"
	const want = "a35d8d0f5d2146c8a37be5c88202bccfc86421806df200b22f272fba6fe08f3b"

	got := BuildFileKey(2, path)
	require.Equal(t, want, got)
	require.Len(t, got, 64)
	require.Equal(t, got, BuildFileKey(2, path))
	require.NotEqual(t, got, BuildFileKey(3, path))
	require.NotEqual(t, BuildFileKey(12, "3"), BuildFileKey(1, "23"), "the NUL separator must prevent concatenation collisions")

	longUnicodePath := "/录像/" + strings.Repeat("摄像头/", 400) + "结束.mp4"
	require.Equal(t, "4c5158515cab760049d0cd57224cf809622c321f68923dfdc8b5bdcd7debab4d", BuildFileKey(17, "/录像/摄像头.mp4"))
	require.Len(t, BuildFileKey(17, longUnicodePath), 64)
	require.Equal(t, BuildFileKey(17, longUnicodePath), BuildFileKey(17, longUnicodePath))
}

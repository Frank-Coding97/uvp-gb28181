package management

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm"
)

func TestIngressCapabilityReaderRequiresTheWholeWriteFamily(t *testing.T) {
	reader := NewIngressCapabilityReader(t19CapabilityProfiles{
		profile: zlm.CapabilityProfile{APIs: []string{
			"/index/api/listFFmpegSource", "/index/api/addFFmpegSource", "/index/api/delFFmpegSource",
			"/index/api/listRtpServer", "/index/api/openRtpServer", "/index/api/closeRtpServer",
		}},
	})

	ffmpeg, err := reader.FFmpegCapability(context.Background(), 7)
	require.NoError(t, err)
	require.Equal(t, FFmpegCapabilitySupported, ffmpeg)
	rtp, err := reader.RTPCapability(context.Background(), 7)
	require.NoError(t, err)
	require.Equal(t, RTPCapabilitySupported, rtp)

	reader = NewIngressCapabilityReader(t19CapabilityProfiles{
		profile: zlm.CapabilityProfile{APIs: []string{"/index/api/listFFmpegSource", "/index/api/addFFmpegSource"}},
	})
	ffmpeg, err = reader.FFmpegCapability(context.Background(), 7)
	require.NoError(t, err)
	require.Equal(t, FFmpegCapabilityUnsupported, ffmpeg)
}

func TestIngressCapabilityReaderKeepsFailedProbeUnknown(t *testing.T) {
	probeErr := errors.New("capability probe unavailable")
	reader := NewIngressCapabilityReader(t19CapabilityProfiles{err: probeErr})

	ffmpeg, err := reader.FFmpegCapability(context.Background(), 7)
	require.ErrorIs(t, err, probeErr)
	require.Equal(t, FFmpegCapabilityUnknown, ffmpeg)
	rtp, err := reader.RTPCapability(context.Background(), 7)
	require.ErrorIs(t, err, probeErr)
	require.Equal(t, RTPCapabilityUnknown, rtp)
}

func TestIngressListPagesExposeCapabilityAndTemplateCatalog(t *testing.T) {
	ffmpeg := NewFFmpegService(FFmpegDependencies{
		Client:     &t11FFmpegClient{},
		Templates:  NewFFmpegTemplateSet("ffmpeg.cmd_z", "ffmpeg.cmd_a"),
		Ledger:     &t11Ledger{},
		Capability: t19FFmpegCapability{state: FFmpegCapabilitySupported},
	})
	ffmpegPage, err := ffmpeg.ListPage(context.Background(), 7, PageRequest{Page: 1, PageSize: 20})
	require.NoError(t, err)
	require.Equal(t, FFmpegCapabilitySupported, ffmpegPage.Capability)
	require.Equal(t, []string{"ffmpeg.cmd_a", "ffmpeg.cmd_z"}, ffmpegPage.Templates)

	rtp := NewRTPService(RTPDependencies{
		Client:     &t11RTPClient{},
		Ledger:     &t11RTPLedger{},
		Capability: t19RTPCapability{state: RTPCapabilitySupported},
	})
	rtpPage, err := rtp.ListPage(context.Background(), 7, PageRequest{Page: 1, PageSize: 20})
	require.NoError(t, err)
	require.Equal(t, RTPCapabilitySupported, rtpPage.Capability)
}

type t19CapabilityProfiles struct {
	profile zlm.CapabilityProfile
	err     error
}

func (f t19CapabilityProfiles) GetCapabilityProfile(context.Context, int64) (zlm.CapabilityProfile, error) {
	return f.profile, f.err
}

type t19FFmpegCapability struct{ state FFmpegCapabilityState }

func (f t19FFmpegCapability) FFmpegCapability(context.Context, int64) (FFmpegCapabilityState, error) {
	return f.state, nil
}

type t19RTPCapability struct{ state RTPCapabilityState }

func (f t19RTPCapability) RTPCapability(context.Context, int64) (RTPCapabilityState, error) {
	return f.state, nil
}

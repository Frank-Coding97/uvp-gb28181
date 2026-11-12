package handler

import (
	"testing"
	"time"

	"github.com/emiago/sipgo/sip"
	"github.com/stretchr/testify/require"
)

func TestAdvertisedRegisterVersionHeaderIsCaseInsensitive(t *testing.T) {
	req := sip.NewRequest(sip.REGISTER, sip.Uri{Host: "platform"})
	req.AppendHeader(sip.NewHeader("x-gb-ver", " 3.0 "))

	got := advertisedRegisterVersion(req)
	require.Equal(t, " 3.0 ", got.Raw)
	require.Equal(t, "2022", string(got.Resolution.Profile.Version))
}

func TestRegisterResponseCarriesPlatformVersion(t *testing.T) {
	req := sip.NewRequest(sip.REGISTER, sip.Uri{Host: "platform"})
	res := newRegisterResponse(req, 401, "Unauthorized", nil, "3.0")

	header := res.GetHeader("x-gb-ver")
	require.NotNil(t, header)
	require.Equal(t, "3.0", header.Value())
}

func TestRegisterResponseUsesConfiguredPlatformVersion(t *testing.T) {
	req := sip.NewRequest(sip.REGISTER, sip.Uri{Host: "platform"})
	res := newRegisterResponse(req, 200, "OK", nil, "2.0")

	header := res.GetHeader("X-GB-Ver")
	require.NotNil(t, header)
	require.Equal(t, "2.0", header.Value())
}

func TestRegisterOKCarriesBeijingTimeWithMilliseconds(t *testing.T) {
	handler := NewRegisterHandler(securityTestCfg())
	handler.now = func() time.Time {
		return time.Date(2026, 8, 21, 8, 16, 11, 314000000, time.UTC)
	}
	req := sip.NewRequest(sip.REGISTER, sip.Uri{Host: "platform"})

	res := handler.buildOKWithExpires(req, 3600)

	header := res.GetHeader("Date")
	require.NotNil(t, header)
	require.Equal(t, "2026-08-21T16:16:11.314", header.Value())
}

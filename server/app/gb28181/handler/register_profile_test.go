package handler

import (
	"testing"

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

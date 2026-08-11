package gb28181

import (
	"testing"

	"github.com/stretchr/testify/require"

	gbconfig "uvplatform.cn/uvp-gb28181/app/gb28181/config"
)

func TestBuildPlaySignerRequiresIndependentStrongKeyWhenEnabled(t *testing.T) {
	settings := gbconfig.PlayAuthSettings{Enabled: true}
	for _, test := range []struct {
		name     string
		active   string
		previous string
		jwt      string
		zlm      string
	}{
		{name: "missing"},
		{name: "weak", active: "short"},
		{name: "jwt reuse", active: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", jwt: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
		{name: "zlm reuse", active: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", zlm: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
		{name: "duplicate key", active: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", previous: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
	} {
		t.Run(test.name, func(t *testing.T) {
			signer, err := buildPlaySigner(settings, test.active, test.previous, test.jwt, test.zlm)
			require.Error(t, err)
			require.Nil(t, signer)
		})
	}
}

func TestBuildPlaySignerAllowsDisabledCompatibilityAndValidRotation(t *testing.T) {
	signer, err := buildPlaySigner(gbconfig.PlayAuthSettings{}, "", "", "jwt", "zlm")
	require.NoError(t, err)
	require.Nil(t, signer)

	signer, err = buildPlaySigner(
		gbconfig.PlayAuthSettings{Enabled: true},
		"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		"jwt",
		"zlm",
	)
	require.NoError(t, err)
	require.NotNil(t, signer)
}

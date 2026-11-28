package config

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"uvplatform.com/uvp-gb28181/app/global/app"
)

func TestCascadeSettingsDefaultsAndCurrentValues(t *testing.T) {
	defaults := CascadeSettings{
		UserAgent: "UVP-GB28181", PlatformName: "UVP GB28181",
		Manufacturer: "UVP", Model: "UVP-GB28181",
	}
	require.Equal(t, defaults, CascadeSettingsFrom(nil))
	require.Equal(t, defaults, CascadeSettingsFrom(&playAuthMutableSource{values: map[string]interface{}{}}))

	previous := app.ConfigYml
	t.Cleanup(func() { app.ConfigYml = previous })
	app.ConfigYml = &playAuthMutableSource{values: map[string]interface{}{
		CascadeUserAgentConfigKey: "RegionalGateway/1.0", CascadePlatformNameConfigKey: "Regional platform",
		CascadeManufacturerConfigKey: "Vendor", CascadeModelConfigKey: "Gateway",
		CascadeCivilCodeConfigKey: "340200", CascadeAddressConfigKey: "Monitoring center",
	}}
	require.Equal(t, CascadeSettings{
		UserAgent: "RegionalGateway/1.0", PlatformName: "Regional platform", Manufacturer: "Vendor",
		Model: "Gateway", CivilCode: "340200", Address: "Monitoring center",
	}, CurrentCascadeSettings())
}

func TestCascadeSettingsRejectInvalidMetadata(t *testing.T) {
	valid := CascadeSettings{UserAgent: "UVP-GB28181", PlatformName: "UVP GB28181", Manufacturer: "UVP", Model: "UVP-GB28181"}
	for _, code := range []string{"", "34", "3402", "340200", "34020001"} {
		v := valid
		v.CivilCode = code
		require.NoError(t, ValidateCascadeSettings(v))
	}
	for name, change := range map[string]func(*CascadeSettings){
		"header injection":      func(v *CascadeSettings) { v.UserAgent = "UVP\r\nX-Injected: 1" },
		"empty identity":        func(v *CascadeSettings) { v.PlatformName = "  " },
		"long user agent":       func(v *CascadeSettings) { v.UserAgent = strings.Repeat("a", 129) },
		"long name":             func(v *CascadeSettings) { v.PlatformName = strings.Repeat("a", 129) },
		"long manufacturer":     func(v *CascadeSettings) { v.Manufacturer = strings.Repeat("a", 129) },
		"long model":            func(v *CascadeSettings) { v.Model = strings.Repeat("a", 129) },
		"long address":          func(v *CascadeSettings) { v.Address = strings.Repeat("a", 257) },
		"invalid civil code":    func(v *CascadeSettings) { v.CivilCode = "34020x" },
		"odd civil code length": func(v *CascadeSettings) { v.CivilCode = "340" },
	} {
		t.Run(name, func(t *testing.T) {
			v := valid
			change(&v)
			require.Error(t, ValidateCascadeSettings(v))
		})
	}
}

func TestCascadeSettingsInvalidStoredValuesUseDefaults(t *testing.T) {
	settings := CascadeSettingsFrom(&playAuthMutableSource{values: map[string]interface{}{
		CascadeUserAgentConfigKey: "UVP\r\nInjected: true", CascadeCivilCodeConfigKey: "invalid",
	}})
	require.Equal(t, "UVP-GB28181", settings.UserAgent)
	require.Empty(t, settings.CivilCode)
}

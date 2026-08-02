package directory

import (
	"testing"

	"github.com/stretchr/testify/require"
	"uvplatform.cn/uvp-gb28181/app/gb28181/civilcode"
)

type fakeCivilCodes map[string]*civilcode.SysCivilCode

func (f fakeCivilCodes) Lookup(code string) *civilcode.SysCivilCode { return f[code] }

func TestCivilCodeProjection(t *testing.T) {
	lookup := fakeCivilCodes{
		"370000": {Code: "370000", ShortName: "山东省", Level: civilcode.LevelProvince},
		"370100": {Code: "370100", ShortName: "济南市", ParentCode: "370000", Level: civilcode.LevelCity},
		"370112": {Code: "370112", ShortName: "历城区", ParentCode: "370100", Level: civilcode.LevelCounty},
	}
	tests := []struct {
		name, raw, reported, code, display string
		reason                             UnknownReason
	}{
		{"province", "37", "", "370000", "山东省", ""},
		{"city", "3701", "", "370100", "济南市", ""},
		{"county", "370112", "", "370112", "历城区", ""},
		{"town", "37011201", "唐冶街道", "37011201", "唐冶街道", ""},
		{"placeholder", "37011201", "行政区 37011201", "", "", UnknownPlaceholderName},
		{"invalid", "37A1", "", "", "", UnknownInvalidCode},
		{"missing", "999999", "", "", "", UnknownDictionaryMiss},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ProjectCivilCode(tt.raw, tt.reported, lookup)
			require.Equal(t, tt.code, got.Code)
			require.Equal(t, tt.display, got.Name)
			require.Equal(t, tt.reason, got.Reason)
			if tt.code != "" {
				require.Equal(t, "national:area:"+tt.code, got.Key)
			}
		})
	}
}

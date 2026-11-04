package security

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// ⛔ 这张表是"后端筛选等级"与"前端展示等级"的共同判据。
//
//	改规则必须同步 web/src/views/gb28181/security/securityFormatting.ts
//	与 securityFormatting.test.ts 的同名用例，否则会出现
//	「按高危筛选却翻出中危」。
func TestEventSeverityMatchesFrontendRules(t *testing.T) {
	cases := []struct {
		name   string
		action Action
		reason Reason
		want   EventSeverity
	}{
		{"已封禁一律高危", ActionBan, ReasonUnknownMethod, SeverityHigh},
		{"设备枚举高危", ActionSample, ReasonRegisterEnumeration, SeverityHigh},
		{"鉴权失败高危", ActionSample, ReasonDigestFailure, SeverityHigh},
		{"nonce 类高危", ActionSample, ReasonNonceReplay, SeverityHigh},
		{"已拒绝中危", ActionDrop, ReasonUnknownMethod, SeverityMedium},
		{"已记录低危", ActionSample, ReasonServerMismatch, SeverityLow},
		{"已放行低危", ActionAllow, ReasonUnknownMethod, SeverityLow},
		{"非鉴权的 ID 非法不算高危", ActionSample, ReasonRegisterIDInvalid, SeverityLow},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, EventSeverityFor(tc.action, tc.reason))
		})
	}
}

func TestEventSeverityRejectsUnknownFilterValue(t *testing.T) {
	require.True(t, EventSeverity("").Valid())
	require.True(t, SeverityHigh.Valid())
	require.False(t, EventSeverity("urgent").Valid())
	require.False(t, EventSeverity("高危").Valid())
}

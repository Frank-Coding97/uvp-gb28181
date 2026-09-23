package handler

import (
	"context"
	"testing"

	"github.com/emiago/sipgo/sip"
	"github.com/stretchr/testify/require"

	"uvplatform.cn/uvp-gb28181/app/gb28181/device"
)

// TestLoggingRegisterWarnsOnAbnormalVersionHeader 锁住附录 I「协议版本标识」的留痕行为。
//
// 为什么值得单测：设备版本头直接决定 `protocol.Profile` 的门禁，而门禁又决定平台**敢不敢**
// 下发精准云台 / 看守位查询 / 巡航轨迹查询这些 2022 才有的命令。缺了这条日志，
// 「某台设备的 2022 功能点不动」在排障时只能靠反推设备型号 —— 而四种异常（没带、格式错、
// 识别不了、2011 老版本）在 `profile.go` 里是四个不同的 warning code，混在一起就没法查了。
//
// 同时锁住"不阻断注册"这一半：四种异常都必须放行，只是回落 2016 兼容 profile。
func TestLoggingRegisterWarnsOnAbnormalVersionHeader(t *testing.T) {
	cases := []struct {
		name     string
		header   string // "" 表示请求里根本不带这个头
		wantCode string
	}{
		{"未声明 X-GB-Ver", "", "missing_version"},
		{"格式无效", "v3.0", "invalid_version"},
		{"表 I.1 之外的版本", "4.0", "unknown_version"},
		{"2011 老版本按 2016 兼容", "1.0", "legacy_version"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, sink := t14Runtime(t)

			h := NewRegisterHandler(securityTestCfg())
			h.SetSecurity(&fakeRegisterSecurity{})
			h.handleRegister = func(context.Context, device.RegisterInfo, int) (bool, error) {
				return true, nil
			}

			req := authorizedRegisterRequest(t, "accepted-nonce", securityTestPassword)
			if tc.header != "" {
				req.AppendHeader(sip.NewHeader("X-GB-Ver", tc.header))
			}
			h.Handle(req, &captureServerTransaction{})

			row := t14RecordWithEvent(t, sink, "gb28181.register.version_header_abnormal")
			require.Equal(t, tc.wantCode, row["warning_code"])
			require.Equal(t, securityTestDeviceID, row["device_id"])
			require.Equal(t, "security-register-test", row["call_id"])
			// 四种异常一律落到 2016 兼容 profile —— 这正是"不阻断注册"的表达方式。
			require.Equal(t, "2016", row["effective_version"])

			// 注册照常成功，异常只影响后续下行命令的门禁。
			t14RecordWithEvent(t, sink, "gb28181.register.succeeded")
		})
	}
}

// TestLoggingRegisterQuietOnDeclared2022 是上一条的对照面：声明 3.0 属正常，不该产生告警。
//
// 没有这条，前一条的断言只要"总是打告警"就能通过 —— 那种实现会让日志里全是噪音，
// 真正的异常反而被淹掉。
func TestLoggingRegisterQuietOnDeclared2022(t *testing.T) {
	_, sink := t14Runtime(t)

	h := NewRegisterHandler(securityTestCfg())
	h.SetSecurity(&fakeRegisterSecurity{})
	h.handleRegister = func(context.Context, device.RegisterInfo, int) (bool, error) {
		return true, nil
	}

	req := authorizedRegisterRequest(t, "accepted-nonce", securityTestPassword)
	req.AppendHeader(sip.NewHeader("X-GB-Ver", "3.0"))
	h.Handle(req, &captureServerTransaction{})

	for _, row := range t14Records(t, sink) {
		require.NotEqual(t, "gb28181.register.version_header_abnormal", row["event"],
			"设备已正确声明 3.0 时不该出现版本告警")
	}
	t14RecordWithEvent(t, sink, "gb28181.register.succeeded")
}

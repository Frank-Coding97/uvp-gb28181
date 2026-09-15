package handler

import (
	"context"
	"testing"

	"github.com/emiago/sipgo/sip"
	"github.com/stretchr/testify/require"

	"uvplatform.cn/uvp-gb28181/app/gb28181/device"
)

// TestLoggingRegisterSucceededCarriesEndpointAndCatalogOutcome 锁住
// `gb28181.register.succeeded` 的三个新增字段。
//
// 为什么值得单测：这条 INFO 是"设备上线"这条链路的**唯一入口日志**，
// 而读者看到它之后立刻会问的下一个问题是"那为什么通道没出来"。
// `catalog_triggered` 就是那个问题的第一个分叉，`peer` / `expires` 是
// "设备在 NAT 后面回连不到"和"设备反复上下线"两个高频故障的现场值。
func TestLoggingRegisterSucceededCarriesEndpointAndCatalogOutcome(t *testing.T) {
	cases := []struct {
		name        string
		isFirst     bool
		withTrigger bool
		wantCatalog bool
	}{
		// 三种组合覆盖 `is_first && trigger != nil && SyncChannelsOnOnline()` 的
		// 每一个可变量 —— 任一条被误当成另一条，日志都会开始骗人。
		{"首注册且触发器已装配：通道同步真的发起了", true, true, true},
		{"刷新注册（非首注册）：不重发通道查询", false, true, false},
		{"触发器未装配：注册成功，但通道同步没有发起", true, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, sink := t14Runtime(t)

			h := NewRegisterHandler(securityTestCfg())
			h.SetSecurity(&fakeRegisterSecurity{})
			h.handleRegister = func(context.Context, device.RegisterInfo, int) (bool, error) {
				return tc.isFirst, nil
			}
			if tc.withTrigger {
				h.SetCatalogTrigger(&countingRegisterTrigger{})
			}

			h.Handle(authorizedRegisterRequest(t, "accepted-nonce", securityTestPassword), &captureServerTransaction{})

			row := t14RecordWithEvent(t, sink, "gb28181.register.succeeded")
			require.Equal(t, securityTestDeviceID, row["device_id"])
			require.Equal(t, "security-register-test", row["call_id"])
			// peer = 本次 REGISTER 实际来自哪里（设备在 NAT 后面时它就是唯一的回连地址）。
			require.Equal(t, "198.51.100.23:5060", row["peer"])
			// expires 决定"这台设备多久之后会被判离线"，排"反复上下线"时第一个要看。
			require.Equal(t, float64(3600), row["expires"])
			require.Equal(t, "UDP", row["transport"])
			require.Equal(t, tc.isFirst, row["is_first"])
			require.Equal(t, tc.wantCatalog, row["catalog_triggered"])
		})
	}
}

// TestLoggingRegisterUnregisteredCarriesPeer 锁住注销事件的 `peer`。
//
// 注销与注册**方向相反**，`peer` 在这里回答的是"它是从哪个地址断开的" ——
// 设备换了网段后注册不上、注销请求却还能到，这种情况只有对比两条日志的 peer 才看得出来。
func TestLoggingRegisterUnregisteredCarriesPeer(t *testing.T) {
	_, sink := t14Runtime(t)

	h := NewRegisterHandler(securityTestCfg())
	h.SetSecurity(&fakeRegisterSecurity{})
	h.handleUnregister = func(context.Context, string) error { return nil }

	req := authorizedRegisterRequest(t, "accepted-nonce", securityTestPassword)
	// Expires: 0 是 GB28181 里"我要注销"的表达方式（不是"立即过期"）。
	req.RemoveHeader("Expires")
	req.AppendHeader(sip.NewHeader("Expires", "0"))

	h.Handle(req, &captureServerTransaction{})

	row := t14RecordWithEvent(t, sink, "gb28181.register.unregistered")
	require.Equal(t, securityTestDeviceID, row["device_id"])
	require.Equal(t, "security-register-test", row["call_id"])
	require.Equal(t, "198.51.100.23:5060", row["peer"])
}

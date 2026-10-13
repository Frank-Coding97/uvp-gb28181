package setup

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMediaNetworkDefaultsPersistAndClear(t *testing.T) {
	svc := newSIPConfigService(t, newConfigTestDB(t))
	password := "K9#nT2xQ"
	req := validSaveRequest(&password)
	req.HookIP = "192.168.1.20"
	req.SDPIP = "sdp.example.com"
	req.StreamIP = "stream.example.com"
	_, err := svc.Save(context.Background(), req)
	require.NoError(t, err)
	got, err := svc.Get(context.Background())
	require.NoError(t, err)
	require.Equal(t, req.HookIP, got.HookIP)
	require.Equal(t, req.SDPIP, got.SDPIP)
	require.Equal(t, req.StreamIP, got.StreamIP)
	// 选中具体真实网卡时三项都可清空：留空即跟随该网卡（运行时推断，库里存空）。
	req.HookIP, req.StreamIP, req.SDPIP, req.Password = "", "", "", nil
	_, err = svc.Save(context.Background(), req)
	require.NoError(t, err)
	got, err = svc.Get(context.Background())
	require.NoError(t, err)
	require.Empty(t, got.HookIP)
	require.Empty(t, got.StreamIP)
	require.Empty(t, got.SDPIP)
}

// 选中 0.0.0.0（所有网卡）时推断不出唯一的兜底地址 ⇒ 三项都必须手填。
func TestMediaNetworkDefaultsRequiredForWildcardListenAddress(t *testing.T) {
	svc := newSIPConfigService(t, newConfigTestDB(t))
	password := "K9#nT2xQ"
	req := validSaveRequest(&password)
	req.ListenIP = wildcardIPv4
	req.AdvertiseIP = ""
	// 三项都填上：0.0.0.0 下它们必须手填，所以这才能存进去。
	req.HookIP, req.SDPIP, req.StreamIP = "192.168.1.10", "192.168.1.10", "192.168.1.10"

	_, err := svc.Save(context.Background(), req)
	require.NoError(t, err)

	for _, clear := range []struct{ name, field string }{
		{"hook", "hookIp"},
		{"sdp", "sdpIp"},
		{"stream", "streamIp"},
	} {
		t.Run(clear.name, func(t *testing.T) {
			next := req
			next.Password = nil
			switch clear.field {
			case "hookIp":
				next.HookIP = ""
			case "sdpIp":
				next.SDPIP = ""
			case "streamIp":
				next.StreamIP = ""
			}
			err := func() error {
				_, err := svc.Save(context.Background(), next)
				return err
			}()
			var validation *ValidationError
			require.ErrorAs(t, err, &validation)
			require.Contains(t, validation.Fields, clear.field)
		})
	}
}

// 虚拟网卡上的地址设备侧访问不到，托底等于埋雷 ⇒ 与 0.0.0.0 同样必须手填。
func TestMediaNetworkDefaultsRequiredForVirtualListenAddress(t *testing.T) {
	db := newConfigTestDB(t)
	svc := NewSIPConfigService(db).WithInterfaces(virtualOnlyProvider(t))
	password := "K9#nT2xQ"
	req := validSaveRequest(&password)
	req.ListenIP = "192.168.56.1"
	req.AdvertiseIP = "192.168.56.1"

	// 三项填齐可存。
	req.HookIP, req.SDPIP, req.StreamIP = "192.168.56.1", "192.168.56.1", "192.168.56.1"
	_, err := svc.Save(context.Background(), req)
	require.NoError(t, err)

	// 清空 Hook 即被拒（该网卡地址不可信）。
	req.HookIP, req.SDPIP, req.StreamIP, req.Password = "", "", "", nil
	err = func() error {
		_, err := svc.Save(context.Background(), req)
		return err
	}()
	var validation *ValidationError
	require.ErrorAs(t, err, &validation)
	require.Contains(t, validation.Fields, "hookIp")
	require.Contains(t, validation.Fields, "sdpIp")
	require.Contains(t, validation.Fields, "streamIp")
}

func TestMediaNetworkDefaultsValidation(t *testing.T) {
	for _, tc := range []struct{ name, hook, stream, invalid string }{
		{"optional", "", "", ""},
		{"ip", "192.168.1.20", "203.0.113.8", ""},
		{"ipv6", "2001:db8::1", "2001:db8::2", ""},
		{"domain", "", "video.example.com", ""},
		{"absolute domain", "", "video.example.com.", ""},
		{"multicast", "224.0.0.1", "", "hookIp"},
		{"numeric invalid", "", "999.1.1.1", "streamIp"},
		{"hook domain", "hook.example.com", "", "hookIp"},
		{"wildcard", "0.0.0.0", "", "hookIp"},
		{"stream wildcard", "", "0.0.0.0", "streamIp"},
		{"url", "", "https://video.example.com", "streamIp"},
		{"port", "", "video.example.com:8080", "streamIp"},
		{"bad domain", "", "bad_host.example.com", "streamIp"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := validSaveRequest(nil)
			req.HookIP, req.StreamIP = tc.hook, tc.stream
			err := ValidateSIPConfigRequest(req, true, MediaAddressRequirement{SDPIP: true})
			if tc.invalid == "" {
				require.NoError(t, err)
				return
			}
			var validation *ValidationError
			require.ErrorAs(t, err, &validation)
			require.Contains(t, validation.Fields, tc.invalid)
		})
	}
}

// SDP IP 必填，且必须拒绝那些「配了也等于没配」的值。
//
// 这三类正是 wvp-GB28181-pro 兜底链会踩到的：它的 sdp-ip 留空时退回 media.ip，
// 裸机部署是 127.0.0.1、Docker 部署是容器名 —— 两者设备都到不了，
// 表征都是「注册成功但拉不到流」。
func TestSDPIPValidation(t *testing.T) {
	for _, tc := range []struct {
		name    string
		sdpIP   string
		invalid bool
	}{
		{"ipv4", "192.168.1.10", false},
		{"ipv6", "2001:db8::1", false},
		{"domain", "media.example.com", false},
		{"absolute domain", "media.example.com.", false},
		{"empty", "", true},
		{"blank", "   ", true},
		{"loopback", "127.0.0.1", true},
		{"loopback range", "127.0.0.53", true},
		{"ipv6 loopback", "::1", true},
		{"ipv4 wildcard", "0.0.0.0", true},
		{"ipv6 wildcard", "::", true},
		{"container name", "polaris-media", true},
		{"single label", "localhost", true},
		{"url", "https://media.example.com", true},
		{"port", "media.example.com:8080", true},
		{"bad domain", "bad_host.example.com", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := validSaveRequest(nil)
			req.SDPIP = tc.sdpIP
			err := ValidateSIPConfigRequest(req, true, MediaAddressRequirement{SDPIP: true})
			if !tc.invalid {
				require.NoError(t, err)
				return
			}
			var validation *ValidationError
			require.ErrorAs(t, err, &validation)
			require.Contains(t, validation.Fields, "sdpIp")
		})
	}
}

// 保存后的值必须原样读回：SDP IP 决定设备往哪推流，被改写等于点播静默失效。
func TestSDPIPRoundTripsUnchanged(t *testing.T) {
	svc := newSIPConfigService(t, newConfigTestDB(t))
	password := "K9#nT2xQ"
	req := validSaveRequest(&password)
	req.SDPIP = "  192.168.1.10  "
	view, err := svc.Save(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, "192.168.1.10", strings.TrimSpace(view.SDPIP))
}

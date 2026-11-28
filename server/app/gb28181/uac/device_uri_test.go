package uac

import "testing"

// 请求行的域必须是**目标编码自己的域**，否则跨域级联时下级会把报文当
// 「不是发给我的」静默丢弃（实测 236 收 6 个 INVITE 零响应，上级 12s 超时）。
func TestDeviceURIUsesTargetDomain(t *testing.T) {
	u := &UAC{domain: "3701000000"}

	cases := []struct {
		name     string
		deviceID string
		want     string
	}{
		{"本地设备与平台同域", "37010000001320009024", "3701000000"},
		{"级联下级的通道走下级域", "34020000001320000025", "3402000000"},
		{"级联下级平台自身", "34020000002000000001", "3402000000"},
		{"不足20位回落到平台域", "3402000000132000", "3701000000"},
		{"含非数字回落到平台域", "3402000000132000002X", "3701000000"},
		{"空串回落到平台域", "", "3701000000"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			uri := u.deviceURI(c.deviceID)
			if got := uri.Host; got != c.want {
				t.Fatalf("deviceURI(%q) host=%q, want %q", c.deviceID, got, c.want)
			}
			if got := uri.User; got != c.deviceID {
				t.Fatalf("deviceURI(%q) user=%q, want %q", c.deviceID, got, c.deviceID)
			}
		})
	}
}

func TestDeviceDomainRejectsNonStandardCodes(t *testing.T) {
	if _, ok := deviceDomain("340200000013200000"); ok {
		t.Fatal("19 位编码不应被当作标准编码取域")
	}
	if _, ok := deviceDomain("340200000013200000251"); ok {
		t.Fatal("21 位编码不应被当作标准编码取域")
	}
	if d, ok := deviceDomain("34020000001320000025"); !ok || d != "3402000000" {
		t.Fatalf("标准编码取域=%q ok=%v, want 3402000000/true", d, ok)
	}
}

package zlm

import (
	"testing"
	gbconfig "uvplatform.cn/uvp-gb28181/app/gb28181/config"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
)

func TestStandaloneRTCUsesConfirmedReceiveAddress(t *testing.T) {
	n := &node.Node{Host: "127.0.0.1", ReceiveHost: "192.0.2.52", PlaybackHost: "192.0.2.53", APISecret: "test-secret", MediaServerUUID: "test-node"}
	media := gbconfig.MediaConfig{HookHost: "127.0.0.1", HookPort: 8280, ManageRTCExternIP: true}
	expected, err := ExpectedConfigForNode(n, media)
	if err != nil {
		t.Fatal(err)
	}
	if expected["rtc.externIP"] != "192.0.2.52" {
		t.Fatal("RTC candidate address must use confirmed receive address, not API or playback host")
	}
	for _, bad := range []string{"", "0.0.0.0", "224.0.0.1", "hostname.invalid"} {
		n.ReceiveHost = bad
		if _, err := ExpectedConfigForNode(n, media); err == nil {
			t.Fatalf("accepted invalid RTC address %q", bad)
		}
	}
	media.ManageRTCExternIP = false
	expected, err = ExpectedConfigForNode(n, media)
	if err != nil {
		t.Fatal(err)
	}
	if _, present := expected["rtc.externIP"]; present {
		t.Fatal("legacy media configuration unexpectedly owns RTC externIP")
	}
}

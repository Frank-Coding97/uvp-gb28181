package protocol_test

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	protocol "uvplatform.cn/uvp-gb28181/app/gb28181/cascade/protocol"
	baseprotocol "uvplatform.cn/uvp-gb28181/app/gb28181/protocol"
)

// T1 evidence table. The page numbers are printed page numbers, not PDF
// viewer offsets. Fixture payloads are test inputs only until a later task
// performs a per-field PDF check before introducing a sending builder.
var fixtureEvidence = map[string]struct {
	profile baseprotocol.Version
	source  string
	sha256  string
}{
	"2016/register/initial.sip":             {baseprotocol.Version2016, "2016 9.1/J (15-49/115-194); T1 baseline: no X-GB-Ver", "557fd36d2056efdd2703f927455d515a4b5928ef30622404c3246687bd8174eb"},
	"2016/register/challenge-401.sip":       {baseprotocol.Version2016, "2016 9.1/J (15-49/115-194); Digest challenge fixture", "cb1d1abf97f1526b58a7fbc41a8c6c8e08246714b8a4d1d2d922cd154a848f8a"},
	"2016/register/ok.sip":                  {baseprotocol.Version2016, "2016 9.1/J (15-49/115-194); successful REGISTER fixture", "cae6db4487acdaf9ec8794ebdb40d59d0353b6691d2f01b74b169ecdc499d470"},
	"2016/register/logout.sip":              {baseprotocol.Version2016, "2016 9.1 (15-49); Expires: 0", "8e98a309280ff7b78fc1769cba7ecb38f0f44c7553c83bfb4c2265c6634c3046"},
	"2022/register/initial.sip":             {baseprotocol.Version2022, "2022 9.1/I (16-55/138); X-GB-Ver: 3.0", "e79ffa0f06811b3b8fba56d41f7382495beb2d829921fb45af8e13d512f2ac51"},
	"2022/register/challenge-401.sip":       {baseprotocol.Version2022, "2022 9.1/I (16-55/138); Digest challenge fixture", "b013edef1649e0eb21889d48e8b109333b48db544d891ab736429d87f7efad2f"},
	"2022/register/ok.sip":                  {baseprotocol.Version2022, "2022 9.1/I (16-55/138); successful REGISTER fixture", "4f51313a97d16ed2a437337d0180de5b1c643f27d1989a984d21e9390f969273"},
	"2022/register/logout.sip":              {baseprotocol.Version2022, "2022 9.1/I (16-55/138); Expires: 0", "a8cc9408f22d7666fc8aed9d4e460dabd500f454e163a9738b5d09a5945b253e"},
	"2016/manscdp/keepalive.xml":            {baseprotocol.Version2016, "2016 9/A (15-49/50-77); MESSAGE MANSCDP", "691cbe26e32a1bc614652b881626c4661af5fca22902f700dc87959f364277b3"},
	"2022/manscdp/keepalive.xml":            {baseprotocol.Version2022, "2022 9/A (16-55/56-105); MESSAGE MANSCDP", "a130517898ecfcc8b533a3820fbe0ede95392750bacdde8ccae48786e4cff76e"},
	"2016/catalog/empty.xml":                {baseprotocol.Version2016, "2016 N (198); SumNum=0 has no record list", "2993cac0227f1f37d637be8f0c26f2cd2959d5194c45a0009eb766c2e992890e"},
	"2016/catalog/single.xml":               {baseprotocol.Version2016, "2016 A/O (50-77/199-201); Catalog response", "9e82300479cea714bec9a222cfe6c441dddc3305822b0de797aaa26ecd32e69a"},
	"2016/catalog/multi-1.xml":              {baseprotocol.Version2016, "2016 N (198); response SN matches request", "a56c7f4eaa23bf103b4c72e19fedb26345c9618adc704b66e5c834cfc7e3a3df"},
	"2016/catalog/multi-2-duplicate-sn.xml": {baseprotocol.Version2016, "2016 N (198); duplicate SN is a batch correlation, not a terminator", "b82fbff5958f37951002d836cd8e728c3e7c59249ae1a44782b8d4fea7517de2"},
	"2022/catalog/empty.xml":                {baseprotocol.Version2022, "2022 M (145); SumNum=0 has no record list", "a0f1f8424c38aa86a59dd431abde19052a85097d32a5e06c737605e9d9aef770"},
	"2022/catalog/single.xml":               {baseprotocol.Version2022, "2022 A/J (56-105/139-142); Catalog response", "d7c6f5a97d5c2a3b5e2fbd2a8deee17fcc8fb5d562785a163835e91d2e5632fa"},
	"2022/catalog/multi-1.xml":              {baseprotocol.Version2022, "2022 M (145); response SN matches request", "1beae72919e5a9d7f2430cd930225de9bcf525421767dbbd9a6b96f3a87e82c8"},
	"2022/catalog/multi-2-duplicate-sn.xml": {baseprotocol.Version2022, "2022 M (145); duplicate SN is a batch correlation, not a terminator", "1da57394acccf0615395a7bde4e64557b28a6a03f07ceb6964c7f3c635bae6da"},
	"2016/manscdp/device-info.xml":          {baseprotocol.Version2016, "2016 A/J (50-77/115-194); DeviceInfo fixture", "df51cfa40a4e6b1abca331578d5025cf1e4ce88688bcf28c4c5dcb852ec11a86"},
	"2016/manscdp/device-status.xml":        {baseprotocol.Version2016, "2016 A/J (50-77/115-194); DeviceStatus fixture with legacy num", "7b966b6c85433596c09e3154a6c1d79cb5a11de456cb9432693a92290c997a9a"},
	"2022/manscdp/device-info.xml":          {baseprotocol.Version2022, "2022 A (56-105); DeviceInfo fixture", "06a010e70b5b24392ec18c606906fed6fdab73244ab98c2a600a5bd416b8ed87"},
	"2022/manscdp/device-status.xml":        {baseprotocol.Version2022, "2022 A (56-105); DeviceStatus fixture with Num", "bdd950cf5412e3424c288767902f5668d3c981da81b3f2f3a36ebcac8c566ddc"},
	"2016/play/udp.sip":                     {baseprotocol.Version2016, "2016 F/K/J (95-98/195/115-194); Play SDP over UDP", "3be0af91bcd3fc2022532cc0ec75c9089411041e1982b5db1cbb0368689a6b72"},
	"2016/play/tcp.sip":                     {baseprotocol.Version2016, "2016 F/K/L (95-98/195/196); Play SDP over TCP", "4958e79eb162e51cf7538765cfefec7a4f172f226934c1dc3c53cbff36528df8"},
	"2022/play/udp.sip":                     {baseprotocol.Version2022, "2022 G/L (129-132/144); Play SDP over UDP", "3be0af91bcd3fc2022532cc0ec75c9089411041e1982b5db1cbb0368689a6b72"},
	"2022/play/tcp.sip":                     {baseprotocol.Version2022, "2022 G/L/D (129-132/144/112); Play SDP over TCP", "4958e79eb162e51cf7538765cfefec7a4f172f226934c1dc3c53cbff36528df8"},
	"2016/manscdp/ptz.xml":                  {baseprotocol.Version2016, "2016 A.3 (50-77); helper scope, no PTZ bit semantics", "c39d6bf527b1e4bc7440849ab8dd91ed6183e3b5dab82035ed7712cd3244c837"},
	"2022/manscdp/ptz.xml":                  {baseprotocol.Version2022, "2022 A.3 (56-105); helper scope, no PTZ bit semantics", "64b419459d70412367eced8656de8f1f89895ec763a4b7f834f6530767bf681c"},
}

func TestGoldenFixtures(t *testing.T) {
	for name, evidence := range fixtureEvidence {
		t.Run(name, func(t *testing.T) {
			body := mustReadFixture(t, name)
			if evidence.sha256 == "" {
				t.Fatalf("fixture %s has no reviewed SHA-256", name)
			}
			gotHash := sha256.Sum256(body)
			if got := hex.EncodeToString(gotHash[:]); got != evidence.sha256 {
				t.Fatalf("fixture hash = %s, want %s", got, evidence.sha256)
			}

			switch {
			case strings.Contains(name, "/register/"):
				message, err := protocol.ParseRegister(body)
				if err != nil {
					t.Fatal(err)
				}
				if err := protocol.ValidateRegisterProfile(evidence.profile, message); err != nil {
					t.Fatal(err)
				}
			case strings.Contains(name, "/manscdp/") || strings.Contains(name, "/catalog/"):
				message, err := protocol.ParseMANSCDP(evidence.profile, body)
				if err != nil {
					t.Fatal(err)
				}
				if err := protocol.ValidateMANSCDPFixture(message); err != nil {
					t.Fatal(err)
				}
			case strings.Contains(name, "/play/"):
				invite, err := protocol.ParsePlayInvite(body)
				if err != nil {
					t.Fatal(err)
				}
				if err := protocol.ValidatePlayInvite(evidence.profile, invite); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestRegisterHeadersAreProfileScoped(t *testing.T) {
	legacy, err := protocol.ParseRegister(mustReadFixture(t, "2016/register/initial.sip"))
	if err != nil {
		t.Fatal(err)
	}
	if legacy.Header("X-GB-Ver") != "" {
		t.Fatalf("2016 fixture must not send X-GB-Ver: %q", legacy.Header("X-GB-Ver"))
	}

	modern, err := protocol.ParseRegister(mustReadFixture(t, "2022/register/initial.sip"))
	if err != nil {
		t.Fatal(err)
	}
	if got := modern.Header("X-GB-Ver"); got != "3.0" {
		t.Fatalf("2022 X-GB-Ver = %q, want 3.0", got)
	}
}

func TestRegisterLifecycleFixtures(t *testing.T) {
	for _, name := range []string{
		"2016/register/challenge-401.sip", "2016/register/ok.sip", "2016/register/logout.sip",
		"2022/register/challenge-401.sip", "2022/register/ok.sip", "2022/register/logout.sip",
	} {
		t.Run(name, func(t *testing.T) {
			message, err := protocol.ParseRegister(mustReadFixture(t, name))
			if err != nil {
				t.Fatal(err)
			}
			switch {
			case strings.Contains(name, "challenge-401"):
				if !strings.HasPrefix(message.StartLine, "SIP/2.0 401") || message.Header("WWW-Authenticate") == "" {
					t.Fatalf("invalid 401 challenge fixture: %#v", message)
				}
			case strings.Contains(name, "/ok.sip"):
				if !strings.HasPrefix(message.StartLine, "SIP/2.0 200") {
					t.Fatalf("invalid 200 fixture: %q", message.StartLine)
				}
			case strings.Contains(name, "logout"):
				if got := message.Header("Expires"); got != "0" {
					t.Fatalf("logout Expires = %q, want 0", got)
				}
			}
		})
	}
}

func TestCatalogFixtureBoundaries(t *testing.T) {
	for _, profile := range []baseprotocol.Version{baseprotocol.Version2016, baseprotocol.Version2022} {
		version := string(profile)
		empty, err := protocol.ParseMANSCDP(profile, mustReadFixture(t, version+"/catalog/empty.xml"))
		if err != nil {
			t.Fatal(err)
		}
		if empty.SumNum == nil || *empty.SumNum != 0 || empty.DeviceListSize != 0 {
			t.Fatalf("empty Catalog = %#v", empty)
		}
		first, err := protocol.ParseMANSCDP(profile, mustReadFixture(t, version+"/catalog/multi-1.xml"))
		if err != nil {
			t.Fatal(err)
		}
		second, err := protocol.ParseMANSCDP(profile, mustReadFixture(t, version+"/catalog/multi-2-duplicate-sn.xml"))
		if err != nil {
			t.Fatal(err)
		}
		if first.SN != second.SN || first.DeviceListSize != 1 || second.DeviceListSize != 1 {
			t.Fatalf("multi-response fixture does not preserve request SN: %#v %#v", first, second)
		}
	}
}

func TestDeviceStatusListAttributeIsProfileScoped(t *testing.T) {
	legacy, err := protocol.ParseMANSCDP(baseprotocol.Version2016, mustReadFixture(t, "2016/manscdp/device-status.xml"))
	if err != nil {
		t.Fatal(err)
	}
	if legacy.DeviceListnum != "0" || legacy.DeviceListNum != "" {
		t.Fatalf("2016 DeviceList attributes = %#v", legacy)
	}

	modern, err := protocol.ParseMANSCDP(baseprotocol.Version2022, mustReadFixture(t, "2022/manscdp/device-status.xml"))
	if err != nil {
		t.Fatal(err)
	}
	if modern.DeviceListNum != "0" || modern.DeviceListnum != "" {
		t.Fatalf("2022 DeviceList attributes = %#v", modern)
	}
}

func mustReadFixture(t *testing.T, name string) []byte {
	t.Helper()
	path := filepath.Join("testdata", name)
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

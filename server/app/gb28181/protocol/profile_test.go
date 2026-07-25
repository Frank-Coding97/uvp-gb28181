package protocol

import "testing"

func TestResolveVersionMapping(t *testing.T) {
	tests := []struct {
		name        string
		raw         string
		wantVersion Version
		wantSource  Source
		wantWarning WarningCode
	}{
		{name: "missing", raw: "", wantVersion: Version2016, wantSource: SourceDefault, wantWarning: WarningMissingVersion},
		{name: "2011-1.0", raw: "1.0", wantVersion: Version2016, wantSource: SourceRegister},
		{name: "2011-1.1", raw: "1.1", wantVersion: Version2016, wantSource: SourceRegister},
		{name: "2016", raw: "2.0", wantVersion: Version2016, wantSource: SourceRegister},
		{name: "2022", raw: "3.0", wantVersion: Version2022, wantSource: SourceRegister},
		{name: "invalid", raw: "not-a-version", wantVersion: Version2016, wantSource: SourceRegister, wantWarning: WarningInvalidVersion},
		{name: "unknown", raw: "9.9", wantVersion: Version2016, wantSource: SourceRegister, wantWarning: WarningUnknownVersion},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := ResolveInput{}
			if tt.name == "missing" {
				input.History = ""
			} else {
				input.Register = tt.raw
			}

			got := Resolve(input)
			if got.Profile.Version != tt.wantVersion {
				t.Fatalf("version = %q, want %q", got.Profile.Version, tt.wantVersion)
			}
			if got.Source != tt.wantSource {
				t.Fatalf("source = %q, want %q", got.Source, tt.wantSource)
			}
			if got.WarningCode != tt.wantWarning {
				t.Fatalf("warning code = %q, want %q", got.WarningCode, tt.wantWarning)
			}
		})
	}
}

func TestResolveSourcePriority(t *testing.T) {
	tests := []struct {
		name       string
		input      ResolveInput
		wantSource Source
		want       Version
	}{
		{
			name:       "override wins over register and history",
			input:      ResolveInput{Override: "2022", Register: "2.0", History: "1.0"},
			wantSource: SourceOverride,
			want:       Version2022,
		},
		{
			name:       "register wins over history",
			input:      ResolveInput{Register: "3.0", History: "2.0"},
			wantSource: SourceRegister,
			want:       Version2022,
		},
		{
			name:       "history wins over default",
			input:      ResolveInput{History: "2.0"},
			wantSource: SourceHistory,
			want:       Version2016,
		},
		{
			name:       "blank values are absent",
			input:      ResolveInput{Override: " \t", Register: "", History: "3.0"},
			wantSource: SourceHistory,
			want:       Version2022,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Resolve(tt.input)
			if got.Source != tt.wantSource || got.Profile.Version != tt.want {
				t.Fatalf("resolution = (%q, %q), want (%q, %q)", got.Source, got.Profile.Version, tt.wantSource, tt.want)
			}
		})
	}
}

func TestResolveInvalidHigherPrioritySourceDoesNotFallThrough(t *testing.T) {
	got := Resolve(ResolveInput{Override: "9.9", Register: "3.0", History: "2.0"})
	if got.Profile.Version != Version2016 {
		t.Fatalf("version = %q, want 2016 fallback", got.Profile.Version)
	}
	if got.Source != SourceOverride {
		t.Fatalf("source = %q, want override", got.Source)
	}
	if got.WarningCode != WarningUnknownVersion {
		t.Fatalf("warning code = %q, want unknown_version", got.WarningCode)
	}
}

func TestProfileProtocolFields(t *testing.T) {
	legacy := ProfileForVersion(Version2016)
	if legacy.Charset != CharsetGB2312 {
		t.Fatalf("2016 charset = %q, want %q", legacy.Charset, CharsetGB2312)
	}
	if legacy.IFrameElement != "IFameCmd" {
		t.Fatalf("2016 iframe element = %q, want IFameCmd", legacy.IFrameElement)
	}
	if legacy.Capabilities.PrecisePTZ {
		t.Fatal("2016 standard profile must not advertise precise PTZ")
	}

	modern := ProfileForVersion(Version2022)
	if modern.Charset != CharsetGB18030 {
		t.Fatalf("2022 charset = %q, want %q", modern.Charset, CharsetGB18030)
	}
	if modern.IFrameElement != "IFrameCmd" {
		t.Fatalf("2022 iframe element = %q, want IFrameCmd", modern.IFrameElement)
	}
	if !modern.Capabilities.PrecisePTZ {
		t.Fatal("2022 profile must advertise precise PTZ")
	}
}

func TestResolveExplicitVendorCharset(t *testing.T) {
	got := Resolve(ResolveInput{Register: "3.0", CharsetOverride: "UTF-8"})
	if got.Profile.Charset != CharsetUTF8 {
		t.Fatalf("charset = %q, want %q", got.Profile.Charset, CharsetUTF8)
	}
	if got.Profile.Version != Version2022 {
		t.Fatalf("version = %q, want %q", got.Profile.Version, Version2022)
	}
	if got.Source != SourceRegister {
		t.Fatalf("source = %q, want %q", got.Source, SourceRegister)
	}
}

func TestProfileResponseSemantics(t *testing.T) {
	profile := ProfileForVersion(Version2022)
	for _, action := range []Action{ActionRecord, ActionGuard, ActionAlarm} {
		if !profile.ResponseFor(action).ResponseRequired || !profile.ResponseFor(action).ResultRequired {
			t.Fatalf("%s must require a device result", action)
		}
	}
	for _, action := range []Action{ActionIFrame, ActionTeleBoot, ActionDragZoom, ActionPrecisePTZ} {
		semantic := profile.ResponseFor(action)
		if semantic.ResponseRequired || semantic.ResultRequired {
			t.Fatalf("%s must not require a business response: %#v", action, semantic)
		}
	}
}

func TestResolverDoesNotAutoSwitchAfterControlFailure(t *testing.T) {
	input := ResolveInput{Register: "3.0"}
	first := Resolve(input)
	// A command failure is an operation result, not a version signal. The
	// resolver has no failure input and must remain deterministic for retries.
	second := Resolve(input)
	if first != second {
		t.Fatalf("resolution changed after a failed operation: first=%#v second=%#v", first, second)
	}
	if first.Profile.Version != Version2022 || first.Source != SourceRegister {
		t.Fatalf("unexpected initial resolution: %#v", first)
	}
}

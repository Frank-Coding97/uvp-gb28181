// Package protocol contains version-only GB/T 28181 protocol decisions.
//
// The package deliberately has no database, SIP, or device capability
// dependencies. A resolved profile is therefore safe to capture on an
// operation and reuse for retries without silently changing wire semantics.
package protocol

import (
	"fmt"
	"strconv"
	"strings"
)

// Version is the effective wire profile used for a device operation.
type Version string

// Keep these constants untyped so callers can use them in both Version and
// string fields (for example, persisted DTOs) without conversions.
const (
	Version2016 = "2016"
	Version2022 = "2022"
	VersionAuto = "auto"
)

// Source identifies where the effective version came from.
type Source string

const (
	SourceOverride Source = "override"
	SourceRegister Source = "register"
	SourceHistory  Source = "history"
	SourceDefault  Source = "default"
)

// WarningCode describes why a candidate did not represent a fully recognized
// version. Warning codes are intentionally data, rather than errors: an
// unknown device still has a usable 2016 compatibility profile.
type WarningCode string

const (
	WarningNone           WarningCode = ""
	WarningMissingVersion WarningCode = "missing_version"
	WarningLegacyVersion  WarningCode = "legacy_version"
	WarningInvalidVersion WarningCode = "invalid_version"
	WarningUnknownVersion WarningCode = "unknown_version"
	WarningInvalidCharset WarningCode = "invalid_charset"
)

// Charset is the actual XML byte encoding, not just a declaration value.
type Charset string

const (
	CharsetGB2312  = "GB2312"
	CharsetGB18030 = "GB18030"
	CharsetUTF8    = "UTF-8"
)

// Capabilities contains capabilities that are part of a protocol profile.
// Device-reported capability hints are intentionally not represented here;
// those hints must never become a sending gate for standard controls.
type Capabilities struct {
	PrecisePTZ bool
}

// ResponseSemantic captures whether an action has a standard application
// response and whether Result=OK/ERROR is required before accepting it.
type ResponseSemantic struct {
	ResponseRequired bool
	ResultRequired   bool
}

// ResponsePolicy groups response semantics for the versioned command set.
type ResponsePolicy struct {
	IFrame     ResponseSemantic
	Record     ResponseSemantic
	Guard      ResponseSemantic
	Alarm      ResponseSemantic
	TeleBoot   ResponseSemantic
	DragZoom   ResponseSemantic
	PrecisePTZ ResponseSemantic
}

// Profile contains all decisions that can change the wire representation for
// the protocol areas currently versioned by this package.
type Profile struct {
	Version       Version
	Charset       Charset
	IFrameElement string
	Capabilities  Capabilities
	// PrecisePTZ is retained as a convenient compatibility accessor for
	// callers that used the initial profile shape. It mirrors Capabilities.
	PrecisePTZ bool
	Responses  ResponsePolicy
}

// Action names the operations whose result semantics are part of a profile.
type Action string

const (
	ActionIFrame     Action = "iframe"
	ActionRecord     Action = "record"
	ActionGuard      Action = "guard"
	ActionAlarm      Action = "alarm"
	ActionTeleBoot   Action = "teleboot"
	ActionDragZoom   Action = "drag_zoom"
	ActionPrecisePTZ Action = "precise_ptz"

	// KeyFrame and drag direction aliases make call sites read naturally while
	// retaining one stable action value for response lookup.
	ActionKeyFrame    = ActionIFrame
	ActionDragZoomIn  = ActionDragZoom
	ActionDragZoomOut = ActionDragZoom
)

var noBusinessResponse = ResponseSemantic{}

var businessResponse = ResponseSemantic{
	ResponseRequired: true,
	ResultRequired:   true,
}

func defaultResponsePolicy() ResponsePolicy {
	return ResponsePolicy{
		IFrame:     noBusinessResponse,
		Record:     businessResponse,
		Guard:      businessResponse,
		Alarm:      businessResponse,
		TeleBoot:   noBusinessResponse,
		DragZoom:   noBusinessResponse,
		PrecisePTZ: noBusinessResponse,
	}
}

// ProfileFor returns a supported profile. Unknown values intentionally use
// the 2016 compatibility profile; callers should resolve the source and
// warning separately before calling this helper.
func ProfileFor(version Version) Profile {
	if version == Version(Version2022) {
		return Profile{
			Version:       Version(Version2022),
			Charset:       Charset(CharsetGB18030),
			IFrameElement: "IFrameCmd",
			Capabilities:  Capabilities{PrecisePTZ: true},
			PrecisePTZ:    true,
			Responses:     defaultResponsePolicy(),
		}
	}
	return Profile{
		Version:       Version(Version2016),
		Charset:       Charset(CharsetGB2312),
		IFrameElement: "IFameCmd",
		Capabilities:  Capabilities{PrecisePTZ: false},
		PrecisePTZ:    false,
		Responses:     defaultResponsePolicy(),
	}
}

// ProfileForVersion is the descriptive alias used by callers that want to
// make the version/profile relationship explicit.
func ProfileForVersion(version Version) Profile {
	return ProfileFor(version)
}

// CharsetFor returns the standard charset associated with a profile.
func CharsetFor(version Version) Charset {
	return ProfileFor(version).Charset
}

// IsSupportedCharset reports whether a charset can be intentionally selected
// by a protocol encoder. UTF-8 is retained for explicit vendor extensions; it
// is never selected implicitly by the 2016/2022 resolver.
func IsSupportedCharset(charset Charset) bool {
	switch charset {
	case Charset(CharsetGB2312), Charset(CharsetGB18030), Charset(CharsetUTF8):
		return true
	default:
		return false
	}
}

// ResponseFor returns the standard result semantics for an action. A missing
// action is deliberately treated as having no business response; callers can
// still reject unsupported actions at their own command-validation boundary.
func (p Profile) ResponseFor(action Action) ResponseSemantic {
	switch action {
	case ActionIFrame:
		return p.Responses.IFrame
	case ActionRecord:
		return p.Responses.Record
	case ActionGuard:
		return p.Responses.Guard
	case ActionAlarm:
		return p.Responses.Alarm
	case ActionTeleBoot:
		return p.Responses.TeleBoot
	case ActionDragZoom:
		return p.Responses.DragZoom
	case ActionPrecisePTZ:
		return p.Responses.PrecisePTZ
	default:
		return noBusinessResponse
	}
}

// ResponseRequired is a convenience predicate for schedulers that only need
// to decide whether to wait for an application response.
func (p Profile) ResponseRequired(action Action) bool {
	return p.ResponseFor(action).ResponseRequired
}

// ResultRequired is a convenience predicate for operation state machines that
// distinguish a transport acknowledgement from a device Result value.
func (p Profile) ResultRequired(action Action) bool {
	return p.ResponseFor(action).ResultRequired
}

// SupportsPrecisePTZ reports the standard precise PTZ capability of this
// profile. It does not inspect or infer device capability declarations.
func (p Profile) SupportsPrecisePTZ() bool {
	return p.Capabilities.PrecisePTZ || p.PrecisePTZ
}

// AdvertisedVersion is the normalized result of parsing X-GB-Ver.
type AdvertisedVersion struct {
	Version     Version
	Raw         string
	WarningCode WarningCode
	Warning     string
}

// ResolveAdvertisedVersion maps the values defined by the two protocol
// generations. GB/T 28181-2011 values use the 2016 compatibility path because
// no separate wire profile is implemented; malformed or unknown values are
// retained as warnings instead of changing protocol generation implicitly.
func ResolveAdvertisedVersion(raw string) AdvertisedVersion {
	return resolveRawVersion(raw, false)
}

func resolveRawVersion(raw string, allowCanonical bool) AdvertisedVersion {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return AdvertisedVersion{
			Version:     Version(Version2016),
			WarningCode: WarningMissingVersion,
			Warning:     warningText(WarningMissingVersion, trimmed),
		}
	}

	if allowCanonical {
		switch trimmed {
		case Version2016:
			return AdvertisedVersion{Version: Version(Version2016), Raw: trimmed}
		case Version2022:
			return AdvertisedVersion{Version: Version(Version2022), Raw: trimmed}
		}
	}

	major, minor, syntacticallyVersioned := splitVersion(trimmed)
	if !syntacticallyVersioned {
		return AdvertisedVersion{
			Version:     Version(Version2016),
			Raw:         trimmed,
			WarningCode: WarningInvalidVersion,
			Warning:     warningText(WarningInvalidVersion, trimmed),
		}
	}

	switch {
	case major == 1:
		return AdvertisedVersion{
			Version: Version(Version2016),
			Raw:     trimmed,
		}
	case major == 2 && minor == 0:
		return AdvertisedVersion{Version: Version(Version2016), Raw: trimmed}
	case major == 3 && minor == 0:
		return AdvertisedVersion{Version: Version(Version2022), Raw: trimmed}
	default:
		return AdvertisedVersion{
			Version:     Version(Version2016),
			Raw:         trimmed,
			WarningCode: WarningUnknownVersion,
			Warning:     warningText(WarningUnknownVersion, trimmed),
		}
	}
}

func splitVersion(raw string) (major, minor int, ok bool) {
	parts := strings.Split(raw, ".")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return 0, 0, false
	}
	major, errMajor := strconv.Atoi(parts[0])
	minor, errMinor := strconv.Atoi(parts[1])
	return major, minor, errMajor == nil && errMinor == nil && major >= 0 && minor >= 0
}

func warningText(code WarningCode, raw string) string {
	switch code {
	case WarningMissingVersion:
		return "设备未声明 X-GB-Ver，按 2016 兼容 profile 运行"
	case WarningLegacyVersion:
		return fmt.Sprintf("设备声明 GB/T 28181-2011 版本 %q，按 2016 兼容 profile 运行", raw)
	case WarningInvalidVersion:
		return fmt.Sprintf("设备 X-GB-Ver %q 格式无效，按 2016 兼容 profile 运行", raw)
	case WarningUnknownVersion:
		return fmt.Sprintf("设备 X-GB-Ver %q 无法识别，按 2016 兼容 profile 运行", raw)
	case WarningInvalidCharset:
		return fmt.Sprintf("显式字符集 %q 不受支持，使用 profile 默认字符集", raw)
	default:
		return ""
	}
}

// ResolveInput contains the independently persisted sources used to resolve
// a new operation. Override/Register/History are raw values so the resolver
// can preserve a warning for malformed data rather than silently discarding
// it. RegisterVersion and HistoryVersion are compatibility aliases for
// callers that used the first draft of this value object.
type ResolveInput struct {
	Override        string
	Register        string
	RegisterVersion string
	History         string
	HistoryVersion  *Version
	CharsetOverride string
}

// Resolution is the immutable decision consumed by an operation serializer.
// It is comparable and contains no mutable maps or slices, so callers can
// safely retain it as an operation snapshot.
type Resolution struct {
	Profile            Profile
	Source             Source
	RawVersion         string
	RawRegisterVersion string
	WarningCode        WarningCode
	Warning            string
}

// Resolve applies the documented priority: override > current REGISTER
// declaration > validated history > default 2016. An invalid non-empty value
// remains authoritative at its source and resolves to 2016 with a warning;
// it is never silently replaced by a lower-priority source.
func Resolve(input ResolveInput) Resolution {
	override := strings.TrimSpace(input.Override)
	if override != "" && override != VersionAuto {
		return resolveCandidate(override, SourceOverride, input.CharsetOverride)
	}

	register := input.Register
	if strings.TrimSpace(register) == "" {
		register = input.RegisterVersion
	}
	if strings.TrimSpace(register) != "" {
		return resolveCandidate(register, SourceRegister, input.CharsetOverride)
	}

	history := strings.TrimSpace(input.History)
	if history == "" && input.HistoryVersion != nil {
		history = string(*input.HistoryVersion)
	}
	if history != "" {
		return resolveCandidate(history, SourceHistory, input.CharsetOverride)
	}

	return resolveCandidate("", SourceDefault, input.CharsetOverride)
}

// ResolveProfile is an explicit alias for code that reads more naturally at
// a profile boundary.
func ResolveProfile(input ResolveInput) Resolution {
	return Resolve(input)
}

func resolveCandidate(raw string, source Source, charsetOverride string) Resolution {
	parsed := resolveRawVersion(raw, true)
	profile := ProfileFor(parsed.Version)
	warningCode := parsed.WarningCode
	warning := parsed.Warning

	if override := strings.TrimSpace(charsetOverride); override != "" {
		charset := normalizeCharset(override)
		if IsSupportedCharset(charset) {
			profile.Charset = charset
		} else {
			if warningCode == WarningNone {
				warningCode = WarningInvalidCharset
				warning = warningText(WarningInvalidCharset, override)
			}
		}
	}

	trimmed := strings.TrimSpace(raw)
	rawRegisterVersion := ""
	if source == SourceRegister {
		rawRegisterVersion = trimmed
	}
	return Resolution{
		Profile:            profile,
		Source:             source,
		RawVersion:         trimmed,
		RawRegisterVersion: rawRegisterVersion,
		WarningCode:        warningCode,
		Warning:            warning,
	}
}

func normalizeCharset(raw string) Charset {
	switch strings.ToUpper(strings.TrimSpace(raw)) {
	case "GB2312", "GBK":
		return Charset(CharsetGB2312)
	case "GB18030":
		return Charset(CharsetGB18030)
	case "UTF8", "UTF-8":
		return Charset(CharsetUTF8)
	default:
		return Charset(strings.TrimSpace(raw))
	}
}

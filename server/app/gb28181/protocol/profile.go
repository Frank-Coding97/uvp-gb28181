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
// These are version-derived, not device-reported. Device-reported capability
// hints are intentionally not represented here; those hints must never become
// a sending gate for standard controls.
type Capabilities struct {
	PrecisePTZ bool
	// HomePositionQuery is the 2022-only "看守位信息查询" command. The control
	// half (DeviceControl/HomePosition) exists since 2016 and is always
	// available; only the read-back query is version-gated.
	HomePositionQuery bool
	// CruiseTrackQuery is the 2022-only "巡航轨迹列表查询 / 巡航轨迹查询"
	// command pair. 同 HomePositionQuery:控制层(PTZCmd 0x84~0x88)2016 就有,
	// 只有**回读**这一半是 2022 新增(标准修订说明:9.5.3、A.2.4.10~A.2.4.14)。
	CruiseTrackQuery bool
	// VideoParamAttribute is the 2022-only "视频参数属性" **配置类型**
	// (A.2.1.13 / A.2.3.2.5)。⛔ 注意它的粒度比上面两个细一层:
	// `DeviceConfig` / `ConfigDownload` 这两条**命令** 2016 就有
	// (2016 的 ConfigType 有 4 个取值),2022 新增的是**配置类型清单**里的
	// VideoParamAttribute / VideoRecordPlan / … 那 8 项。
	// 所以"命令发得出去"与"设备认这个类型"是两件事 —— 详见
	// docs/gb28181-2022-device-config-ambiguity.md §六 与
	// docs/gb28181-2022-video-param-attribute-panel.md §十。
	//
	// ⚠️ 当前**没有调用方**,刻意保留为"平台自己决定要发写入"的判据位:
	// 操作员手点「下发」、以及 ack 之后自动追加的**回读**对账都不该被它挡
	// —— 前者是"不试一次就永远用不了"的探明手段,后者是一次读、最坏只烧一个 SN,
	// 而它恰好是判定"设备到底认不认这个类型"的唯一可靠依据。
	VideoParamAttribute bool
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
	// TargetTrack 是 GB/T 28181-2022 A.2.3.1.14 目标跟踪。
	// ⛔ 它是**无应答命令**，两处标准原文：① 9.3.1 d) 把"目标跟踪"与云台控制 / 远程启动 /
	// 强制关键帧 / 拉框放大缩小 / PTZ 精准控制 / 存储卡格式化列在同一句 ——
	// 「目标设备**不发送应答命令**」；② 表 1 序号 13「目标跟踪 / A.2.3.1.14 /（无）」。
	// 设成 businessResponse 的后果不是"多等一会儿"，而是把一个成功当失败报：设备按标准
	// 不回执 → operation 排到 transport deadline 才落 timeout → 前端把正常下发显示成
	// 「结果未知」（同族先例 FormatSDCard 已在真机上反证过这一点）。
	TargetTrack ResponseSemantic
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
	ActionIFrame      Action = "iframe"
	ActionRecord      Action = "record"
	ActionGuard       Action = "guard"
	ActionAlarm       Action = "alarm"
	ActionTeleBoot    Action = "teleboot"
	ActionDragZoom    Action = "drag_zoom"
	ActionPrecisePTZ  Action = "precise_ptz"
	ActionTargetTrack Action = "target_track"

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
		IFrame:      noBusinessResponse,
		Record:      businessResponse,
		Guard:       businessResponse,
		Alarm:       businessResponse,
		TeleBoot:    noBusinessResponse,
		DragZoom:    noBusinessResponse,
		PrecisePTZ:  noBusinessResponse,
		TargetTrack: noBusinessResponse,
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
			Capabilities:  Capabilities{PrecisePTZ: true, HomePositionQuery: true, CruiseTrackQuery: true, VideoParamAttribute: true},
			PrecisePTZ:    true,
			Responses:     defaultResponsePolicy(),
		}
	}
	return Profile{
		Version:       Version(Version2016),
		Charset:       Charset(CharsetGB2312),
		IFrameElement: "IFameCmd",
		Capabilities:  Capabilities{PrecisePTZ: false, HomePositionQuery: false, CruiseTrackQuery: false, VideoParamAttribute: false},
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
	case ActionTargetTrack:
		return p.Responses.TargetTrack
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

// SupportsHomePositionQuery reports whether this profile may send the
// HomePositionQuery command. 看守位信息查询 is a 2022 addition; a 2016 device
// has no defined behaviour for it, so sending one only burns an SN and a
// three-attempt retry budget before timing out.
func (p Profile) SupportsHomePositionQuery() bool {
	return p.Capabilities.HomePositionQuery
}

// SupportsCruiseTrackQuery reports whether this profile may send the
// CruiseTrackListQuery / CruiseTrackQuery pair. 巡航轨迹查询 is a 2022
// addition, so a 2016 device has no defined behaviour for it — sending one
// only burns an SN and a three-attempt retry budget before timing out.
//
// ⚠️ 只用于**自动对账**这类"平台自己决定要发"的场合。操作员点「同步」触发的那次
// 查询**不能**用它挡 —— 门禁只拦平台自发,不拦操作员点出来的(同 SupportsHomePositionQuery)。
func (p Profile) SupportsCruiseTrackQuery() bool {
	return p.Capabilities.CruiseTrackQuery
}

// SupportsVideoParamAttribute reports whether this profile's ConfigType list
// includes "视频参数属性" — a 2022 addition (2016 has only 4 config types).
//
// ⚠️ 当前**没有调用方**，且刻意如此。它只为"平台自己决定要发一次写入"这类
// 动作准备判断位；下面两种情况都**不该**被它挡：
//
//   - 操作员手点「下发」：被误登记成 2016 的真 2022 设备，不试一次就永远
//     用不了这个功能（同 SupportsHomePositionQuery 的既有口径）。
//   - ack 之后自动追加的**回读**对账：那是一次读，最坏结果只是超时烧一个 SN，
//     而它正是"设备到底认不认这个类型"的唯一可靠依据 —— 挡掉判定手段等于
//     把结论也一起挡掉。
//
// 判定"设备不支持"必须**以回读结果为准**（应答里没有 `VideoParamAttribute`
// 元素），不以本方法为准：登记值只是"登记的说法"，回读才是设备的实际回答。
func (p Profile) SupportsVideoParamAttribute() bool {
	return p.Capabilities.VideoParamAttribute
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
	case major == 1 && (minor == 0 || minor == 1):
		return AdvertisedVersion{
			Version:     Version(Version2016),
			Raw:         trimmed,
			WarningCode: WarningLegacyVersion,
			Warning:     warningText(WarningLegacyVersion, trimmed),
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

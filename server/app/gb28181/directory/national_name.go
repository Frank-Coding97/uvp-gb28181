package directory

import (
	"strings"
	"unicode"

	"uvplatform.cn/uvp-gb28181/app/gb28181/civilcode"
)

type UnknownReason string

const (
	UnknownInvalidCode     UnknownReason = "INVALID_CODE"
	UnknownDictionaryMiss  UnknownReason = "DICTIONARY_MISS"
	UnknownMissingName     UnknownReason = "MISSING_NAME"
	UnknownPlaceholderName UnknownReason = "PLACEHOLDER_NAME"
)

type CivilCodeLookup interface {
	Lookup(code string) *civilcode.SysCivilCode
}

type CivilCodeProjection struct {
	Key          string        `json:"key"`
	Code         string        `json:"code"`
	StandardCode string        `json:"standardCode"`
	Name         string        `json:"name"`
	Level        int8          `json:"level"`
	ParentCode   string        `json:"parentCode"`
	RawCode      string        `json:"rawCode"`
	Reason       UnknownReason `json:"reason,omitempty"`
}

func ProjectCivilCode(rawCode, reportedName string, lookup CivilCodeLookup) CivilCodeProjection {
	rawCode = strings.TrimSpace(rawCode)
	result := CivilCodeProjection{RawCode: rawCode}
	if !validDigits(rawCode) || (len(rawCode) != 2 && len(rawCode) != 4 && len(rawCode) != 6 && len(rawCode) != 8) {
		result.Reason = UnknownInvalidCode
		return result
	}

	standard := rawCode
	switch len(rawCode) {
	case 2:
		standard += "0000"
	case 4:
		standard += "00"
	case 8:
		standard = rawCode[:6]
	}
	entry := lookup.Lookup(standard)
	if entry == nil {
		result.Reason = UnknownDictionaryMiss
		return result
	}

	result.StandardCode = standard
	result.Level = entry.Level
	result.ParentCode = entry.ParentCode
	if len(rawCode) == 8 {
		name := strings.TrimSpace(reportedName)
		if name == "" {
			result.Reason = UnknownMissingName
			return result
		}
		if name == rawCode || strings.EqualFold(name, "行政区 "+rawCode) || strings.EqualFold(name, "行政区"+rawCode) {
			result.Reason = UnknownPlaceholderName
			return result
		}
		result.Name = name
		result.Code = rawCode
		result.ParentCode = standard
	} else {
		result.Code = standard
		result.Name = strings.TrimSpace(entry.ShortName)
		if result.Name == "" {
			result.Name = strings.TrimSpace(entry.Name)
		}
		if result.Name == "" {
			result.Reason = UnknownMissingName
			return result
		}
	}
	result.Key = "national:area:" + result.Code
	return result
}

func validDigits(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r > unicode.MaxASCII || !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

package config

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"uvplatform.com/uvp-gb28181/app/global/app"
)

const (
	CascadeUserAgentConfigKey    = "gb28181.cascade.user_agent"
	CascadePlatformNameConfigKey = "gb28181.cascade.platform_name"
	CascadeManufacturerConfigKey = "gb28181.cascade.manufacturer"
	CascadeModelConfigKey        = "gb28181.cascade.model"
	CascadeCivilCodeConfigKey    = "gb28181.cascade.civil_code"
	CascadeAddressConfigKey      = "gb28181.cascade.address"
)

type CascadeSettings struct {
	UserAgent    string `json:"userAgent"`
	PlatformName string `json:"platformName"`
	Manufacturer string `json:"manufacturer"`
	Model        string `json:"model"`
	CivilCode    string `json:"civilCode"`
	Address      string `json:"address"`
}

func DefaultCascadeSettings() CascadeSettings {
	return CascadeSettings{UserAgent: "UVP-GB28181", PlatformName: "UVP GB28181", Manufacturer: "UVP", Model: "UVP-GB28181"}
}

func CascadeSettingsFrom(c valueSource) CascadeSettings {
	settings := DefaultCascadeSettings()
	if c == nil {
		return settings
	}
	for _, field := range []struct {
		key      string
		target   *string
		max      int
		required bool
	}{
		{CascadeUserAgentConfigKey, &settings.UserAgent, 128, true},
		{CascadePlatformNameConfigKey, &settings.PlatformName, 128, true},
		{CascadeManufacturerConfigKey, &settings.Manufacturer, 128, true},
		{CascadeModelConfigKey, &settings.Model, 128, true},
		{CascadeAddressConfigKey, &settings.Address, 256, false},
	} {
		if c.Get(field.key) != nil {
			value := c.GetString(field.key)
			if validCascadeText(value, field.max, field.required) {
				*field.target = strings.TrimSpace(value)
			}
		}
	}
	if value := c.GetString(CascadeCivilCodeConfigKey); validCascadeCivilCode(value) {
		settings.CivilCode = value
	}
	return settings
}

func CurrentCascadeSettings() CascadeSettings {
	return CascadeSettingsFrom(app.ConfigYml)
}

func ValidateCascadeSettings(settings CascadeSettings) error {
	for _, field := range []struct {
		label    string
		value    string
		max      int
		required bool
	}{
		{"User-Agent", settings.UserAgent, 128, true},
		{"平台名称", settings.PlatformName, 128, true},
		{"厂商", settings.Manufacturer, 128, true},
		{"型号", settings.Model, 128, true},
		{"安装地址", settings.Address, 256, false},
	} {
		if !validCascadeText(field.value, field.max, field.required) {
			return fmt.Errorf("级联%s无效，最多%d个字符且不能包含控制字符", field.label, field.max)
		}
	}
	if !validCascadeCivilCode(settings.CivilCode) {
		return errors.New("级联行政区划必须为空或2、4、6、8位数字")
	}
	return nil
}

func validCascadeText(value string, max int, required bool) bool {
	return (!required || strings.TrimSpace(value) != "") && utf8.RuneCountInString(value) <= max && strings.IndexFunc(value, unicode.IsControl) == -1
}

func validCascadeCivilCode(value string) bool {
	if value == "" {
		return true
	}
	if len(value) != 2 && len(value) != 4 && len(value) != 6 && len(value) != 8 {
		return false
	}
	for _, digit := range value {
		if digit < '0' || digit > '9' {
			return false
		}
	}
	return true
}

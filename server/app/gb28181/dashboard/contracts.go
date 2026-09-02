package dashboard

import (
	"errors"
	"fmt"
	"math"
)

const CurrentSchemaVersion = 3

type SectionStatus string

const (
	StatusOK          SectionStatus = "ok"
	StatusEmpty       SectionStatus = "empty"
	StatusForbidden   SectionStatus = "forbidden"
	StatusDisabled    SectionStatus = "disabled"
	StatusUnavailable SectionStatus = "unavailable"
	StatusStale       SectionStatus = "stale"
	StatusPartial     SectionStatus = "partial"
)

type Coverage string

const (
	CoverageComplete   Coverage = "complete"
	CoveragePartial    Coverage = "partial"
	CoverageNotStarted Coverage = "not_started"
)

type ScopeType string

const (
	ScopeUserVisible ScopeType = "user-visible"
	ScopePlatform    ScopeType = "platform"
	ScopeNode        ScopeType = "node"
)

type Scope struct {
	Type   ScopeType `json:"type"`
	NodeID int64     `json:"nodeId,omitempty"`
}

type SectionEnvelope[T any] struct {
	Status   SectionStatus `json:"status"`
	AsOf     string        `json:"asOf"`
	Scope    Scope         `json:"scope"`
	Coverage Coverage      `json:"coverage"`
	Data     T             `json:"data"`
}

type WidgetLayout struct {
	ID       string         `json:"id"`
	X        int            `json:"x"`
	Y        int            `json:"y"`
	W        int            `json:"w"`
	H        int            `json:"h"`
	Visible  bool           `json:"visible"`
	Settings map[string]any `json:"settings"`
}

type Layout struct {
	SchemaVersion int            `json:"schemaVersion"`
	Widgets       []WidgetLayout `json:"widgets"`
}

type WidgetDefinition struct {
	ID   string
	MinW int
	MaxW int
	MinH int
	MaxH int
	Base WidgetLayout
}

var widgetDefinitions = []WidgetDefinition{
	widget("sip-rpm", 0, 0, 4, 3, true, 3, 7, 3, 4),
	widget("sip-today", 4, 0, 4, 3, true, 3, 7, 3, 4),
	widget("play-success-24h", 8, 0, 4, 3, true, 3, 7, 3, 4),
	widget("media-traffic-today", 12, 0, 4, 3, true, 3, 7, 3, 4),
	widget("media-runtime", 16, 0, 4, 3, true, 3, 7, 3, 4),
	widget("media-rate", 0, 3, 12, 5, true, 10, 20, 4, 8),
	widget("device-online-rate", 12, 3, 4, 5, true, 3, 7, 3, 7),
	widget("channel-online-rate", 16, 3, 4, 5, true, 3, 7, 3, 7),
	widget("active-stream-ranking", 0, 8, 14, 4, true, 7, 20, 3, 7),
	widget("media-node-health", 14, 8, 6, 4, true, 5, 10, 3, 7),
	widget("sip-monitor", 0, 12, 14, 5, false, 10, 20, 4, 8),
}

var legacyWidgetDefaults = map[string]WidgetLayout{
	"sip-rpm":               {ID: "sip-rpm", X: 0, Y: 0, W: 2, H: 2, Visible: true},
	"sip-today":             {ID: "sip-today", X: 2, Y: 0, W: 2, H: 2, Visible: true},
	"play-success-24h":      {ID: "play-success-24h", X: 4, Y: 0, W: 2, H: 2, Visible: true},
	"media-traffic-today":   {ID: "media-traffic-today", X: 6, Y: 0, W: 2, H: 2, Visible: false},
	"media-runtime":         {ID: "media-runtime", X: 8, Y: 0, W: 4, H: 2, Visible: true},
	"sip-monitor":           {ID: "sip-monitor", X: 0, Y: 2, W: 8, H: 5, Visible: true},
	"device-online-rate":    {ID: "device-online-rate", X: 8, Y: 2, W: 2, H: 5, Visible: true},
	"channel-online-rate":   {ID: "channel-online-rate", X: 10, Y: 2, W: 2, H: 5, Visible: true},
	"media-rate":            {ID: "media-rate", X: 0, Y: 7, W: 8, H: 4, Visible: true},
	"media-node-health":     {ID: "media-node-health", X: 8, Y: 7, W: 4, H: 4, Visible: true},
	"active-stream-ranking": {ID: "active-stream-ranking", X: 0, Y: 11, W: 12, H: 4, Visible: true},
}

var schema2WidgetDefaults = map[string]WidgetLayout{
	"sip-rpm":               {ID: "sip-rpm", X: 0, Y: 0, W: 4, H: 2, Visible: true},
	"sip-today":             {ID: "sip-today", X: 4, Y: 0, W: 4, H: 2, Visible: true},
	"play-success-24h":      {ID: "play-success-24h", X: 8, Y: 0, W: 4, H: 2, Visible: true},
	"device-online-rate":    {ID: "device-online-rate", X: 12, Y: 0, W: 4, H: 2, Visible: true},
	"channel-online-rate":   {ID: "channel-online-rate", X: 16, Y: 0, W: 4, H: 2, Visible: true},
	"media-traffic-today":   {ID: "media-traffic-today", X: 0, Y: 15, W: 4, H: 2, Visible: false},
	"media-runtime":         {ID: "media-runtime", X: 14, Y: 2, W: 6, H: 5, Visible: true},
	"sip-monitor":           {ID: "sip-monitor", X: 0, Y: 2, W: 14, H: 5, Visible: true},
	"media-rate":            {ID: "media-rate", X: 0, Y: 7, W: 14, H: 4, Visible: true},
	"media-node-health":     {ID: "media-node-health", X: 14, Y: 7, W: 6, H: 4, Visible: true},
	"active-stream-ranking": {ID: "active-stream-ranking", X: 0, Y: 11, W: 20, H: 4, Visible: true},
}

func widget(id string, x, y, w, h int, visible bool, minW, maxW, minH, maxH int) WidgetDefinition {
	return WidgetDefinition{
		ID: id, MinW: minW, MaxW: maxW, MinH: minH, MaxH: maxH,
		Base: WidgetLayout{ID: id, X: x, Y: y, W: w, H: h, Visible: visible, Settings: map[string]any{}},
	}
}

func DefaultLayout() Layout {
	result := Layout{SchemaVersion: CurrentSchemaVersion, Widgets: make([]WidgetLayout, 0, len(widgetDefinitions))}
	for _, definition := range widgetDefinitions {
		result.Widgets = append(result.Widgets, cloneWidget(definition.Base))
	}
	return result
}

func NormalizeLayout(layout Layout) (Layout, error) {
	if layout.SchemaVersion < 0 || layout.SchemaVersion > CurrentSchemaVersion {
		return Layout{}, fmt.Errorf("unsupported dashboard schema version %d", layout.SchemaVersion)
	}

	definitions := make(map[string]WidgetDefinition, len(widgetDefinitions))
	for _, definition := range widgetDefinitions {
		definitions[definition.ID] = definition
	}
	seen := make(map[string]bool, len(layout.Widgets))
	normalized := Layout{SchemaVersion: CurrentSchemaVersion, Widgets: make([]WidgetLayout, 0, len(widgetDefinitions))}
	for _, item := range layout.Widgets {
		definition, ok := definitions[item.ID]
		if !ok {
			return Layout{}, fmt.Errorf("unknown dashboard widget %q", item.ID)
		}
		if seen[item.ID] {
			return Layout{}, fmt.Errorf("duplicate dashboard widget %q", item.ID)
		}
		if layout.SchemaVersion < CurrentSchemaVersion {
			item = migrateLegacyWidget(item, definition, layout.SchemaVersion)
		}
		if err := validateWidget(item, definition); err != nil {
			return Layout{}, err
		}
		seen[item.ID] = true
		normalized.Widgets = append(normalized.Widgets, cloneWidget(item))
	}
	for _, definition := range widgetDefinitions {
		if !seen[definition.ID] {
			normalized.Widgets = append(normalized.Widgets, cloneWidget(definition.Base))
		}
	}
	return normalized, nil
}

func migrateLegacyWidget(item WidgetLayout, definition WidgetDefinition, schemaVersion int) WidgetLayout {
	defaults, sourceColumns := legacyWidgetDefaults, 12
	if schemaVersion == 2 {
		defaults, sourceColumns = schema2WidgetDefaults, 20
	}
	legacy, matchesDefault := defaults[item.ID]
	if matchesDefault && sameWidgetPlacement(item, legacy) {
		migrated := cloneWidget(definition.Base)
		migrated.Settings = item.Settings
		return migrated
	}
	migrated := cloneWidget(item)
	migrated.X = int(math.Round(float64(item.X) * 20 / float64(sourceColumns)))
	migrated.W = int(math.Round(float64(item.W) * 20 / float64(sourceColumns)))
	if migrated.W < definition.MinW {
		migrated.W = definition.MinW
	}
	if migrated.W > definition.MaxW {
		migrated.W = definition.MaxW
	}
	if migrated.H < definition.MinH {
		migrated.H = definition.MinH
	}
	if migrated.H > definition.MaxH {
		migrated.H = definition.MaxH
	}
	if migrated.X+migrated.W > 20 {
		migrated.X = 20 - migrated.W
	}
	return migrated
}

func sameWidgetPlacement(item, expected WidgetLayout) bool {
	return item.X == expected.X && item.Y == expected.Y && item.W == expected.W && item.H == expected.H && item.Visible == expected.Visible
}

func validateWidget(item WidgetLayout, definition WidgetDefinition) error {
	if item.X < 0 || item.Y < 0 || item.W < definition.MinW || item.W > definition.MaxW || item.H < definition.MinH || item.H > definition.MaxH {
		return fmt.Errorf("invalid geometry for dashboard widget %q", item.ID)
	}
	if item.X+item.W > 20 {
		return fmt.Errorf("dashboard widget %q exceeds 20 columns", item.ID)
	}
	if len(item.Settings) != 0 {
		return errors.New("dashboard widget settings are not supported")
	}
	return nil
}

func cloneWidget(item WidgetLayout) WidgetLayout {
	clone := item
	clone.Settings = make(map[string]any, len(item.Settings))
	for key, value := range item.Settings {
		clone.Settings[key] = value
	}
	return clone
}

package dashboard

import (
	"errors"
	"fmt"
)

const CurrentSchemaVersion = 1

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
	widget("sip-rpm", 0, 0, 2, 2, true, 2, 4, 2, 3),
	widget("sip-today", 2, 0, 2, 2, true, 2, 4, 2, 3),
	widget("play-success-24h", 4, 0, 2, 2, true, 2, 4, 2, 3),
	widget("media-traffic-today", 6, 0, 2, 2, false, 2, 4, 2, 3),
	widget("media-runtime", 8, 0, 4, 2, true, 3, 6, 2, 3),
	widget("sip-monitor", 0, 2, 8, 5, true, 6, 12, 4, 8),
	widget("device-online-rate", 8, 2, 2, 5, true, 2, 4, 3, 6),
	widget("channel-online-rate", 10, 2, 2, 5, true, 2, 4, 3, 6),
	widget("media-rate", 0, 7, 8, 4, true, 6, 12, 3, 7),
	widget("media-node-health", 8, 7, 4, 4, true, 3, 6, 3, 7),
	widget("active-stream-ranking", 0, 11, 12, 4, true, 4, 12, 3, 7),
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

func validateWidget(item WidgetLayout, definition WidgetDefinition) error {
	if item.X < 0 || item.Y < 0 || item.W < definition.MinW || item.W > definition.MaxW || item.H < definition.MinH || item.H > definition.MaxH {
		return fmt.Errorf("invalid geometry for dashboard widget %q", item.ID)
	}
	if item.X+item.W > 12 {
		return fmt.Errorf("dashboard widget %q exceeds 12 columns", item.ID)
	}
	if len(item.Settings) != 0 {
		return errors.New("dashboard widget settings are not supported in schema v1")
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

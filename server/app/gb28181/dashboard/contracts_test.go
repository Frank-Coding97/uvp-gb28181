package dashboard

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDefaultLayoutContainsEveryWidgetOnce(t *testing.T) {
	layout := DefaultLayout()
	require.Equal(t, CurrentSchemaVersion, layout.SchemaVersion)
	require.Len(t, layout.Widgets, 12)

	seen := make(map[string]bool, len(layout.Widgets))
	for _, widget := range layout.Widgets {
		require.False(t, seen[widget.ID], "duplicate widget %s", widget.ID)
		seen[widget.ID] = true
	}
	for index, id := range []string{"sip-rpm", "sip-today", "play-success-24h", "media-traffic-today", "media-runtime"} {
		widget := widgetByID(t, layout, id)
		require.True(t, widget.Visible, "%s must be visible in the reference first row", id)
		require.Equal(t, index*4, widget.X)
		require.Equal(t, 4, widget.W)
		require.Equal(t, 2, widget.H)
		require.Zero(t, widget.Y)
	}
	require.Equal(t, WidgetLayout{ID: "sip-monitor", X: 0, Y: 6, W: 12, H: 8, Visible: true, Settings: map[string]any{}}, widgetByID(t, layout, "sip-monitor"))
	require.Equal(t, WidgetLayout{ID: "media-node-health", X: 12, Y: 6, W: 8, H: 5, Visible: true, Settings: map[string]any{}}, widgetByID(t, layout, "media-node-health"))
	require.Equal(t, WidgetLayout{ID: "platform-info", X: 12, Y: 11, W: 8, H: 3, Visible: true, Settings: map[string]any{}}, widgetByID(t, layout, "platform-info"))
	require.False(t, widgetByID(t, layout, "active-stream-ranking").Visible)
	require.Equal(t, WidgetLayout{ID: "media-rate", X: 0, Y: 2, W: 12, H: 4, Visible: true, Settings: map[string]any{}}, widgetByID(t, layout, "media-rate"))
	for index, id := range []string{"device-online-rate", "channel-online-rate"} {
		widget := widgetByID(t, layout, id)
		require.Equal(t, 12+index*4, widget.X)
		require.Equal(t, 2, widget.Y)
		require.Equal(t, 4, widget.W)
		require.Equal(t, 4, widget.H)
	}
}

func TestNormalizeLayoutMigratesLegacyReferenceRowWithoutGap(t *testing.T) {
	legacy := Layout{SchemaVersion: 1, Widgets: []WidgetLayout{
		{ID: "sip-rpm", X: 0, Y: 0, W: 2, H: 2, Visible: true, Settings: map[string]any{}},
		{ID: "sip-today", X: 2, Y: 0, W: 2, H: 2, Visible: true, Settings: map[string]any{}},
		{ID: "play-success-24h", X: 4, Y: 0, W: 2, H: 2, Visible: true, Settings: map[string]any{}},
		{ID: "media-runtime", X: 8, Y: 0, W: 4, H: 2, Visible: true, Settings: map[string]any{}},
		{ID: "device-online-rate", X: 8, Y: 2, W: 2, H: 5, Visible: true, Settings: map[string]any{}},
		{ID: "channel-online-rate", X: 10, Y: 2, W: 2, H: 5, Visible: true, Settings: map[string]any{}},
	}}

	normalized, err := NormalizeLayout(legacy)
	require.NoError(t, err)
	require.Equal(t, CurrentSchemaVersion, normalized.SchemaVersion)
	for index, id := range []string{"sip-rpm", "sip-today", "play-success-24h", "media-traffic-today", "media-runtime"} {
		widget := widgetByID(t, normalized, id)
		require.True(t, widget.Visible)
		require.Equal(t, index*4, widget.X)
		require.Equal(t, 4, widget.W)
	}
	require.True(t, widgetByID(t, normalized, "sip-monitor").Visible)
	require.Equal(t, 12, widgetByID(t, normalized, "device-online-rate").X)
	require.Equal(t, 16, widgetByID(t, normalized, "channel-online-rate").X)
	require.False(t, widgetByID(t, normalized, "active-stream-ranking").Visible)
	require.True(t, widgetByID(t, normalized, "media-node-health").Visible)

	current := Layout{SchemaVersion: 2, Widgets: DefaultLayout().Widgets}
	for index := range current.Widgets {
		current.Widgets[index] = schema2WidgetByID(t, current.Widgets[index].ID)
	}
	normalized, err = NormalizeLayout(current)
	require.NoError(t, err)
	for index, id := range []string{"sip-rpm", "sip-today", "play-success-24h", "media-traffic-today", "media-runtime"} {
		widget := widgetByID(t, normalized, id)
		require.Equal(t, index*4, widget.X)
		require.Zero(t, widget.Y)
		require.True(t, widget.Visible)
	}
	require.Equal(t, 12, widgetByID(t, normalized, "device-online-rate").X)
	require.Equal(t, 16, widgetByID(t, normalized, "channel-online-rate").X)
}

func TestNormalizeLayoutMigratesSchema3DefaultsToCompactOptionalLayout(t *testing.T) {
	legacy := Layout{SchemaVersion: 3, Widgets: []WidgetLayout{
		{ID: "sip-rpm", X: 0, Y: 0, W: 4, H: 3, Visible: true, Settings: map[string]any{}},
		{ID: "sip-today", X: 4, Y: 0, W: 4, H: 3, Visible: true, Settings: map[string]any{}},
		{ID: "play-success-24h", X: 8, Y: 0, W: 4, H: 3, Visible: true, Settings: map[string]any{}},
		{ID: "media-traffic-today", X: 12, Y: 0, W: 4, H: 3, Visible: true, Settings: map[string]any{}},
		{ID: "media-runtime", X: 16, Y: 0, W: 4, H: 3, Visible: true, Settings: map[string]any{}},
		{ID: "media-rate", X: 0, Y: 3, W: 12, H: 5, Visible: true, Settings: map[string]any{}},
		{ID: "device-online-rate", X: 12, Y: 3, W: 4, H: 5, Visible: true, Settings: map[string]any{}},
		{ID: "channel-online-rate", X: 16, Y: 3, W: 4, H: 5, Visible: true, Settings: map[string]any{}},
		{ID: "active-stream-ranking", X: 0, Y: 8, W: 14, H: 4, Visible: true, Settings: map[string]any{}},
		{ID: "media-node-health", X: 14, Y: 8, W: 6, H: 4, Visible: true, Settings: map[string]any{}},
		{ID: "sip-monitor", X: 0, Y: 12, W: 14, H: 5, Visible: false, Settings: map[string]any{}},
	}}
	normalized, err := NormalizeLayout(legacy)
	require.NoError(t, err)
	require.Equal(t, DefaultLayout(), normalized)
}

func TestNormalizeLayoutMigratesSchema4DefaultsToSIPMonitorAndPlatformInfo(t *testing.T) {
	legacy := Layout{SchemaVersion: 4}
	for _, id := range []string{"sip-rpm", "sip-today", "play-success-24h", "media-traffic-today", "media-runtime", "media-rate", "device-online-rate", "channel-online-rate", "active-stream-ranking", "media-node-health", "sip-monitor"} {
		item := schema4WidgetDefaults[id]
		item.Settings = map[string]any{}
		legacy.Widgets = append(legacy.Widgets, item)
	}
	normalized, err := NormalizeLayout(legacy)
	require.NoError(t, err)
	require.Equal(t, DefaultLayout(), normalized)
}

func TestNormalizeLayoutMigratesSchema5DefaultsToEqualHeightSIPAndRightStack(t *testing.T) {
	legacy := Layout{SchemaVersion: 5}
	for _, id := range []string{"sip-rpm", "sip-today", "play-success-24h", "media-traffic-today", "media-runtime", "media-rate", "device-online-rate", "channel-online-rate", "active-stream-ranking", "media-node-health", "sip-monitor", "platform-info"} {
		item := schema5WidgetDefaults[id]
		item.Settings = map[string]any{}
		legacy.Widgets = append(legacy.Widgets, item)
	}
	normalized, err := NormalizeLayout(legacy)
	require.NoError(t, err)
	require.Equal(t, DefaultLayout(), normalized)
}

func TestNormalizeLayoutMigratesSchema6DefaultsToTallerSIPAndRightStack(t *testing.T) {
	legacy := Layout{SchemaVersion: 6}
	for _, id := range []string{"sip-rpm", "sip-today", "play-success-24h", "media-traffic-today", "media-runtime", "media-rate", "device-online-rate", "channel-online-rate", "active-stream-ranking", "media-node-health", "sip-monitor", "platform-info"} {
		item := schema6WidgetDefaults[id]
		item.Settings = map[string]any{}
		legacy.Widgets = append(legacy.Widgets, item)
	}
	normalized, err := NormalizeLayout(legacy)
	require.NoError(t, err)
	require.Equal(t, DefaultLayout(), normalized)
}

func TestNormalizeLayoutRejectsInvalidGeometryAndUnknownWidgets(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Layout)
	}{
		{name: "unknown", mutate: func(layout *Layout) { layout.Widgets[0].ID = "unknown" }},
		{name: "duplicate", mutate: func(layout *Layout) { layout.Widgets[1].ID = layout.Widgets[0].ID }},
		{name: "negative", mutate: func(layout *Layout) { layout.Widgets[0].X = -1 }},
		{name: "too-small", mutate: func(layout *Layout) { layout.Widgets[0].W = 0 }},
		{name: "overflow", mutate: func(layout *Layout) { layout.Widgets[0].X = 11; layout.Widgets[0].W = 2 }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			layout := DefaultLayout()
			tt.mutate(&layout)
			_, err := NormalizeLayout(layout)
			require.Error(t, err)
		})
	}
}

func TestNormalizeLayoutMigratesKnownWidgetsAndAddsMissingDefaults(t *testing.T) {
	legacy := Layout{SchemaVersion: 0, Widgets: []WidgetLayout{{ID: "sip-rpm", X: 3, Y: 4, W: 2, H: 2, Visible: true}}}
	normalized, err := NormalizeLayout(legacy)
	require.NoError(t, err)
	require.Equal(t, CurrentSchemaVersion, normalized.SchemaVersion)
	require.Len(t, normalized.Widgets, 12)
	require.Equal(t, 5, widgetByID(t, normalized, "sip-rpm").X)
}

func schema2WidgetByID(t *testing.T, id string) WidgetLayout {
	t.Helper()
	widgets := map[string]WidgetLayout{
		"sip-rpm":               {ID: "sip-rpm", X: 0, Y: 0, W: 4, H: 2, Visible: true, Settings: map[string]any{}},
		"sip-today":             {ID: "sip-today", X: 4, Y: 0, W: 4, H: 2, Visible: true, Settings: map[string]any{}},
		"play-success-24h":      {ID: "play-success-24h", X: 8, Y: 0, W: 4, H: 2, Visible: true, Settings: map[string]any{}},
		"device-online-rate":    {ID: "device-online-rate", X: 12, Y: 0, W: 4, H: 2, Visible: true, Settings: map[string]any{}},
		"channel-online-rate":   {ID: "channel-online-rate", X: 16, Y: 0, W: 4, H: 2, Visible: true, Settings: map[string]any{}},
		"media-traffic-today":   {ID: "media-traffic-today", X: 0, Y: 15, W: 4, H: 2, Visible: false, Settings: map[string]any{}},
		"media-runtime":         {ID: "media-runtime", X: 14, Y: 2, W: 6, H: 5, Visible: true, Settings: map[string]any{}},
		"sip-monitor":           {ID: "sip-monitor", X: 0, Y: 2, W: 14, H: 5, Visible: true, Settings: map[string]any{}},
		"media-rate":            {ID: "media-rate", X: 0, Y: 7, W: 14, H: 4, Visible: true, Settings: map[string]any{}},
		"media-node-health":     {ID: "media-node-health", X: 14, Y: 7, W: 6, H: 4, Visible: true, Settings: map[string]any{}},
		"active-stream-ranking": {ID: "active-stream-ranking", X: 0, Y: 11, W: 20, H: 4, Visible: true, Settings: map[string]any{}},
		"platform-info":         {ID: "platform-info", X: 12, Y: 6, W: 8, H: 2, Visible: true, Settings: map[string]any{}},
	}
	widget, ok := widgets[id]
	require.True(t, ok, "schema 2 widget %s not found", id)
	return widget
}

func TestSectionEnvelopeSerializesStableStateContract(t *testing.T) {
	envelope := SectionEnvelope[map[string]int]{
		Status:   StatusPartial,
		Scope:    Scope{Type: ScopeNode, NodeID: 2},
		Coverage: CoveragePartial,
		Data:     map[string]int{"streams": 3},
	}
	data, err := json.Marshal(envelope)
	require.NoError(t, err)
	require.JSONEq(t, `{"status":"partial","asOf":"","scope":{"type":"node","nodeId":2},"coverage":"partial","data":{"streams":3}}`, string(data))
}

func widgetByID(t *testing.T, layout Layout, id string) WidgetLayout {
	t.Helper()
	for _, widget := range layout.Widgets {
		if widget.ID == id {
			return widget
		}
	}
	t.Fatalf("widget %s not found", id)
	return WidgetLayout{}
}

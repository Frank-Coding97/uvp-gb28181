package dashboard

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDefaultLayoutContainsEveryWidgetOnce(t *testing.T) {
	layout := DefaultLayout()
	require.Equal(t, CurrentSchemaVersion, layout.SchemaVersion)
	require.Len(t, layout.Widgets, 11)

	seen := make(map[string]bool, len(layout.Widgets))
	for _, widget := range layout.Widgets {
		require.False(t, seen[widget.ID], "duplicate widget %s", widget.ID)
		seen[widget.ID] = true
	}
	for index, id := range []string{"sip-rpm", "sip-today", "play-success-24h", "device-online-rate", "channel-online-rate"} {
		widget := widgetByID(t, layout, id)
		require.True(t, widget.Visible, "%s must be visible in the reference five-card row", id)
		require.Equal(t, index*4, widget.X)
		require.Equal(t, 4, widget.W)
		require.Zero(t, widget.Y)
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
	for index, id := range []string{"sip-rpm", "sip-today", "play-success-24h", "device-online-rate", "channel-online-rate"} {
		widget := widgetByID(t, normalized, id)
		require.True(t, widget.Visible)
		require.Equal(t, index*4, widget.X)
		require.Equal(t, 4, widget.W)
	}
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
	require.Len(t, normalized.Widgets, 11)
	require.Equal(t, 5, widgetByID(t, normalized, "sip-rpm").X)
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

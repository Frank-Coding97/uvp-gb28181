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
	require.False(t, widgetByID(t, layout, "media-traffic-today").Visible)
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
	require.Equal(t, 3, widgetByID(t, normalized, "sip-rpm").X)
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

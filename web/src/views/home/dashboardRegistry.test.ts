import { describe, expect, it } from "vitest";
import { DEFAULT_DASHBOARD_LAYOUT, normalizeDashboardLayout } from "./dashboardRegistry";

describe("dashboard registry", () => {
  it("contains five equal visible cards in the reference first row", () => {
    expect(DEFAULT_DASHBOARD_LAYOUT.widgets).toHaveLength(11);
    expect(new Set(DEFAULT_DASHBOARD_LAYOUT.widgets.map(widget => widget.id)).size).toBe(11);
    for (const [index, id] of ["sip-rpm", "sip-today", "play-success-24h", "media-traffic-today", "media-runtime"].entries()) {
      expect(DEFAULT_DASHBOARD_LAYOUT.widgets.find(widget => widget.id === id)).toMatchObject({ x: index * 4, y: 0, w: 4, h: 2, visible: true });
    }
    expect(DEFAULT_DASHBOARD_LAYOUT.widgets.find(widget => widget.id === "sip-monitor")?.visible).toBe(false);
    expect(DEFAULT_DASHBOARD_LAYOUT.widgets.find(widget => widget.id === "active-stream-ranking")?.visible).toBe(false);
    expect(DEFAULT_DASHBOARD_LAYOUT.widgets.find(widget => widget.id === "media-node-health")?.visible).toBe(false);
    expect(DEFAULT_DASHBOARD_LAYOUT.widgets.find(widget => widget.id === "media-rate")).toMatchObject({ x: 0, y: 2, w: 12, h: 4, visible: true });
    expect(DEFAULT_DASHBOARD_LAYOUT.widgets.find(widget => widget.id === "device-online-rate")).toMatchObject({ x: 12, y: 2, w: 4, h: 4, visible: true });
    expect(DEFAULT_DASHBOARD_LAYOUT.widgets.find(widget => widget.id === "channel-online-rate")).toMatchObject({ x: 16, y: 2, w: 4, h: 4, visible: true });
  });

  it("migrates a known old widget and appends new defaults", () => {
    const normalized = normalizeDashboardLayout({
      schemaVersion: 0,
      widgets: [{ id: "sip-rpm", x: 3, y: 2, w: 2, h: 2, visible: true, settings: {} }]
    });
    expect(normalized.schemaVersion).toBe(4);
    expect(normalized.widgets).toHaveLength(11);
    expect(normalized.widgets.find(widget => widget.id === "sip-rpm")?.x).toBe(5);
  });

  it("migrates the legacy first row without preserving its hidden-card gap", () => {
    const normalized = normalizeDashboardLayout({
      schemaVersion: 1,
      widgets: [
        { id: "sip-rpm", x: 0, y: 0, w: 2, h: 2, visible: true, settings: {} },
        { id: "sip-today", x: 2, y: 0, w: 2, h: 2, visible: true, settings: {} },
        { id: "play-success-24h", x: 4, y: 0, w: 2, h: 2, visible: true, settings: {} },
        { id: "media-runtime", x: 8, y: 0, w: 4, h: 2, visible: true, settings: {} },
        { id: "device-online-rate", x: 8, y: 2, w: 2, h: 5, visible: true, settings: {} },
        { id: "channel-online-rate", x: 10, y: 2, w: 2, h: 5, visible: true, settings: {} }
      ]
    });
    for (const [index, id] of ["sip-rpm", "sip-today", "play-success-24h", "media-traffic-today", "media-runtime"].entries()) {
      expect(normalized.widgets.find(widget => widget.id === id)).toMatchObject({ x: index * 4, y: 0, w: 4, visible: true });
    }
    expect(normalized.widgets.find(widget => widget.id === "sip-monitor")?.visible).toBe(false);
    expect(normalized.widgets.find(widget => widget.id === "device-online-rate")).toMatchObject({ x: 12, y: 2, w: 4, h: 4 });
    expect(normalized.widgets.find(widget => widget.id === "channel-online-rate")).toMatchObject({ x: 16, y: 2, w: 4, h: 4 });
  });

  it("migrates the schema 2 default layout to the new first row", () => {
    const normalized = normalizeDashboardLayout({ schemaVersion: 2, widgets: [
      { id: "sip-rpm", x: 0, y: 0, w: 4, h: 2, visible: true, settings: {} },
      { id: "sip-today", x: 4, y: 0, w: 4, h: 2, visible: true, settings: {} },
      { id: "play-success-24h", x: 8, y: 0, w: 4, h: 2, visible: true, settings: {} },
      { id: "device-online-rate", x: 12, y: 0, w: 4, h: 2, visible: true, settings: {} },
      { id: "channel-online-rate", x: 16, y: 0, w: 4, h: 2, visible: true, settings: {} },
      { id: "media-traffic-today", x: 0, y: 15, w: 4, h: 2, visible: false, settings: {} },
      { id: "media-runtime", x: 14, y: 2, w: 6, h: 5, visible: true, settings: {} },
      { id: "sip-monitor", x: 0, y: 2, w: 14, h: 5, visible: true, settings: {} }
    ] });
    for (const [index, id] of ["sip-rpm", "sip-today", "play-success-24h", "media-traffic-today", "media-runtime"].entries()) {
      expect(normalized.widgets.find(widget => widget.id === id)).toMatchObject({ x: index * 4, y: 0, w: 4, h: 2, visible: true });
    }
    expect(normalized.widgets.find(widget => widget.id === "sip-monitor")?.visible).toBe(false);
    expect(normalized.widgets.find(widget => widget.id === "device-online-rate")).toMatchObject({ x: 12, y: 2, w: 4, h: 4 });
    expect(normalized.widgets.find(widget => widget.id === "channel-online-rate")).toMatchObject({ x: 16, y: 2, w: 4, h: 4 });
  });

  it("migrates schema 3 defaults to the compact optional layout", () => {
    const widgets = [
      { id: "sip-rpm", x: 0, y: 0, w: 4, h: 3, visible: true, settings: {} },
      { id: "sip-today", x: 4, y: 0, w: 4, h: 3, visible: true, settings: {} },
      { id: "play-success-24h", x: 8, y: 0, w: 4, h: 3, visible: true, settings: {} },
      { id: "media-traffic-today", x: 12, y: 0, w: 4, h: 3, visible: true, settings: {} },
      { id: "media-runtime", x: 16, y: 0, w: 4, h: 3, visible: true, settings: {} },
      { id: "media-rate", x: 0, y: 3, w: 12, h: 5, visible: true, settings: {} },
      { id: "device-online-rate", x: 12, y: 3, w: 4, h: 5, visible: true, settings: {} },
      { id: "channel-online-rate", x: 16, y: 3, w: 4, h: 5, visible: true, settings: {} },
      { id: "active-stream-ranking", x: 0, y: 8, w: 14, h: 4, visible: true, settings: {} },
      { id: "media-node-health", x: 14, y: 8, w: 6, h: 4, visible: true, settings: {} },
      { id: "sip-monitor", x: 0, y: 12, w: 14, h: 5, visible: false, settings: {} }
    ];
    expect(normalizeDashboardLayout({ schemaVersion: 3, widgets })).toEqual(DEFAULT_DASHBOARD_LAYOUT);
  });

  const invalidLayouts = [
    [{ id: "unknown", x: 0, y: 0, w: 2, h: 2, visible: true, settings: {} }],
    [
      { id: "sip-rpm", x: 0, y: 0, w: 2, h: 2, visible: true, settings: {} },
      { id: "sip-rpm", x: 2, y: 0, w: 2, h: 2, visible: true, settings: {} }
    ],
    [{ id: "sip-rpm", x: -1, y: 0, w: 2, h: 2, visible: true, settings: {} }],
    [{ id: "sip-rpm", x: 11, y: 0, w: 2, h: 2, visible: true, settings: {} }]
  ];

  it("rejects unknown, duplicate or invalid widgets", () => {
    for (const widgets of invalidLayouts) {
      expect(() => normalizeDashboardLayout({ schemaVersion: 4, widgets })).toThrow();
    }
  });
});

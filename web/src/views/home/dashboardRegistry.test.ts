import { describe, expect, it } from "vitest";
import { DEFAULT_DASHBOARD_LAYOUT, normalizeDashboardLayout } from "./dashboardRegistry";

describe("dashboard registry", () => {
  it("contains all widgets once and hides traffic by default", () => {
    expect(DEFAULT_DASHBOARD_LAYOUT.widgets).toHaveLength(11);
    expect(new Set(DEFAULT_DASHBOARD_LAYOUT.widgets.map(widget => widget.id)).size).toBe(11);
    expect(DEFAULT_DASHBOARD_LAYOUT.widgets.find(widget => widget.id === "media-traffic-today")?.visible).toBe(false);
  });

  it("migrates a known old widget and appends new defaults", () => {
    const normalized = normalizeDashboardLayout({
      schemaVersion: 0,
      widgets: [{ id: "sip-rpm", x: 3, y: 2, w: 2, h: 2, visible: true, settings: {} }]
    });
    expect(normalized.schemaVersion).toBe(1);
    expect(normalized.widgets).toHaveLength(11);
    expect(normalized.widgets.find(widget => widget.id === "sip-rpm")?.x).toBe(3);
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
      expect(() => normalizeDashboardLayout({ schemaVersion: 1, widgets })).toThrow();
    }
  });
});

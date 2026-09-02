export const DASHBOARD_SCHEMA_VERSION = 4;

export type DashboardWidgetId =
  | "sip-rpm"
  | "sip-today"
  | "play-success-24h"
  | "media-traffic-today"
  | "media-runtime"
  | "sip-monitor"
  | "device-online-rate"
  | "channel-online-rate"
  | "media-rate"
  | "media-node-health"
  | "active-stream-ranking";

export interface DashboardWidgetLayout {
  id: DashboardWidgetId;
  x: number;
  y: number;
  w: number;
  h: number;
  visible: boolean;
  settings: Record<string, never>;
}

export interface DashboardLayout {
  schemaVersion: number;
  widgets: DashboardWidgetLayout[];
}

interface DashboardWidgetDefinition {
  minW: number;
  maxW: number;
  minH: number;
  maxH: number;
  layout: DashboardWidgetLayout;
}

const widget = (
  id: DashboardWidgetId,
  x: number,
  y: number,
  w: number,
  h: number,
  visible: boolean,
  minW: number,
  maxW: number,
  minH: number,
  maxH: number
): DashboardWidgetDefinition => ({ minW, maxW, minH, maxH, layout: { id, x, y, w, h, visible, settings: {} } });

export const DASHBOARD_WIDGET_REGISTRY: readonly DashboardWidgetDefinition[] = [
  widget("sip-rpm", 0, 0, 4, 2, true, 3, 7, 2, 3),
  widget("sip-today", 4, 0, 4, 2, true, 3, 7, 2, 3),
  widget("play-success-24h", 8, 0, 4, 2, true, 3, 7, 2, 3),
  widget("media-traffic-today", 12, 0, 4, 2, true, 3, 7, 2, 3),
  widget("media-runtime", 16, 0, 4, 2, true, 3, 7, 2, 3),
  widget("media-rate", 0, 2, 12, 4, true, 10, 20, 4, 8),
  widget("device-online-rate", 12, 2, 4, 4, true, 3, 7, 3, 7),
  widget("channel-online-rate", 16, 2, 4, 4, true, 3, 7, 3, 7),
  widget("active-stream-ranking", 0, 6, 14, 4, false, 7, 20, 3, 7),
  widget("media-node-health", 14, 6, 6, 4, false, 5, 10, 3, 7),
  widget("sip-monitor", 0, 10, 14, 5, false, 10, 20, 4, 8)
] as const;

const LEGACY_WIDGET_DEFAULTS: Partial<Record<DashboardWidgetId, DashboardWidgetLayout>> = {
  "sip-rpm": { id: "sip-rpm", x: 0, y: 0, w: 2, h: 2, visible: true, settings: {} },
  "sip-today": { id: "sip-today", x: 2, y: 0, w: 2, h: 2, visible: true, settings: {} },
  "play-success-24h": { id: "play-success-24h", x: 4, y: 0, w: 2, h: 2, visible: true, settings: {} },
  "media-traffic-today": { id: "media-traffic-today", x: 6, y: 0, w: 2, h: 2, visible: false, settings: {} },
  "media-runtime": { id: "media-runtime", x: 8, y: 0, w: 4, h: 2, visible: true, settings: {} },
  "sip-monitor": { id: "sip-monitor", x: 0, y: 2, w: 8, h: 5, visible: true, settings: {} },
  "device-online-rate": { id: "device-online-rate", x: 8, y: 2, w: 2, h: 5, visible: true, settings: {} },
  "channel-online-rate": { id: "channel-online-rate", x: 10, y: 2, w: 2, h: 5, visible: true, settings: {} },
  "media-rate": { id: "media-rate", x: 0, y: 7, w: 8, h: 4, visible: true, settings: {} },
  "media-node-health": { id: "media-node-health", x: 8, y: 7, w: 4, h: 4, visible: true, settings: {} },
  "active-stream-ranking": { id: "active-stream-ranking", x: 0, y: 11, w: 12, h: 4, visible: true, settings: {} }
};

const SCHEMA_2_WIDGET_DEFAULTS: Partial<Record<DashboardWidgetId, DashboardWidgetLayout>> = {
  "sip-rpm": { id: "sip-rpm", x: 0, y: 0, w: 4, h: 2, visible: true, settings: {} },
  "sip-today": { id: "sip-today", x: 4, y: 0, w: 4, h: 2, visible: true, settings: {} },
  "play-success-24h": { id: "play-success-24h", x: 8, y: 0, w: 4, h: 2, visible: true, settings: {} },
  "device-online-rate": { id: "device-online-rate", x: 12, y: 0, w: 4, h: 2, visible: true, settings: {} },
  "channel-online-rate": { id: "channel-online-rate", x: 16, y: 0, w: 4, h: 2, visible: true, settings: {} },
  "media-traffic-today": { id: "media-traffic-today", x: 0, y: 15, w: 4, h: 2, visible: false, settings: {} },
  "media-runtime": { id: "media-runtime", x: 14, y: 2, w: 6, h: 5, visible: true, settings: {} },
  "sip-monitor": { id: "sip-monitor", x: 0, y: 2, w: 14, h: 5, visible: true, settings: {} },
  "media-rate": { id: "media-rate", x: 0, y: 7, w: 14, h: 4, visible: true, settings: {} },
  "media-node-health": { id: "media-node-health", x: 14, y: 7, w: 6, h: 4, visible: true, settings: {} },
  "active-stream-ranking": { id: "active-stream-ranking", x: 0, y: 11, w: 20, h: 4, visible: true, settings: {} }
};

const SCHEMA_3_WIDGET_DEFAULTS: Partial<Record<DashboardWidgetId, DashboardWidgetLayout>> = {
  "sip-rpm": { id: "sip-rpm", x: 0, y: 0, w: 4, h: 3, visible: true, settings: {} },
  "sip-today": { id: "sip-today", x: 4, y: 0, w: 4, h: 3, visible: true, settings: {} },
  "play-success-24h": { id: "play-success-24h", x: 8, y: 0, w: 4, h: 3, visible: true, settings: {} },
  "media-traffic-today": { id: "media-traffic-today", x: 12, y: 0, w: 4, h: 3, visible: true, settings: {} },
  "media-runtime": { id: "media-runtime", x: 16, y: 0, w: 4, h: 3, visible: true, settings: {} },
  "media-rate": { id: "media-rate", x: 0, y: 3, w: 12, h: 5, visible: true, settings: {} },
  "device-online-rate": { id: "device-online-rate", x: 12, y: 3, w: 4, h: 5, visible: true, settings: {} },
  "channel-online-rate": { id: "channel-online-rate", x: 16, y: 3, w: 4, h: 5, visible: true, settings: {} },
  "active-stream-ranking": { id: "active-stream-ranking", x: 0, y: 8, w: 14, h: 4, visible: true, settings: {} },
  "media-node-health": { id: "media-node-health", x: 14, y: 8, w: 6, h: 4, visible: true, settings: {} },
  "sip-monitor": { id: "sip-monitor", x: 0, y: 12, w: 14, h: 5, visible: false, settings: {} }
};

const cloneWidget = (layout: DashboardWidgetLayout): DashboardWidgetLayout => ({ ...layout, settings: {} });

export const DEFAULT_DASHBOARD_LAYOUT: DashboardLayout = {
  schemaVersion: DASHBOARD_SCHEMA_VERSION,
  widgets: DASHBOARD_WIDGET_REGISTRY.map(definition => cloneWidget(definition.layout))
};

function migrateLegacyWidget(item: DashboardWidgetLayout, definition: DashboardWidgetDefinition, schemaVersion: number): DashboardWidgetLayout {
  const defaults = schemaVersion === 3 ? SCHEMA_3_WIDGET_DEFAULTS : schemaVersion === 2 ? SCHEMA_2_WIDGET_DEFAULTS : LEGACY_WIDGET_DEFAULTS;
  const sourceColumns = schemaVersion >= 2 ? 20 : 12;
  const legacy = defaults[item.id];
  if (legacy && item.x === legacy.x && item.y === legacy.y && item.w === legacy.w && item.h === legacy.h && item.visible === legacy.visible) {
    return cloneWidget(definition.layout);
  }
  const width = Math.min(definition.maxW, Math.max(definition.minW, Math.round(item.w * 20 / sourceColumns)));
  const height = Math.min(definition.maxH, Math.max(definition.minH, item.h));
  const x = Math.min(20 - width, Math.round(item.x * 20 / sourceColumns));
  return { ...item, x, w: width, h: height, settings: {} };
}

export function normalizeDashboardLayout(input: { schemaVersion: number; widgets: unknown[] }): DashboardLayout {
  if (!Number.isInteger(input.schemaVersion) || input.schemaVersion < 0 || input.schemaVersion > DASHBOARD_SCHEMA_VERSION) {
    throw new Error("不支持的仪表盘布局版本");
  }
  const registry = new Map(DASHBOARD_WIDGET_REGISTRY.map(definition => [definition.layout.id, definition]));
  const seen = new Set<DashboardWidgetId>();
  const normalized: DashboardWidgetLayout[] = [];

  for (const raw of input.widgets) {
    if (!raw || typeof raw !== "object") throw new Error("仪表盘组件格式无效");
    const rawItem = raw as Partial<DashboardWidgetLayout>;
    const definition = registry.get(rawItem.id as DashboardWidgetId);
    if (!definition) throw new Error(`未知仪表盘组件: ${String(rawItem.id)}`);
    if (seen.has(definition.layout.id)) throw new Error(`仪表盘组件重复: ${definition.layout.id}`);
    const item = input.schemaVersion < DASHBOARD_SCHEMA_VERSION
      ? migrateLegacyWidget(rawItem as DashboardWidgetLayout, definition, input.schemaVersion)
      : rawItem;
    if (
      ![item.x, item.y, item.w, item.h].every(Number.isInteger) ||
      item.x! < 0 ||
      item.y! < 0 ||
      item.w! < definition.minW ||
      item.w! > definition.maxW ||
      item.h! < definition.minH ||
      item.h! > definition.maxH ||
      item.x! + item.w! > 20 ||
      typeof item.visible !== "boolean" ||
      !item.settings ||
      Object.keys(item.settings).length > 0
    ) {
      throw new Error(`仪表盘组件布局无效: ${definition.layout.id}`);
    }
    seen.add(definition.layout.id);
    normalized.push(cloneWidget(item as DashboardWidgetLayout));
  }

  for (const definition of DASHBOARD_WIDGET_REGISTRY) {
    if (!seen.has(definition.layout.id)) normalized.push(cloneWidget(definition.layout));
  }
  return { schemaVersion: DASHBOARD_SCHEMA_VERSION, widgets: normalized };
}

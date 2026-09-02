export const DASHBOARD_SCHEMA_VERSION = 1;

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
  widget("active-stream-ranking", 0, 11, 12, 4, true, 4, 12, 3, 7)
] as const;

const cloneWidget = (layout: DashboardWidgetLayout): DashboardWidgetLayout => ({ ...layout, settings: {} });

export const DEFAULT_DASHBOARD_LAYOUT: DashboardLayout = {
  schemaVersion: DASHBOARD_SCHEMA_VERSION,
  widgets: DASHBOARD_WIDGET_REGISTRY.map(definition => cloneWidget(definition.layout))
};

export function normalizeDashboardLayout(input: { schemaVersion: number; widgets: unknown[] }): DashboardLayout {
  if (!Number.isInteger(input.schemaVersion) || input.schemaVersion < 0 || input.schemaVersion > DASHBOARD_SCHEMA_VERSION) {
    throw new Error("不支持的仪表盘布局版本");
  }
  const registry = new Map(DASHBOARD_WIDGET_REGISTRY.map(definition => [definition.layout.id, definition]));
  const seen = new Set<DashboardWidgetId>();
  const normalized: DashboardWidgetLayout[] = [];

  for (const raw of input.widgets) {
    if (!raw || typeof raw !== "object") throw new Error("仪表盘组件格式无效");
    const item = raw as Partial<DashboardWidgetLayout>;
    const definition = registry.get(item.id as DashboardWidgetId);
    if (!definition) throw new Error(`未知仪表盘组件: ${String(item.id)}`);
    if (seen.has(definition.layout.id)) throw new Error(`仪表盘组件重复: ${definition.layout.id}`);
    if (
      ![item.x, item.y, item.w, item.h].every(Number.isInteger) ||
      item.x! < 0 ||
      item.y! < 0 ||
      item.w! < definition.minW ||
      item.w! > definition.maxW ||
      item.h! < definition.minH ||
      item.h! > definition.maxH ||
      item.x! + item.w! > 12 ||
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

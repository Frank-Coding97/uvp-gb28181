import type { DashboardWidgetId } from "./dashboardRegistry";
import type { DashboardDrilldownMetric } from "./dashboardDrilldownState";

const historyCards = new Set<DashboardWidgetId>(["sip-rpm", "sip-today", "play-success-24h", "media-traffic-today"]);

export function isHistoryDrilldownWidget(id: DashboardWidgetId): id is DashboardDrilldownMetric {
  return historyCards.has(id);
}

export function openHistoryDrilldown(
  id: DashboardWidgetId,
  editing: boolean,
  open: (metric: DashboardDrilldownMetric) => void
) {
  if (!editing && isHistoryDrilldownWidget(id)) open(id);
}

export function handleHistoryDrilldownKeydown(
  event: KeyboardEvent,
  id: DashboardWidgetId,
  editing: boolean,
  open: (metric: DashboardDrilldownMetric) => void
) {
  if (event.key !== "Enter" && event.key !== " ") return;
  if (editing || !isHistoryDrilldownWidget(id)) return;
  event.preventDefault();
  open(id);
}

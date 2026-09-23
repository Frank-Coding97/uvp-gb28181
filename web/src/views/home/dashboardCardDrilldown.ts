import type { DashboardWidgetId } from "./dashboardRegistry";
import type { DashboardDrilldownMetric } from "./dashboardDrilldownState";

const historyCards = new Set<DashboardWidgetId>(["media-traffic-today"]);

export function isHistoryDrilldownWidget(id: DashboardWidgetId): id is DashboardDrilldownMetric {
  return historyCards.has(id);
}

export function isDashboardDrilldownWidget(id: DashboardWidgetId) {
  return isHistoryDrilldownWidget(id) || id === "media-runtime";
}

export function openHistoryDrilldown(
  id: DashboardWidgetId,
  editing: boolean,
  open: (metric: DashboardDrilldownMetric) => void
) {
  if (!editing && isHistoryDrilldownWidget(id)) open(id);
}

export function openDashboardCardDrilldown(
  id: DashboardWidgetId,
  editing: boolean,
  openHistory: (metric: DashboardDrilldownMetric) => void,
  openMedia: () => void
) {
  if (editing) return;
  if (id === "media-runtime") openMedia();
  else openHistoryDrilldown(id, false, openHistory);
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

export function handleDashboardCardDrilldownKeydown(
  event: KeyboardEvent,
  id: DashboardWidgetId,
  editing: boolean,
  openHistory: (metric: DashboardDrilldownMetric) => void,
  openMedia: () => void
) {
  if (event.key !== "Enter" && event.key !== " ") return;
  if (editing || !isDashboardDrilldownWidget(id)) return;
  event.preventDefault();
  openDashboardCardDrilldown(id, false, openHistory, openMedia);
}

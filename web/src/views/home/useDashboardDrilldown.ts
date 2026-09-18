import { computed, onBeforeUnmount, ref } from "vue";
import { getDashboardDrilldown, type DashboardHistoryEnvelope, type DashboardHistoryData } from "@/api/home-dashboard-drilldown";
import {
  DASHBOARD_DRILLDOWN_RANGES,
  type DashboardDrilldownMetric,
  type DashboardHistoryRange
} from "./dashboardDrilldownState";

export type DashboardDrilldownLoader = (
  metric: DashboardDrilldownMetric,
  range: DashboardHistoryRange,
  signal: AbortSignal
) => Promise<DashboardHistoryEnvelope<DashboardHistoryData>>;

const defaultLoader: DashboardDrilldownLoader = async (metric, range, signal) => (await getDashboardDrilldown(metric, range, signal)).data;

export function useDashboardDrilldown(loader: DashboardDrilldownLoader = defaultLoader) {
  const visible = ref(false);
  const metric = ref<DashboardDrilldownMetric | null>(null);
  const range = ref<DashboardHistoryRange>("24h");
  const loading = ref(false);
  const stale = ref(false);
  const error = ref("");
  const result = ref<DashboardHistoryEnvelope<DashboardHistoryData> | null>(null);
  let controller: AbortController | null = null;
  let token = 0;

  const ranges = computed(() => (metric.value ? DASHBOARD_DRILLDOWN_RANGES[metric.value] : []));

  async function reload() {
    if (!metric.value || !visible.value) return;
    controller?.abort();
    controller = new AbortController();
    const currentToken = ++token;
    loading.value = true;
    error.value = "";
    const currentMetric = metric.value;
    const currentRange = range.value;
    try {
      const next = await loader(currentMetric, currentRange, controller.signal);
      if (currentToken !== token || controller.signal.aborted) return;
      result.value = next;
      stale.value = false;
    } catch (cause) {
      if (currentToken !== token || controller.signal.aborted) return;
      error.value = cause instanceof Error ? cause.message : "下钻数据加载失败";
      stale.value = result.value !== null;
    } finally {
      if (currentToken === token) loading.value = false;
    }
  }

  function open(nextMetric: DashboardDrilldownMetric) {
    metric.value = nextMetric;
    range.value = "24h";
    visible.value = true;
    return reload();
  }

  function setRange(nextRange: DashboardHistoryRange) {
    if (!metric.value || !DASHBOARD_DRILLDOWN_RANGES[metric.value].includes(nextRange)) return Promise.resolve();
    range.value = nextRange;
    return reload();
  }

  function close() {
    token += 1;
    controller?.abort();
    controller = null;
    loading.value = false;
    visible.value = false;
  }

  onBeforeUnmount(close);
  return { visible, metric, range, ranges, loading, stale, error, result, open, setRange, reload, close };
}

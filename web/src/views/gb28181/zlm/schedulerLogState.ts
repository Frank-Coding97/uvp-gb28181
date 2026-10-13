import type { SchedulerAlgorithm, SchedulerLogFilter } from "@/api/gb28181-zlm";

export interface SchedulerLogFilterState {
  timeRange?: Array<string | number | Date>;
  nodeId?: number;
  algorithm?: SchedulerAlgorithm;
  result?: "success" | "failure";
  streamId?: string;
  limit?: number;
}

export type SchedulerChartPeriod = "24h" | "7d";

export const SCHEDULER_CHART_PERIODS: readonly {
  key: SchedulerChartPeriod;
  label: string;
  durationMs: number;
}[] = [
  { key: "24h", label: "24 小时", durationMs: 24 * 60 * 60 * 1000 },
  { key: "7d", label: "7 天", durationMs: 7 * 24 * 60 * 60 * 1000 }
];

export function schedulerLogResultFromQuery(value: unknown): SchedulerLogFilterState["result"] {
  const normalized = Array.isArray(value) ? value[0] : value;
  return normalized === "success" || normalized === "failure" ? normalized : undefined;
}

function toRFC3339(value: string | number | Date) {
  const parsed = value instanceof Date ? new Date(value.getTime()) : new Date(value);
  if (Number.isNaN(parsed.getTime())) throw new Error("时间格式无效");
  return parsed.toISOString();
}

export function buildSchedulerLogFilter(state: SchedulerLogFilterState): SchedulerLogFilter {
  if (state.limit !== undefined && (!Number.isSafeInteger(state.limit) || state.limit <= 0 || state.limit > 1000)) {
    throw new Error("日志条数范围为 1-1000");
  }
  const filter: SchedulerLogFilter = state.limit === undefined ? {} : { limit: state.limit };
  if (state.timeRange?.length === 2 && state.timeRange[0] && state.timeRange[1]) {
    const from = toRFC3339(state.timeRange[0]);
    const to = toRFC3339(state.timeRange[1]);
    if (Date.parse(from) > Date.parse(to)) throw new Error("开始时间不能晚于结束时间");
    filter.from = from;
    filter.to = to;
  }
  if (state.nodeId && Number.isSafeInteger(state.nodeId) && state.nodeId > 0) filter.nodeId = state.nodeId;
  if (state.algorithm) filter.algorithm = state.algorithm;
  if (state.result) filter.result = state.result;
  const streamId = state.streamId?.trim();
  if (streamId) filter.streamId = streamId;
  return filter;
}

export function buildSchedulerChartWindowFilter(
  state: SchedulerLogFilterState,
  period: SchedulerChartPeriod,
  now: Date = new Date()
): SchedulerLogFilter {
  const config = SCHEDULER_CHART_PERIODS.find(item => item.key === period);
  if (!config) throw new Error("调度图表时间范围无效");
  const base = buildSchedulerLogFilter({ ...state, timeRange: [] });
  const to = new Date(now.getTime());
  let from: Date;
  if (period === "24h") {
    to.setMinutes(0, 0, 0);
    from = new Date(to.getTime());
    from.setHours(from.getHours() - 23);
  } else {
    to.setHours(0, 0, 0, 0);
    from = new Date(to.getTime());
    from.setDate(from.getDate() - 6);
  }
  return { ...base, from: from.toISOString(), to: new Date(now.getTime()).toISOString() };
}

import type { SchedulerAlgorithm, SchedulerLogFilter } from "@/api/gb28181-zlm";

export interface SchedulerLogFilterState {
  timeRange?: Array<string | number | Date>;
  nodeId?: number;
  algorithm?: SchedulerAlgorithm;
  result?: "success" | "failure";
  streamId?: string;
  limit: number;
}

function toRFC3339(value: string | number | Date) {
  const parsed = value instanceof Date ? new Date(value.getTime()) : new Date(value);
  if (Number.isNaN(parsed.getTime())) throw new Error("时间格式无效");
  return parsed.toISOString();
}

export function buildSchedulerLogFilter(state: SchedulerLogFilterState): SchedulerLogFilter {
  if (!Number.isSafeInteger(state.limit) || state.limit <= 0 || state.limit > 1000) {
    throw new Error("日志条数范围为 1-1000");
  }
  const filter: SchedulerLogFilter = { limit: state.limit };
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

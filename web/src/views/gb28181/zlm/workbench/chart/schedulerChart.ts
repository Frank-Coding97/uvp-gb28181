import type { SchedulerAlgorithm, SchedulerLogEntry, SchedulerLogFilter } from "@/api/gb28181-zlm";

import type { ChartDatum, MediaChartSpec } from "./overviewChart";

export type SchedulerChartStatus = "ready" | "empty" | "unknown" | "unavailable" | "partial";

export interface SchedulerChartDistribution {
  category: string;
  count: number;
  nodeId?: number;
}

export interface SchedulerChartState {
  status: SchedulerChartStatus;
  sampleCount: number;
  limit: number | null;
  filter: SchedulerLogFilter;
  filterText: string;
  resultDistribution: SchedulerChartDistribution[];
  nodeDistribution: SchedulerChartDistribution[];
  summary: string;
  warning: string | null;
  asOf: string | null;
}

export interface SchedulerChartStateOptions {
  unavailable?: boolean;
}

export const SCHEDULER_CHART_SAMPLE_LIMIT = 1_000;

const algorithmLabels: Record<SchedulerAlgorithm, string> = {
  roundrobin: "轮询",
  weighted: "加权轮询",
  leastload: "最小负载"
};

function normalizedLimit(filter: SchedulerLogFilter): number | null {
  const value = filter.limit;
  return typeof value === "number" && Number.isSafeInteger(value) && value > 0 ? Math.min(value, 1000) : null;
}

function formatFilter(filter: SchedulerLogFilter, sampleCount: number, limit: number | null): string {
  const parts: string[] = [];
  if (filter.from || filter.to) parts.push(`时间 ${filter.from || "起始"} 至 ${filter.to || "当前"}`);
  if (filter.nodeId) parts.push(`节点 #${filter.nodeId}`);
  const algorithm = filter.algorithm || filter.policy;
  if (algorithm) parts.push(`策略 ${algorithmLabels[algorithm] || algorithm}`);
  if (filter.result) parts.push(`结果 ${filter.result === "success" ? "成功" : "失败"}`);
  if (filter.streamId) parts.push(`流 ${filter.streamId}`);
  parts.push(`当前 ${sampleCount} 条样本`);
  if (limit !== null) parts.push(`上限 ${limit}`);
  return parts.join(" · ");
}

function increment(map: Map<string, SchedulerChartDistribution>, category: string, nodeId?: number) {
  const key = nodeId === undefined ? category : `${category}:${nodeId}`;
  const current = map.get(key);
  if (current) current.count += 1;
  else map.set(key, { category, count: 1, ...(nodeId === undefined ? {} : { nodeId }) });
}

function sorted(values: Map<string, SchedulerChartDistribution>): SchedulerChartDistribution[] {
  return [...values.values()].sort((left, right) => {
    const resultOrder = (category: string) => category === "成功" ? 0 : category === "失败" ? 1 : 2;
    return right.count - left.count || resultOrder(left.category) - resultOrder(right.category) || left.category.localeCompare(right.category);
  });
}

function resultCategory(log: SchedulerLogEntry): string {
  return typeof log.errorMessage === "string" && log.errorMessage.trim() === "" ? "成功" : "失败";
}

function nodeCategory(log: SchedulerLogEntry): { category: string; nodeId?: number } {
  const nodeId = Number.isSafeInteger(log.nodeID) && log.nodeID > 0 ? log.nodeID : undefined;
  const name = typeof log.nodeName === "string" ? log.nodeName.trim() : "";
  return { category: name || (nodeId === undefined ? "未知节点" : `节点 #${nodeId}`), nodeId };
}

export function buildSchedulerChartState(
  logs: readonly SchedulerLogEntry[] | null | undefined,
  filter: SchedulerLogFilter = {},
  options: SchedulerChartStateOptions = {}
): SchedulerChartState {
  const limit = normalizedLimit(filter);
  const effectiveLimit = limit ?? SCHEDULER_CHART_SAMPLE_LIMIT;
  const sourceLogs = Array.isArray(logs) ? logs : [];
  const sampledLogs = sourceLogs.slice(0, effectiveLimit);
  const sampleCount = sampledLogs.length;
  const filterText = formatFilter(filter, sampleCount, limit);
  if (options.unavailable) {
    return {
      status: "unavailable",
      sampleCount,
      limit,
      filter,
      filterText,
      resultDistribution: [],
      nodeDistribution: [],
      summary: "调度日志暂不可用",
      warning: "后端没有返回当前筛选样本，不能将其视为 0 条成功或失败",
      asOf: null
    };
  }
  if (!Array.isArray(logs)) {
    return {
      status: "unknown",
      sampleCount: 0,
      limit,
      filter,
      filterText,
      resultDistribution: [],
      nodeDistribution: [],
      summary: "暂时没有可用的调度日志样本",
      warning: "调度日志尚未返回",
      asOf: null
    };
  }
  if (sourceLogs.length === 0) {
    return {
      status: "empty",
      sampleCount: 0,
      limit,
      filter,
      filterText,
      resultDistribution: [],
      nodeDistribution: [],
      summary: "当前筛选没有调度日志",
      warning: null,
      asOf: null
    };
  }

  const results = new Map<string, SchedulerChartDistribution>();
  const nodes = new Map<string, SchedulerChartDistribution>();
  let latest: string | null = null;
  for (const log of sampledLogs) {
    increment(results, resultCategory(log));
    const node = nodeCategory(log);
    increment(nodes, node.category, node.nodeId);
    if (typeof log.happenedAt === "string" && log.happenedAt && (!latest || log.happenedAt > latest)) latest = log.happenedAt;
  }
  const resultDistribution = sorted(results);
  const nodeDistribution = sorted(nodes);
  const sampledAtLimit = sourceLogs.length > sampledLogs.length || (limit !== null && sampledLogs.length >= limit);
  return {
    status: sampledAtLimit ? "partial" : "ready",
    sampleCount,
    limit,
    filter,
    filterText,
    resultDistribution,
    nodeDistribution,
    summary: `当前筛选统计 ${sampleCount} 条样本：${resultDistribution.map(item => `${item.category} ${item.count}`).join("、")}`,
    warning: sampledAtLimit
      ? `已达到${limit === null ? "前端统计" : "后端返回"}上限 ${effectiveLimit} 条，只代表当前筛选样本，不代表全量历史`
      : null,
    asOf: latest
  };
}

function distributionSpec(id: string, values: SchedulerChartDistribution[], yTitle: string): MediaChartSpec {
  const data: ChartDatum[] = values.map(item => ({ category: item.category, count: item.count, nodeId: item.nodeId }));
  return {
    type: "bar",
    background: "transparent",
    data: [{ id, values: data }],
    series: [{ type: "bar", data: { id }, xField: "category", yField: "count", barMaxWidth: 28 }],
    axes: [{ orient: "left", title: { text: yTitle } }, { orient: "bottom", label: { autoRotate: false, autoHide: true } }],
    tooltip: { activeType: "dimension" },
    padding: { left: 8, right: 12, top: 8, bottom: 8 }
  };
}

export function createSchedulerResultSpec(state: SchedulerChartState): MediaChartSpec {
  return distributionSpec("scheduler-result-distribution", state.resultDistribution, "调度次数");
}

export function createSchedulerNodeSpec(state: SchedulerChartState): MediaChartSpec {
  return distributionSpec("scheduler-node-distribution", state.nodeDistribution, "承接次数");
}

export const schedulerChartState = buildSchedulerChartState;
export const schedulerResultSpec = createSchedulerResultSpec;
export const schedulerNodeSpec = createSchedulerNodeSpec;

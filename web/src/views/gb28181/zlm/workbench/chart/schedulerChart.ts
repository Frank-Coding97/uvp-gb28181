import type { SchedulerAlgorithm, SchedulerLogEntry, SchedulerLogFilter } from "@/api/gb28181-zlm";

import type { SchedulerChartPeriod } from "../../schedulerLogState";
import type { ChartDatum, MediaChartSpec } from "./overviewChart";

export type SchedulerChartStatus = "ready" | "empty" | "unknown" | "unavailable" | "partial";

export interface SchedulerChartDistribution {
  bucket: string;
  category: string;
  count: number;
  nodeId?: number;
}

export interface SchedulerChartState {
  status: SchedulerChartStatus;
  period: SchedulerChartPeriod;
  timeBuckets: string[];
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
  period?: SchedulerChartPeriod;
}

const SCHEDULER_NODE_CATEGORY_LIMIT = 40;

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
  if (filter.result) parts.push(`调度状态 ${filter.result === "success" ? "已命中" : "未命中"}`);
  if (filter.streamId) parts.push(`流 ${filter.streamId}`);
  parts.push(`当前 ${sampleCount} 条样本`);
  if (limit !== null) parts.push(`上限 ${limit}`);
  return parts.join(" · ");
}

function resultCategory(log: SchedulerLogEntry): string {
  return typeof log.errorMessage === "string" && log.errorMessage.trim() === "" ? "已命中" : "未命中";
}

function nodeCategory(log: SchedulerLogEntry): { category: string; nodeId: number } | null {
  const nodeId = Number.isSafeInteger(log.nodeID) && log.nodeID > 0 ? log.nodeID : undefined;
  const name = typeof log.nodeName === "string" ? log.nodeName.trim() : "";
  if (nodeId === undefined) return null;
  return { category: name || `节点 #${nodeId}`, nodeId };
}

function validDate(value: string | undefined): Date | null {
  if (!value) return null;
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? null : date;
}

function floorHour(value: Date): Date {
  const result = new Date(value.getTime());
  result.setMinutes(0, 0, 0);
  return result;
}

function floorDay(value: Date): Date {
  const result = new Date(value.getTime());
  result.setHours(0, 0, 0, 0);
  return result;
}

function shift(value: Date, period: SchedulerChartPeriod, amount: number): Date {
  const result = new Date(value.getTime());
  if (period === "24h") result.setHours(result.getHours() + amount);
  else result.setDate(result.getDate() + amount);
  return result;
}

function bucketFor(value: Date, period: SchedulerChartPeriod): string {
  return (period === "24h" ? floorHour(value) : floorDay(value)).toISOString();
}

function createTimeBuckets(
  filter: SchedulerLogFilter,
  logs: readonly SchedulerLogEntry[],
  period: SchedulerChartPeriod
): string[] {
  const filterTo = validDate(filter.to);
  const latest = logs.reduce<Date | null>((current, log) => {
    const date = validDate(log.happenedAt);
    return date && (!current || date > current) ? date : current;
  }, null);
  const anchor = period === "24h" ? floorHour(filterTo || latest || new Date()) : floorDay(filterTo || latest || new Date());
  const first = shift(anchor, period, period === "24h" ? -23 : -6);
  const buckets: string[] = [];
  for (let current = first; current <= anchor; current = shift(current, period, 1)) buckets.push(current.toISOString());
  return buckets;
}

function increment(map: Map<string, SchedulerChartDistribution>, bucket: string, category: string, nodeId?: number) {
  const key = `${bucket}:${category}:${nodeId ?? ""}`;
  const current = map.get(key);
  if (current) current.count += 1;
  else map.set(key, { bucket, category, count: 1, ...(nodeId === undefined ? {} : { nodeId }) });
}

function fillDistribution(
  buckets: readonly string[],
  categories: readonly { category: string; nodeId?: number }[],
  values: Map<string, SchedulerChartDistribution>
): SchedulerChartDistribution[] {
  return buckets.flatMap(bucket =>
    categories.map(
      ({ category, nodeId }) =>
        values.get(`${bucket}:${category}:${nodeId ?? ""}`) || {
          bucket,
          category,
          count: 0,
          ...(nodeId === undefined ? {} : { nodeId })
        }
    )
  );
}

function fillNodeDistribution(
  buckets: readonly string[],
  categories: readonly { category: string; nodeId?: number }[],
  values: Map<string, SchedulerChartDistribution>
): SchedulerChartDistribution[] {
  const selected = categories.slice(0, SCHEDULER_NODE_CATEGORY_LIMIT - 1);
  const selectedKeys = new Set(selected.map(item => `${item.category}:${item.nodeId ?? ""}`));
  const result = fillDistribution(buckets, selected, values);
  const hasOther = categories.length > selected.length;
  if (!hasOther) return result;
  return [
    ...result,
    ...buckets.map(bucket => ({
      bucket,
      category: "其他",
      count: [...values.values()].reduce(
        (sum, item) =>
          item.bucket === bucket && !selectedKeys.has(`${item.category}:${item.nodeId ?? ""}`) ? sum + item.count : sum,
        0
      )
    }))
  ];
}

export function buildSchedulerChartState(
  logs: readonly SchedulerLogEntry[] | null | undefined,
  filter: SchedulerLogFilter = {},
  options: SchedulerChartStateOptions = {}
): SchedulerChartState {
  const period = options.period || "24h";
  const limit = normalizedLimit(filter);
  const sourceLogs = Array.isArray(logs) ? logs : [];
  const sampledLogs = limit === null ? sourceLogs : sourceLogs.slice(0, limit);
  const sampleCount = sampledLogs.length;
  const filterText = formatFilter(filter, sampleCount, limit);
  const empty = (status: SchedulerChartStatus, summary: string, warning: string | null): SchedulerChartState => ({
    status,
    period,
    timeBuckets: [],
    sampleCount,
    limit,
    filter,
    filterText,
    resultDistribution: [],
    nodeDistribution: [],
    summary,
    warning,
    asOf: null
  });
  if (options.unavailable) return empty("unavailable", "调度日志暂不可用", "后端没有返回当前筛选样本");
  if (!Array.isArray(logs)) return empty("unknown", "暂时没有可用的调度日志样本", "调度日志尚未返回");
  if (sourceLogs.length === 0) return empty("empty", "当前筛选没有调度日志", null);

  const timeBuckets = createTimeBuckets(filter, sampledLogs, period);
  const resultValues = new Map<string, SchedulerChartDistribution>();
  const nodeValues = new Map<string, SchedulerChartDistribution>();
  const nodeCategories = new Map<string, { category: string; nodeId: number }>();
  let latest: string | null = null;
  for (const log of sampledLogs) {
    const happenedAt = validDate(log.happenedAt);
    if (!happenedAt) continue;
    const bucket = bucketFor(happenedAt, period);
    if (!timeBuckets.includes(bucket)) continue;
    increment(resultValues, bucket, resultCategory(log));
    const node = nodeCategory(log);
    if (node) {
      nodeCategories.set(`${node.category}:${node.nodeId}`, node);
      increment(nodeValues, bucket, node.category, node.nodeId);
    }
    if (!latest || log.happenedAt > latest) latest = log.happenedAt;
  }
  const resultCategories = [{ category: "已命中" }, { category: "未命中" }];
  const resultDistribution = fillDistribution(timeBuckets, resultCategories, resultValues);
  const nodeTotalsByKey = new Map<string, number>();
  for (const item of nodeValues.values()) {
    const key = `${item.category}:${item.nodeId ?? ""}`;
    nodeTotalsByKey.set(key, (nodeTotalsByKey.get(key) || 0) + item.count);
  }
  const nodeTotals = [...nodeCategories.values()]
    .map(node => ({ ...node, total: nodeTotalsByKey.get(`${node.category}:${node.nodeId}`) || 0 }))
    .sort((left, right) => right.total - left.total || left.category.localeCompare(right.category));
  const nodeDistribution = fillNodeDistribution(timeBuckets, nodeTotals, nodeValues);
  const sampledAtLimit = limit !== null && (sourceLogs.length > sampledLogs.length || sampledLogs.length >= limit);
  const resultTotals = new Map<string, number>();
  for (const item of resultDistribution) resultTotals.set(item.category, (resultTotals.get(item.category) || 0) + item.count);
  return {
    status: sampledAtLimit ? "partial" : "ready",
    period,
    timeBuckets,
    sampleCount,
    limit,
    filter,
    filterText,
    resultDistribution,
    nodeDistribution,
    summary: `当前筛选统计 ${sampleCount} 条样本：${["已命中", "未命中"]
      .filter(category => (resultTotals.get(category) || 0) > 0)
      .map(category => `${category} ${resultTotals.get(category)}`)
      .join("、")}`,
    warning: sampledAtLimit ? `已达到后端返回上限 ${limit} 条，只代表当前筛选样本，不代表全量历史` : null,
    asOf: latest
  };
}

function formatHour(value: string): string {
  const date = new Date(value);
  return `${String(date.getHours()).padStart(2, "0")}:00`;
}

function formatDay(value: string): string {
  const date = new Date(value);
  return `${date.getMonth() + 1}/${date.getDate()}`;
}

function distributionSpec(
  id: string,
  values: SchedulerChartDistribution[],
  yTitle: string,
  period: SchedulerChartPeriod
): MediaChartSpec {
  const data: ChartDatum[] = values.map(item => ({
    bucket: item.bucket,
    category: item.category,
    count: item.count,
    nodeId: item.nodeId
  }));
  return {
    type: "bar",
    background: "transparent",
    data: [{ id, values: data }],
    series: [
      { type: "bar", data: { id }, xField: "bucket", yField: "count", seriesField: "category", stack: true, barMaxWidth: 28 }
    ],
    axes: [
      { orient: "left", title: { text: yTitle }, label: { autoHide: true } },
      { orient: "bottom", label: { autoRotate: false, autoHide: true, formatMethod: period === "24h" ? formatHour : formatDay } }
    ],
    tooltip: {
      activeType: "dimension",
      dimension: {
        title: { visible: false }
      }
    },
    padding: { left: 8, right: 12, top: 8, bottom: 8 }
  };
}

export function createSchedulerResultSpec(state: SchedulerChartState): MediaChartSpec {
  return distributionSpec("scheduler-result-distribution", state.resultDistribution, "调度次数", state.period);
}

export function createSchedulerNodeSpec(state: SchedulerChartState): MediaChartSpec {
  return distributionSpec("scheduler-node-distribution", state.nodeDistribution, "承接次数", state.period);
}

export const schedulerChartState = buildSchedulerChartState;
export const schedulerResultSpec = createSchedulerResultSpec;
export const schedulerNodeSpec = createSchedulerNodeSpec;

import type { ZLMNodeRuntime, ZLMOverview, ZLMRuntimeMedia } from "@/api/gb28181-zlm-runtime";

export type OverviewChartStatus = "ready" | "empty" | "unknown" | "unavailable" | "partial";

export interface ChartDatum {
  [key: string]: string | number | boolean | null | undefined;
}

export interface MediaChartSpec {
  type: string;
  background?: string;
  data?: Array<{ id?: string; values: ChartDatum[] }>;
  series?: Array<Record<string, unknown>>;
  axes?: Array<Record<string, unknown>>;
  legends?: Record<string, unknown>;
  tooltip?: Record<string, unknown>;
  padding?: number | Record<string, number>;
  [key: string]: unknown;
}

export interface OverviewNodeLoadDatum {
  nodeId: number;
  nodeName: string;
  metric: "NetThread" | "WorkThread";
  value: number | null;
  netThreadLoad: number | null;
  workThreadLoad: number | null;
  statusText: string;
  sampled: boolean;
}

export interface OverviewHealthDatum {
  nodeId: number;
  nodeName: string;
  dimension: "状态" | "运行态新鲜度" | "媒体采样" | "指标采样";
  value: number | null;
  label: string;
  statusText: string;
  sampled: boolean;
}

export interface OverviewDistributionDatum {
  dimension: "协议" | "来源" | "节点";
  category: string;
  count: number;
  nodeId?: number;
}

export interface OverviewChartState {
  status: OverviewChartStatus;
  sampled: {
    nodeIds: number[];
    count: number;
    streamNodeIds: number[];
  };
  sampledNodeIds: number[];
  failed: { nodeIds: number[]; count: number };
  failedNodeIds: number[];
  asOf: string | null;
  nodeLoad: OverviewNodeLoadDatum[];
  health: OverviewHealthDatum[];
  distribution: OverviewDistributionDatum[];
  protocolDistribution: OverviewDistributionDatum[];
  sourceDistribution: OverviewDistributionDatum[];
  nodeDistribution: OverviewDistributionDatum[];
  summary: string;
  warning: string | null;
}

export const OVERVIEW_DISTRIBUTION_CATEGORY_LIMIT = 12;

function uniquePositiveIds(values: readonly number[] | undefined): number[] {
  if (!values) return [];
  return [...new Set(values.filter(value => Number.isSafeInteger(value) && value > 0))];
}

function finiteNumber(value: unknown): number | null {
  return typeof value === "number" && Number.isFinite(value) ? value : null;
}

function nodeStatusText(node: ZLMNodeRuntime): string {
  if (node.state === "maintenance") return "维护中";
  if (node.status === "unavailable") return "采集失败";
  if (node.state === "offline") return "离线";
  if (node.status === "partial") return "部分数据";
  if (node.status === "fresh") return "在线";
  return "状态未知";
}

function isMetricsSampled(node: ZLMNodeRuntime, sampledNodeIds: ReadonlySet<number>): boolean {
  return sampledNodeIds.has(node.nodeId) && node.metricsComplete === true && !!node.metrics;
}

function isMediaSampled(node: ZLMNodeRuntime): boolean {
  return node.mediaFreshness !== "unavailable" && node.streams !== undefined;
}

function healthValue(node: ZLMNodeRuntime, dimension: OverviewHealthDatum["dimension"]): number | null {
  const status = nodeStatusText(node);
  if (status === "状态未知" || status === "采集失败") return null;
  switch (dimension) {
    case "状态":
      return node.state === "offline" ? 0 : node.state === "maintenance" ? 0.5 : node.status === "partial" ? 0.5 : 1;
    case "运行态新鲜度":
      return node.freshness === "fresh" ? 1 : node.freshness === "stale" ? 0.5 : null;
    case "媒体采样":
      return isMediaSampled(node) ? 1 : null;
    case "指标采样":
      return node.metricsComplete ? 1 : null;
  }
}

function healthLabel(node: ZLMNodeRuntime, dimension: OverviewHealthDatum["dimension"]): string {
  const status = nodeStatusText(node);
  if (status === "采集失败") return status;
  switch (dimension) {
    case "状态":
      return status;
    case "运行态新鲜度":
      return node.freshness === "fresh" ? "新鲜" : node.freshness === "stale" ? "已过期" : "不可用";
    case "媒体采样":
      return isMediaSampled(node) ? "已采样" : "不可用";
    case "指标采样":
      return node.metricsComplete ? "已采样" : "不完整";
  }
}

function originLabel(stream: ZLMRuntimeMedia): string {
  const explicit = stream.originTypeName?.trim();
  if (explicit) return explicit;
  const originType = finiteNumber(stream.originType);
  return originType === null ? "未知来源" : `来源类型 ${originType}`;
}

function distributionFor(
  streams: readonly ZLMRuntimeMedia[],
  dimension: OverviewDistributionDatum["dimension"],
  categoryOf: (stream: ZLMRuntimeMedia) => string,
  nodeIdOf?: (stream: ZLMRuntimeMedia) => number | undefined
): OverviewDistributionDatum[] {
  const counts = new Map<string, OverviewDistributionDatum>();
  for (const stream of streams) {
    const category = categoryOf(stream);
    const nodeId = nodeIdOf?.(stream);
    const key = nodeId === undefined ? category : `${category}:${nodeId}`;
    const current = counts.get(key);
    if (current) {
      current.count += 1;
      continue;
    }
    counts.set(key, { dimension, category, count: 1, ...(nodeId === undefined ? {} : { nodeId }) });
  }
  const sorted = [...counts.values()].sort((left, right) => right.count - left.count || left.category.localeCompare(right.category));
  if (sorted.length <= OVERVIEW_DISTRIBUTION_CATEGORY_LIMIT) return sorted;
  const visible = sorted.slice(0, OVERVIEW_DISTRIBUTION_CATEGORY_LIMIT);
  const otherCount = sorted.slice(OVERVIEW_DISTRIBUTION_CATEGORY_LIMIT).reduce((sum, item) => sum + item.count, 0);
  return [...visible, { dimension, category: "其他", count: otherCount }];
}

function mediaCategory(stream: ZLMRuntimeMedia): string {
  const schema = stream.media?.schema?.trim();
  return schema || "未知协议";
}

function streamNodeCategory(stream: ZLMRuntimeMedia): string {
  return `节点 ${stream.nodeId}`;
}

function sampledAndFailed(overview: ZLMOverview) {
  const nodeIds = overview.nodes.map(node => node.nodeId);
  const derivedSampled = overview.nodes
    .filter(node => node.status === "fresh" || node.status === "partial")
    .map(node => node.nodeId);
  const sampledNodeIds = uniquePositiveIds(overview.metricsSampledNodeIds?.length ? overview.metricsSampledNodeIds : derivedSampled);
  const failedNodeIds = uniquePositiveIds(
    overview.failedNodeIds?.length
      ? overview.failedNodeIds
      : overview.nodes.filter(node => node.status === "unavailable").map(node => node.nodeId)
  );
  return { nodeIds, sampledNodeIds, failedNodeIds };
}

function createNodeLoad(overview: ZLMOverview, sampledNodeIds: ReadonlySet<number>): OverviewNodeLoadDatum[] {
  const result = overview.nodes.flatMap(node => {
    const sampled = isMetricsSampled(node, sampledNodeIds);
    const netThreadLoad = sampled ? finiteNumber(node.metrics?.netThreadLoad) : null;
    const workThreadLoad = sampled ? finiteNumber(node.metrics?.workThreadLoad) : null;
    const statusText = nodeStatusText(node);
    return [
      { nodeId: node.nodeId, nodeName: node.name, metric: "NetThread" as const, value: netThreadLoad, netThreadLoad, workThreadLoad, statusText, sampled },
      { nodeId: node.nodeId, nodeName: node.name, metric: "WorkThread" as const, value: workThreadLoad, netThreadLoad, workThreadLoad, statusText, sampled }
    ];
  });
  return result.sort((left, right) => {
    const leftPeak = Math.max(left.netThreadLoad ?? -1, left.workThreadLoad ?? -1);
    const rightPeak = Math.max(right.netThreadLoad ?? -1, right.workThreadLoad ?? -1);
    return rightPeak - leftPeak || left.nodeName.localeCompare(right.nodeName);
  });
}

function createHealth(overview: ZLMOverview, sampledNodeIds: ReadonlySet<number>): OverviewHealthDatum[] {
  const dimensions: OverviewHealthDatum["dimension"][] = ["状态", "运行态新鲜度", "媒体采样", "指标采样"];
  return overview.nodes.flatMap(node => dimensions.map(dimension => ({
    nodeId: node.nodeId,
    nodeName: node.name,
    dimension,
    value: healthValue(node, dimension),
    label: healthLabel(node, dimension),
    statusText: nodeStatusText(node),
    sampled: dimension === "指标采样" ? isMetricsSampled(node, sampledNodeIds) : dimension === "媒体采样" ? isMediaSampled(node) : true
  })));
}

function emptyState(status: OverviewChartStatus, summary: string, warning: string | null = null): OverviewChartState {
  return {
    status,
    sampled: { nodeIds: [], count: 0, streamNodeIds: [] },
    sampledNodeIds: [],
    failed: { nodeIds: [], count: 0 },
    failedNodeIds: [],
    asOf: null,
    nodeLoad: [],
    health: [],
    distribution: [],
    protocolDistribution: [],
    sourceDistribution: [],
    nodeDistribution: [],
    summary,
    warning
  };
}

export function buildOverviewChartState(overview: ZLMOverview | null | undefined): OverviewChartState {
  if (!overview) return emptyState("unknown", "暂时没有可用的集群采样", "集群运行态尚未返回");

  const { nodeIds, sampledNodeIds, failedNodeIds } = sampledAndFailed(overview);
  if (nodeIds.length === 0 && failedNodeIds.length === 0) return emptyState("empty", "尚未配置媒体节点");
  if (nodeIds.length === 0 && failedNodeIds.length > 0) {
    const unavailable = emptyState("unavailable", "集群运行态不可用，无法判断当前节点数量", `${failedNodeIds.length} 个节点采集失败`);
    unavailable.failed = { nodeIds: failedNodeIds, count: failedNodeIds.length };
    unavailable.failedNodeIds = failedNodeIds;
    unavailable.asOf = overview.asOf || null;
    return unavailable;
  }

  const sampledSet = new Set(sampledNodeIds);
  const streamsKnown = Array.isArray((overview as unknown as { streams?: unknown }).streams);
  const streams = streamsKnown ? overview.streams : [];
  const protocolDistribution = distributionFor(streams, "协议", mediaCategory);
  const sourceDistribution = distributionFor(streams, "来源", originLabel);
  const nodeDistribution = distributionFor(streams, "节点", streamNodeCategory, stream => stream.nodeId);
  const noSuccessfulSample = sampledNodeIds.length === 0 && failedNodeIds.length === 0
    && !overview.nodes.some(node => node.status === "fresh" || node.status === "partial");
  const status: OverviewChartStatus = failedNodeIds.length === nodeIds.length && nodeIds.length > 0
    ? "unavailable"
    : noSuccessfulSample
      ? "unknown"
      : overview.partial || failedNodeIds.length > 0 || !streamsKnown
        ? "partial"
        : "ready";
  const summary = status === "unavailable"
    ? `集群不可用，采样 0 个节点，失败 ${failedNodeIds.length} 个`
    : status === "unknown"
      ? `集群暂无有效运行态采样，已配置 ${nodeIds.length} 个节点`
    : status === "partial"
      ? `集群部分可用，采样 ${sampledNodeIds.length} 个节点，失败 ${failedNodeIds.length} 个`
      : `集群正常，采样 ${sampledNodeIds.length} 个节点`;
  return {
    status,
    sampled: {
      nodeIds: sampledNodeIds,
      count: sampledNodeIds.length,
      streamNodeIds: uniquePositiveIds(overview.mediaSampledNodeIds?.length ? overview.mediaSampledNodeIds : streams.map(stream => stream.nodeId))
    },
    sampledNodeIds,
    failed: { nodeIds: failedNodeIds, count: failedNodeIds.length },
    failedNodeIds,
    asOf: overview.asOf || null,
    nodeLoad: createNodeLoad(overview, sampledSet),
    health: createHealth(overview, sampledSet),
    distribution: [...protocolDistribution, ...sourceDistribution, ...nodeDistribution],
    protocolDistribution,
    sourceDistribution,
    nodeDistribution,
    summary,
    warning: status === "partial"
      ? `${failedNodeIds.length} 个节点未完成采样，图表仅统计已返回数据`
      : status === "unavailable"
        ? "当前没有可用节点运行态"
        : status === "unknown"
          ? "节点处于维护/离线或尚未完成采样，不能判断为 0"
          : null
  };
}

function commonSpec(dataId: string, values: ChartDatum[]): MediaChartSpec {
  return {
    type: "bar",
    background: "transparent",
    data: [{ id: dataId, values }],
    padding: { left: 8, right: 12, top: 8, bottom: 8 },
    tooltip: { activeType: "dimension" }
  };
}

export function createOverviewNodeLoadSpec(state: OverviewChartState): MediaChartSpec {
  const spec = commonSpec("overview-node-load", state.nodeLoad.map(item => ({
    nodeId: item.nodeId,
    node: item.nodeName,
    metric: item.metric,
    value: item.value,
    status: item.statusText
  })));
  spec.series = [{
    type: "bar",
    direction: "horizontal",
    data: { id: "overview-node-load" },
    xField: "value",
    yField: "node",
    seriesField: "metric",
    barMaxWidth: 18
  }];
  spec.axes = [
    { orient: "bottom", title: { text: "负载" }, label: { formatMethod: (value: number) => `${Math.round(value * 100)}%` } },
    { orient: "left", label: { autoHide: true } }
  ];
  return spec;
}

export function createOverviewHealthSpec(state: OverviewChartState): MediaChartSpec {
  return {
    type: "heatmap",
    background: "transparent",
    data: [{ id: "overview-health", values: state.health.map(item => ({
      nodeId: item.nodeId,
      node: item.nodeName,
      dimension: item.dimension,
      value: item.value,
      label: item.label,
      status: item.statusText
    })) }],
    series: [{
      type: "heatmap",
      data: { id: "overview-health" },
      xField: "dimension",
      yField: "node",
      valueField: "value",
      label: { visible: true, style: { text: (datum: ChartDatum) => String(datum.label ?? "—") } }
    }],
    axes: [{ orient: "bottom", label: { autoHide: false } }, { orient: "left", label: { autoHide: true } }],
    padding: { left: 8, right: 12, top: 8, bottom: 8 }
  };
}

export function createOverviewDistributionSpec(state: OverviewChartState): MediaChartSpec {
  const values = state.distribution.map(item => ({
    category: item.category,
    dimension: item.dimension,
    count: item.count,
    nodeId: item.nodeId
  }));
  const spec = commonSpec("overview-distribution", values);
  spec.series = [{
    type: "bar",
    data: { id: "overview-distribution" },
    xField: "category",
    yField: "count",
    seriesField: "dimension",
    barMaxWidth: 28
  }];
  spec.axes = [{ orient: "left", title: { text: "媒体流数" } }, { orient: "bottom", label: { autoRotate: false, autoHide: true } }];
  return spec;
}

// Short aliases keep panel code readable while preserving the explicit domain names above.
export const overviewChartState = buildOverviewChartState;
export const overviewNodeLoadSpec = createOverviewNodeLoadSpec;
export const overviewHealthSpec = createOverviewHealthSpec;
export const overviewDistributionSpec = createOverviewDistributionSpec;

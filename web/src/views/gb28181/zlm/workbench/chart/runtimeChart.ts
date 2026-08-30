import type { ZLMNodeRuntime } from "@/api/gb28181-zlm-runtime";

import type { ChartDatum, MediaChartSpec } from "./overviewChart";

export const RUNTIME_SAMPLE_LIMIT = 60;

export interface RuntimeChartSample {
  nodeId: number;
  asOf: string;
  streamCount: number | null;
  viewerCount: number | null;
  throughput: number | null;
  sessionCount: number | null;
  netThreadLoad: number | null;
  workThreadLoad: number | null;
  fdCount: number | null;
  recordingCount: number | null;
  mediaKnown: boolean;
  metricsKnown: boolean;
}

export interface RuntimeTrendContext {
  nodeId: number | null;
  range: string;
}

export interface RuntimeTrendHistory extends RuntimeTrendContext {
  samples: RuntimeChartSample[];
}

export type RuntimeChartStatus = "ready" | "empty" | "unknown" | "partial";

export interface RuntimeTrendChartState {
  status: RuntimeChartStatus;
  title: string;
  sampleCount: number;
  samples: RuntimeChartSample[];
  asOf: string | null;
  sampledLabel: string;
  summary: string;
  warning: string | null;
  spec: MediaChartSpec;
}

function numberOrNull(value: unknown): number | null {
  return typeof value === "number" && Number.isFinite(value) ? value : null;
}

function nonNegativeOrNull(value: unknown): number | null {
  const number = numberOrNull(value);
  return number !== null && number >= 0 ? number : null;
}

function mediaKnown(runtime: ZLMNodeRuntime): boolean {
  return runtime.mediaFreshness !== "unavailable" && runtime.streams !== undefined;
}

function metricsKnown(runtime: ZLMNodeRuntime): boolean {
  return runtime.metricsComplete === true;
}

export function runtimeSnapshot(runtime: ZLMNodeRuntime): RuntimeChartSample {
  const hasMedia = mediaKnown(runtime);
  const hasMetrics = metricsKnown(runtime);
  const streams = hasMedia ? runtime.streams ?? [] : [];
  return {
    nodeId: runtime.nodeId,
    asOf: runtime.asOf,
    streamCount: hasMedia ? streams.length : null,
    viewerCount: hasMedia ? streams.reduce<number | null>((sum, stream) => {
      const readers = nonNegativeOrNull(stream.readerCount);
      return sum === null || readers === null ? null : sum + readers;
    }, 0) : null,
    throughput: hasMedia ? streams.reduce<number | null>((sum, stream) => {
      const speed = nonNegativeOrNull(stream.bytesSpeed);
      return sum === null || speed === null ? null : sum + speed;
    }, 0) : null,
    sessionCount: hasMetrics ? nonNegativeOrNull(runtime.metrics.networkSessionCount) : null,
    netThreadLoad: hasMetrics ? numberOrNull(runtime.metrics.netThreadLoad) : null,
    workThreadLoad: hasMetrics ? numberOrNull(runtime.metrics.workThreadLoad) : null,
    fdCount: hasMetrics ? nonNegativeOrNull(runtime.metrics.socketCount) : null,
    recordingCount: hasMedia ? streams.reduce<number | null>((count, stream) => count === null ? null : count + (stream.recordingMp4 || stream.recordingHls ? 1 : 0), 0) : null,
    mediaKnown: hasMedia,
    metricsKnown: hasMetrics
  };
}

export function createRuntimeTrendHistory(context: RuntimeTrendContext): RuntimeTrendHistory {
  return { nodeId: context.nodeId, range: context.range, samples: [] };
}

/**
 * Append one fresh runtime response. A context change intentionally starts a
 * new current-session series, so a point from a previous node/range can never
 * be mistaken for part of the current trend.
 */
export function appendRuntimeSnapshot(
  history: RuntimeTrendHistory,
  runtime: ZLMNodeRuntime,
  context: RuntimeTrendContext,
  limit = RUNTIME_SAMPLE_LIMIT
): RuntimeTrendHistory {
  const boundedLimit = Math.min(RUNTIME_SAMPLE_LIMIT, Math.max(1, Math.floor(limit)));
  const nextSample = runtimeSnapshot(runtime);
  const changed = history.nodeId !== context.nodeId || history.range !== context.range;
  const samples = changed ? [] : history.samples;
  return {
    nodeId: context.nodeId,
    range: context.range,
    samples: [...samples, nextSample].slice(-boundedLimit)
  };
}

export function resetRuntimeSnapshots(context: RuntimeTrendContext): RuntimeTrendHistory {
  return createRuntimeTrendHistory(context);
}

function trendValues(samples: readonly RuntimeChartSample[]): ChartDatum[] {
  return samples.flatMap(sample => [
    { asOf: sample.asOf, metric: "吞吐 KB/s", value: sample.throughput === null ? null : sample.throughput / 1024 },
    { asOf: sample.asOf, metric: "播放人数", value: sample.viewerCount }
  ]);
}

export function createRuntimeTrendSpec(samples: readonly RuntimeChartSample[]): MediaChartSpec {
  return {
    type: "line",
    background: "transparent",
    data: [{ id: "runtime-session-trend", values: trendValues(samples) }],
    series: [{
      type: "line",
      data: { id: "runtime-session-trend" },
      xField: "asOf",
      yField: "value",
      seriesField: "metric",
      point: { visible: samples.length <= 20 }
    }],
    axes: [
      { orient: "left", title: { text: "KB/s · 人" }, label: { autoHide: true } },
      { orient: "bottom", label: { autoHide: true, autoRotate: false } }
    ],
    tooltip: { activeType: "dimension" },
    padding: { left: 8, right: 12, top: 8, bottom: 8 }
  };
}

export function buildRuntimeTrendChartState(history: RuntimeTrendHistory | null | undefined): RuntimeTrendChartState {
  if (!history) {
    return {
      status: "unknown",
      title: "实时吞吐趋势",
      sampleCount: 0,
      samples: [],
      asOf: null,
      sampledLabel: "进入页面后采样，最多保留 60 点",
      summary: "当前没有可用的运行态采样",
      warning: "节点运行态尚未返回",
      spec: createRuntimeTrendSpec([])
    };
  }
  const samples = history.samples.slice(-RUNTIME_SAMPLE_LIMIT);
  const knownCount = samples.filter(sample => sample.mediaKnown || sample.metricsKnown).length;
  const hasUnknown = samples.some(sample => !sample.mediaKnown || !sample.metricsKnown);
  const status: RuntimeChartStatus = samples.length === 0 ? "empty" : knownCount === 0 ? "unknown" : hasUnknown ? "partial" : "ready";
  const last = samples.at(-1);
  return {
    status,
    title: "实时吞吐趋势",
    sampleCount: samples.length,
    samples,
    asOf: last?.asOf ?? null,
    sampledLabel: `进入页面后采样，已记录 ${samples.length} / ${RUNTIME_SAMPLE_LIMIT} 点`,
    summary: samples.length === 0
      ? "进入页面后等待运行态采样"
      : `当前节点 ${history.nodeId ?? "—"} 已记录 ${samples.length} 个采样点，最新 ${last?.asOf ?? "—"}`,
    warning: status === "partial" ? "部分采样指标不可用，未知值显示为破折号" : status === "unknown" ? "当前采样不可用，不能判断为 0" : null,
    spec: createRuntimeTrendSpec(samples)
  };
}

// Names used by panels can stay domain-specific without creating a generic dashboard schema.
export const runtimeTrendPoint = runtimeSnapshot;
export const appendRuntimeSample = appendRuntimeSnapshot;
export const runtimeTrendChartState = buildRuntimeTrendChartState;

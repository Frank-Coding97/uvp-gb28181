import type { ZLMNodeRuntime } from "@/api/gb28181-zlm-runtime";

import type { ChartDatum, MediaChartSpec } from "./overviewChart";

export const RUNTIME_SAMPLE_LIMIT = 60;
export const RUNTIME_TREND_WINDOW_MS = 5 * 60 * 1000;

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
  return samples.flatMap(sample => {
    const sampledAt = Date.parse(sample.asOf);
    if (!Number.isFinite(sampledAt)) return [];
    return [{
      sampledAt,
      asOf: sample.asOf,
      metric: "媒体速率 KB/s",
      value: sample.throughput === null ? null : sample.throughput / 1024
    }];
  });
}

export function createRuntimeTrendSpec(samples: readonly RuntimeChartSample[]): MediaChartSpec {
  const parsedValues = trendValues(samples);
  const latestSampledAt = parsedValues.reduce<number | null>((latest, datum) => {
    const sampledAt = typeof datum.sampledAt === "number" ? datum.sampledAt : null;
    return sampledAt === null ? latest : latest === null ? sampledAt : Math.max(latest, sampledAt);
  }, null);
  const windowStart = latestSampledAt === null ? null : latestSampledAt - RUNTIME_TREND_WINDOW_MS;
  const values = windowStart === null
    ? parsedValues
    : parsedValues.filter(datum => typeof datum.sampledAt === "number" && datum.sampledAt >= windowStart);
  const timeDomain = latestSampledAt === null
    ? {}
    : { min: windowStart, max: latestSampledAt };
  return {
    type: "line",
    background: "transparent",
    animationAppear: { duration: 300 },
    animationEnter: { duration: 300 },
    animationUpdate: { duration: 450, easing: "linear" },
    animationExit: { duration: 300 },
    data: [{ id: "runtime-session-trend", values }],
    series: [{
      type: "line",
      data: { id: "runtime-session-trend" },
      xField: "sampledAt",
      yField: "value",
      seriesField: "metric",
      invalidType: "break",
      point: { visible: false }
    }],
    axes: [
      { orient: "left", min: 0, softMax: 1, title: { text: "KB/s" }, label: { autoHide: true } },
      {
        orient: "bottom",
        type: "time",
        nice: false,
        ...timeDomain,
        layers: [{ tickCount: 5, timeFormat: "%H:%M:%S", timeFormatMode: "local" }],
        label: { autoHide: true, autoRotate: false }
      }
    ],
    tooltip: {
      activeType: "dimension",
      dimension: {
        title: {
          value: { field: "sampledAt" },
          valueTimeFormat: "%Y-%m-%d %H:%M:%S",
          valueTimeFormatMode: "local"
        },
        content: [{
          key: "媒体速率",
          value: (datum?: ChartDatum) => {
            const value = datum?.value;
            if (typeof value !== "number" || !Number.isFinite(value)) return "—";
            if (value < 1) return `${Math.round(value * 1024)} B/s`;
            return `${value.toFixed(value >= 100 ? 0 : 1)} KB/s`;
          }
        }]
      }
    },
    padding: { left: 8, right: 12, top: 8, bottom: 8 }
  };
}

export function buildRuntimeTrendChartState(history: RuntimeTrendHistory | null | undefined): RuntimeTrendChartState {
  if (!history) {
    return {
      status: "unknown",
      title: "实时媒体速率",
      sampleCount: 0,
      samples: [],
      asOf: null,
      sampledLabel: "展示最近 5 分钟在线媒体流的实时传输速率合计",
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
    title: "实时媒体速率",
    sampleCount: samples.length,
    samples,
    asOf: last?.asOf ?? null,
    sampledLabel: "展示最近 5 分钟在线媒体流的实时传输速率合计",
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

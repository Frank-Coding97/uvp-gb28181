import type { ZLMNodeRuntime } from "@/api/gb28181-zlm-runtime";
import type { MediaRateSample } from "@/views/gb28181/components/mediaRateChart";

export interface RuntimeChartSample {
  nodeId: number;
  asOf: string;
  streamCount: number | null;
  viewerCount: number | null;
  throughput: number | null;
  upstream: number | null;
  downstream: number | null;
  sessionCount: number | null;
  netThreadLoad: number | null;
  workThreadLoad: number | null;
  fdCount: number | null;
  recordingCount: number | null;
  mediaKnown: boolean;
  metricsKnown: boolean;
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
  const hasMediaTraffic = runtime.metrics.mediaTrafficAvailable === true;
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
    upstream: hasMediaTraffic ? nonNegativeOrNull(runtime.metrics.upstreamBytesPerSecond) : null,
    downstream: hasMediaTraffic ? nonNegativeOrNull(runtime.metrics.downstreamBytesPerSecond) : null,
    sessionCount: hasMetrics ? nonNegativeOrNull(runtime.metrics.networkSessionCount) : null,
    netThreadLoad: hasMetrics ? numberOrNull(runtime.metrics.netThreadLoad) : null,
    workThreadLoad: hasMetrics ? numberOrNull(runtime.metrics.workThreadLoad) : null,
    fdCount: hasMetrics ? nonNegativeOrNull(runtime.metrics.socketCount) : null,
    recordingCount: hasMedia ? streams.reduce<number | null>((count, stream) => count === null ? null : count + (stream.recordingMp4 || stream.recordingHls ? 1 : 0), 0) : null,
    mediaKnown: hasMedia,
    metricsKnown: hasMetrics
  };
}

export function runtimeMediaRateSamples(runtime: ZLMNodeRuntime | null | undefined): MediaRateSample[] {
  return (runtime?.mediaRateSamples ?? []).flatMap(sample => {
    if (!Number.isFinite(sample.sampledAt)) return [];
    const upstream = nonNegativeOrNull(sample.upstream);
    const downstream = nonNegativeOrNull(sample.downstream);
    if (upstream === null || downstream === null) return [];
    return [{ sampledAt: sample.sampledAt, upstream, downstream }];
  });
}

export function runtimeTrendSamples(runtime: ZLMNodeRuntime | null | undefined) {
  return (runtime?.trendSamples ?? []).filter(sample => Number.isFinite(sample.sampledAt));
}

import type { ZLMNodeRuntime } from "@/api/gb28181-zlm-runtime";

export interface RuntimeTrendPoint {
  asOf: string;
  streamCount: number;
  viewerCount: number;
  throughput: number;
  sessionCount: number;
  netThreadLoad: number;
  workThreadLoad: number;
  fdCount: number;
  recordingCount: number;
}

function streamsOf(runtime: ZLMNodeRuntime) {
  return runtime.streams ?? [];
}

export function runtimeTrendPoint(runtime: ZLMNodeRuntime): RuntimeTrendPoint {
  const streams = streamsOf(runtime);
  return {
    asOf: runtime.asOf,
    streamCount: streams.length,
    viewerCount: streams.reduce((sum, stream) => sum + Math.max(0, stream.readerCount), 0),
    throughput: streams.reduce((sum, stream) => sum + Math.max(0, stream.bytesSpeed), 0),
    sessionCount: runtime.metrics.networkSessionCount,
    netThreadLoad: runtime.metrics.netThreadLoad,
    workThreadLoad: runtime.metrics.workThreadLoad,
    fdCount: runtime.metrics.socketCount,
    recordingCount: streams.filter(stream => stream.recordingMp4 || stream.recordingHls).length
  };
}

export function appendRuntimeSample(
  history: RuntimeTrendPoint[],
  runtime: ZLMNodeRuntime,
  limit = 60
): RuntimeTrendPoint[] {
  const boundedLimit = Math.max(1, Math.floor(limit));
  return [...history, runtimeTrendPoint(runtime)].slice(-boundedLimit);
}

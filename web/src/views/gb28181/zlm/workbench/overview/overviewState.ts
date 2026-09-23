import type { ZLMOverview, ZLMRuntimeMedia } from "@/api/gb28181-zlm-runtime";

export type OverviewKpiKey = "nodes" | "streams" | "sessions" | "viewers" | "throughput" | "recordings";
export type OverviewKpiState = "ready" | "partial" | "unknown";

export interface OverviewKpi {
  key: OverviewKpiKey;
  label: string;
  value: number | null;
  state: OverviewKpiState;
  note: string;
  unit?: string;
}

function positiveIds(values: readonly number[] | undefined): number[] {
  return (values ?? []).filter(value => Number.isSafeInteger(value) && value > 0);
}

function sampleState(sampled: boolean, partial: boolean): OverviewKpiState {
  if (!sampled) return "unknown";
  return partial ? "partial" : "ready";
}

function sampleNote(state: OverviewKpiState): string {
  if (state === "unknown") return "未知，尚未采样";
  if (state === "partial") return "部分采样，仅展示已返回节点";
  return "采样完成";
}

function sumStreams(streams: readonly ZLMRuntimeMedia[], pick: (stream: ZLMRuntimeMedia) => number): number {
  return streams.reduce((total, stream) => total + pick(stream), 0);
}

/**
 * Builds every overview KPI from the same server snapshot.
 *
 * Node count is registry data and remains known even when all runtime readers
 * fail. All other values are null until their corresponding sampling scope is
 * present; null is intentionally rendered as an em dash by the panel.
 */
export function buildOverviewKpis(overview: ZLMOverview | null | undefined): OverviewKpi[] {
  const nodes = overview?.nodes ?? [];
  const streams = overview?.streams ?? [];
  const metricsSampled = Array.isArray(overview?.metricsSampledNodeIds)
    ? positiveIds(overview.metricsSampledNodeIds).length > 0
    : (overview?.metrics.sampledNodeCount ?? 0) > 0;
  const mediaSampled = Array.isArray(overview?.mediaSampledNodeIds)
    ? positiveIds(overview.mediaSampledNodeIds).length > 0
    : streams.length > 0;
  const partial = !!overview?.partial || (overview?.failedNodeIds?.length ?? 0) > 0;
  const runtimeState = sampleState(metricsSampled, partial);
  const mediaState = sampleState(mediaSampled, partial);

  return [
    { key: "nodes", label: "媒体节点", value: overview ? nodes.length : null, state: overview ? "ready" : "unknown", note: overview ? `${nodes.length} 个已纳入当前视图` : "未知，集群采样尚未返回" },
    { key: "streams", label: "在线媒体流", value: mediaSampled ? (overview?.metrics.streamCount ?? streams.length) : null, state: mediaState, note: sampleNote(mediaState) },
    { key: "sessions", label: "网络会话", value: metricsSampled ? (overview?.metrics.networkSessionCount ?? null) : null, state: runtimeState, note: sampleNote(runtimeState) },
    { key: "viewers", label: "观看者", value: mediaSampled ? sumStreams(streams, stream => stream.readerCount) : null, state: mediaState, note: sampleNote(mediaState) },
    { key: "throughput", label: "吞吐", value: mediaSampled ? sumStreams(streams, stream => stream.bytesSpeed) : null, state: mediaState, note: sampleNote(mediaState), unit: "B/s" },
    { key: "recordings", label: "录制中", value: mediaSampled ? sumStreams(streams, stream => stream.recordingMp4 || stream.recordingHls ? 1 : 0) : null, state: mediaState, note: sampleNote(mediaState) }
  ];
}

export function overviewKpiValueText(kpi: OverviewKpi): string {
  if (kpi.value === null) return "—";
  return kpi.unit === "B/s" ? `${kpi.value} B/s` : String(kpi.value);
}

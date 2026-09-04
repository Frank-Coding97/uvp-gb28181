import type { ZLMOverview, ZLMRuntimeMedia } from "@/api/gb28181-zlm-runtime";

export type DashboardHistoryRange = "1h" | "24h" | "7d";
export type DashboardDrilldownMetric = "sip-rpm" | "sip-today" | "play-success-24h" | "media-traffic-today";
export type MediaRuntimeLedgerKind = "streams" | "viewers" | "sessions" | "recordings";

export const DASHBOARD_DRILLDOWN_RANGES: Record<DashboardDrilldownMetric, DashboardHistoryRange[]> = {
  "sip-rpm": ["1h", "24h", "7d"],
  "sip-today": ["1h", "24h", "7d"],
  "play-success-24h": ["1h", "24h", "7d"],
  "media-traffic-today": ["24h", "7d"]
};

export interface MediaRuntimeSessionRow {
  nodeId: number;
  nodeName: string;
  sessions: number;
  sampled: boolean;
}

export interface MediaRuntimeLedger {
  streams: ZLMRuntimeMedia[];
  viewers: ZLMRuntimeMedia[];
  sessions: MediaRuntimeSessionRow[];
  recordings: ZLMRuntimeMedia[];
  totals: { streams: number; viewers: number; sessions: number; recordings: number };
  partial: boolean;
  warnings: string[];
  asOf: string;
}

export function buildMediaRuntimeLedger(overview: ZLMOverview | null | undefined): MediaRuntimeLedger {
  const streams = overview?.streams ?? [];
  const viewers = streams.filter(item => Math.max(0, item.readerCount) > 0);
  const recordings = streams.filter(item => item.recordingMp4 || item.recordingHls);
  const sampledNodeIds = new Set(overview?.metricsSampledNodeIds ?? []);
  const sessions = (overview?.nodes ?? []).map(node => ({
    nodeId: node.nodeId,
    nodeName: node.name,
    sessions: Math.max(0, node.metrics.networkSessionCount),
    sampled: sampledNodeIds.has(node.nodeId)
  }));
  const totals = {
    streams: streams.length,
    viewers: streams.reduce((sum, item) => sum + Math.max(0, item.readerCount), 0),
    sessions: sessions.filter(item => item.sampled).reduce((sum, item) => sum + item.sessions, 0),
    recordings: recordings.length
  };
  const warnings: string[] = [];
  if (overview && overview.metrics.streamCount !== totals.streams) warnings.push("在线流汇总与明细数量不一致");
  if (overview && overview.metrics.networkSessionCount !== totals.sessions) warnings.push("网络会话汇总与节点明细不一致");
  return {
    streams,
    viewers,
    sessions,
    recordings,
    totals,
    partial: !!overview?.partial || warnings.length > 0 || sessions.some(item => !item.sampled),
    warnings,
    asOf: overview?.asOf ?? ""
  };
}

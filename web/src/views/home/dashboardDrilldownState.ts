import type { ZLMMediaIdentity, ZLMOverview } from "@/api/gb28181-zlm-runtime";
import type { HomeRuntimeBinding } from "@/api/home-dashboard";

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

export interface MediaBusinessStream {
  key: string;
  nodeId: number;
  nodeName: string;
  vhost: string;
  app: string;
  streamId: string;
  deviceId: string;
  deviceName: string;
  channelId: string;
  channelName: string;
  protocols: string[];
  viewers: number;
  totalReaders: number;
  bytesSpeed: number;
  aliveSecond: number;
  recordingMp4: boolean;
  recordingHls: boolean;
  mediaTargets: ZLMMediaIdentity[];
}

export interface MediaRuntimeLedger {
  streams: MediaBusinessStream[];
  viewers: MediaBusinessStream[];
  sessions: MediaRuntimeSessionRow[];
  recordings: MediaBusinessStream[];
  totals: { streams: number; viewers: number; sessions: number; recordings: number };
  partial: boolean;
  warnings: string[];
  asOf: string;
}

export function buildMediaRuntimeLedger(overview: ZLMOverview | null | undefined, bindings: HomeRuntimeBinding[] = []): MediaRuntimeLedger {
  const rawStreams = overview?.streams ?? [];
  const bindingByStream = new Map(bindings.map(item => [item.streamId, item]));
  const nodeNames = new Map((overview?.nodes ?? []).map(item => [item.nodeId, item.name]));
  const grouped = new Map<string, MediaBusinessStream>();
  for (const item of rawStreams) {
    const key = `${item.nodeId}\u001f${item.media.vhost}\u001f${item.media.app}\u001f${item.media.stream}`;
    const binding = bindingByStream.get(item.media.stream);
    const current = grouped.get(key) ?? {
      key,
      nodeId: item.nodeId,
      nodeName: nodeNames.get(item.nodeId) ?? `节点 #${item.nodeId}`,
      vhost: item.media.vhost,
      app: item.media.app,
      streamId: item.media.stream,
      deviceId: binding?.deviceId ?? "",
      deviceName: binding?.deviceName ?? "",
      channelId: binding?.channelId ?? "",
      channelName: binding?.channelName ?? "",
      protocols: [],
      viewers: 0,
      totalReaders: 0,
      bytesSpeed: 0,
      aliveSecond: 0,
      recordingMp4: false,
      recordingHls: false,
      mediaTargets: []
    };
    if (!current.protocols.includes(item.media.schema)) current.protocols.push(item.media.schema);
    current.viewers += Math.max(0, item.readerCount);
    current.totalReaders += Math.max(0, item.totalReaderCount);
    current.bytesSpeed = Math.max(current.bytesSpeed, Math.max(0, item.bytesSpeed));
    current.aliveSecond = Math.max(current.aliveSecond, Math.max(0, item.aliveSecond));
    current.recordingMp4 ||= item.recordingMp4;
    current.recordingHls ||= item.recordingHls;
    current.mediaTargets.push({ ...item.media });
    grouped.set(key, current);
  }
  const streams = [...grouped.values()].map(item => ({ ...item, protocols: item.protocols.sort() }))
    .sort((a, b) => b.viewers - a.viewers || b.bytesSpeed - a.bytesSpeed || a.key.localeCompare(b.key));
  const viewers = streams.filter(item => item.viewers > 0);
  const recordings = streams.filter(item => item.recordingMp4);
  const sampledNodeIds = new Set(overview?.metricsSampledNodeIds ?? []);
  const sessions = (overview?.nodes ?? []).map(node => ({
    nodeId: node.nodeId,
    nodeName: node.name,
    sessions: Math.max(0, node.metrics.networkSessionCount),
    sampled: sampledNodeIds.has(node.nodeId)
  }));
  const totals = {
    streams: streams.length,
    viewers: streams.reduce((sum, item) => sum + item.viewers, 0),
    sessions: sessions.filter(item => item.sampled).reduce((sum, item) => sum + item.sessions, 0),
    recordings: recordings.length
  };
  const warnings: string[] = [];
  if (overview && overview.metrics.streamCount !== rawStreams.length) warnings.push("在线流汇总与协议明细数量不一致");
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

import type { ZLMAudienceViewer, ZLMMediaIdentity, ZLMOverview } from "@/api/gb28181-zlm-runtime";
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
  /**
   * 按远端 IP 聚合的观众口径。
   *
   * ⛔ totals.viewers 优先取这里的 uniqueIps（一人多路算 1）；available=false
   * 时（后端未装配播放器读取能力）必须回退到 Σ readerCount，不能显示 0。
   */
  audience: {
    available: boolean;
    uniqueIps: number;
    connections: number;
    viewers: ZLMAudienceViewer[];
    partial: boolean;
    truncated: boolean;
  };
  totals: { streams: number; viewers: number; connections: number; sessions: number; recordings: number };
  partial: boolean;
  warnings: string[];
  asOf: string;
}

export function buildMediaRuntimeLedger(
  overview: ZLMOverview | null | undefined,
  bindings: HomeRuntimeBinding[] = []
): MediaRuntimeLedger {
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
  const streams = [...grouped.values()]
    .map(item => ({ ...item, protocols: item.protocols.sort() }))
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
  const audience = {
    available: !!overview?.audience?.available,
    uniqueIps: Math.max(0, overview?.audience?.uniqueIps ?? 0),
    connections: Math.max(0, overview?.audience?.connections ?? 0),
    viewers: overview?.audience?.viewers ?? [],
    partial: !!overview?.audience?.partial,
    truncated: !!overview?.audience?.truncated
  };
  const totals = {
    streams: streams.length,
    viewers: audience.available ? audience.uniqueIps : streams.reduce((sum, item) => sum + item.viewers, 0),
    connections: audience.available ? audience.connections : streams.reduce((sum, item) => sum + item.viewers, 0),
    sessions: sessions.filter(item => item.sampled).reduce((sum, item) => sum + item.sessions, 0),
    recordings: recordings.length
  };
  const warnings: string[] = [];
  // 后端 streamCount 是「逻辑流数」（同一条流只算一次，不分协议），
  // 所以必须跟分组后的 streams 比。拿它比 rawStreams.length 会把
  // 一条流的多协议输出误报成「数量不一致」。
  if (overview && overview.metrics.streamCount !== streams.length) warnings.push("在线流汇总与协议明细数量不一致");
  if (overview && overview.metrics.networkSessionCount !== totals.sessions) warnings.push("网络会话汇总与节点明细不一致");
  if (audience.truncated) warnings.push("观众明细已截断，仅统计前 128 路有人的流");
  if (audience.partial) warnings.push("部分节点观众明细读取失败，人数可能偏少");
  return {
    streams,
    viewers,
    sessions,
    recordings,
    audience,
    totals,
    partial: !!overview?.partial || warnings.length > 0 || sessions.some(item => !item.sampled),
    warnings,
    asOf: overview?.asOf ?? ""
  };
}

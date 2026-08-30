import type { MediaScope } from "@/store/modules/media-workbench";
import type {
  ZLMNodeRuntime,
  ZLMOverview,
  ZLMOwnershipTarget,
  ZLMStreamQuery,
  ZLMStreamViewer
} from "@/api/gb28181-zlm-runtime";

export interface MonitoringQuery {
  schema?: string;
  vhost?: string;
  app?: string;
  stream?: string;
  recording?: "mp4" | "hls";
  peerIp?: string;
  localPort?: string;
}

export interface MonitoringStreamFilters {
  schema: string;
  vhost: string;
  app: string;
  stream: string;
  recording: "" | "mp4" | "hls";
}

export interface MonitoringViewerFilters {
  schema: string;
  vhost: string;
  app: string;
  stream: string;
}

export interface MonitoringNetworkFilters {
  peerIp: string;
  localPort: string;
  page: number;
  pageSize: number;
}

function queryText(value: unknown): string {
  const candidate = Array.isArray(value) ? value[0] : value;
  return typeof candidate === "string" ? candidate : typeof candidate === "number" ? String(candidate) : "";
}

function queryRecording(query: Record<string, unknown>): MonitoringStreamFilters["recording"] {
  const recording = queryText(query.recording).toLowerCase();
  if (recording === "mp4" || queryText(query.recordingMp4).toLowerCase() === "true") return "mp4";
  if (recording === "hls" || queryText(query.recordingHls).toLowerCase() === "true") return "hls";
  return "";
}

export function createStreamFilters(query: Record<string, unknown> = {}): MonitoringStreamFilters {
  return {
    schema: queryText(query.schema),
    vhost: queryText(query.vhost),
    app: queryText(query.app),
    stream: queryText(query.stream),
    recording: queryRecording(query)
  };
}

export function createViewerFilters(query: Record<string, unknown> = {}): MonitoringViewerFilters {
  return {
    schema: queryText(query.schema),
    vhost: queryText(query.vhost),
    app: queryText(query.app),
    stream: queryText(query.stream)
  };
}

export function createNetworkFilters(query: Record<string, unknown> = {}): MonitoringNetworkFilters {
  return {
    peerIp: queryText(query.peerIp),
    localPort: queryText(query.localPort),
    page: 1,
    pageSize: 20
  };
}

export function buildStreamRequestQuery(
  filters: MonitoringStreamFilters,
  page: number,
  pageSize: number,
  nodeId?: number
): ZLMStreamQuery {
  return {
    ...(nodeId ? { nodeId } : {}),
    page,
    pageSize,
    schema: filters.schema.trim() || undefined,
    vhost: filters.vhost.trim() || undefined,
    app: filters.app.trim() || undefined,
    stream: filters.stream.trim() || undefined,
    recordingMp4: filters.recording === "mp4" ? true : undefined,
    recordingHls: filters.recording === "hls" ? true : undefined
  };
}

export function scopeRange(scope: MediaScope, nodeId: number | null): string {
  return scope === "all" ? "all" : `node:${nodeId ?? scope}`;
}

export function selectedTargetNode(targets: readonly ZLMOwnershipTarget[]): number | null {
  const nodeId = targets[0]?.nodeId;
  return targets.length > 0 && targets.every(target => target.nodeId === nodeId) ? nodeId ?? null : null;
}

export function sameNodeTargets(targets: readonly ZLMOwnershipTarget[]): boolean {
  return targets.length > 0 && selectedTargetNode(targets) !== null;
}

export function targetFromViewer(viewer: Pick<ZLMStreamViewer, "nodeId" | "media">): ZLMOwnershipTarget {
  return { nodeId: viewer.nodeId, media: { ...viewer.media } };
}

/** Turn an aggregate backend response into the runtime shape consumed by the chart/KPI panel. */
export function overviewAsRuntime(overview: ZLMOverview): ZLMNodeRuntime {
  const sampledNodeCount = overview.metrics.sampledNodeCount;
  const mediaKnown = overview.mediaSampledNodeIds.length > 0;
  const allNodesMediaSampled = sampledNodeCount > 0 && overview.mediaSampledNodeIds.length >= sampledNodeCount;
  const metricsComplete = sampledNodeCount > 0 && overview.metricsSampledNodeIds.length >= sampledNodeCount && !overview.partial;
  return {
    nodeId: 0,
    name: `全部节点（${sampledNodeCount}）`,
    state: "active",
    status: overview.partial ? "partial" : "fresh",
    freshness: overview.partial ? "stale" : "fresh",
    asOf: overview.asOf,
    heartbeatFreshness: overview.partial ? "stale" : "fresh",
    metrics: {
      mediaSourceCount: overview.metrics.mediaSourceCount,
      multiMediaSourceMuxerCount: overview.metrics.multiMediaSourceMuxerCount,
      tcpServerCount: overview.metrics.tcpServerCount,
      tcpSessionCount: overview.metrics.tcpSessionCount,
      udpServerCount: overview.metrics.udpServerCount,
      udpSessionCount: overview.metrics.udpSessionCount,
      tcpClientCount: overview.metrics.tcpClientCount,
      socketCount: overview.metrics.socketCount,
      networkSessionCount: overview.metrics.networkSessionCount,
      netThreadLoad: overview.metrics.netThreadLoadAvg,
      workThreadLoad: overview.metrics.workThreadLoadAvg,
      objectStatistics: overview.metrics.objectStatistics,
      eventThreadLoads: overview.nodes.flatMap(node => (node.metrics.eventThreadLoads ?? []).map(thread => ({
        ...thread,
        nodeId: node.nodeId,
        name: `${node.name} · ${thread.name}`
      })))
    },
    metricsComplete,
    mediaFreshness: allNodesMediaSampled ? "fresh" : mediaKnown ? "stale" : "unavailable",
    streams: mediaKnown ? overview.streams : undefined,
    errors: overview.errors?.map(item => item.error)
  };
}

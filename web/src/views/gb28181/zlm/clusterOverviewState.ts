import type { ZLMOverview, ZLMRuntimeMedia } from "@/api/gb28181-zlm-runtime";

export type OverviewHealthKind = "healthy" | "partial" | "unavailable" | "inactive" | "empty";

export interface OverviewHealthSummary {
  kind: OverviewHealthKind;
  successfulCount: number;
  failedCount: number;
  accessibleLabel: string;
}

export function overviewHealthSummary(overview: ZLMOverview): OverviewHealthSummary {
  const successfulCount = overview.successfulNodeIds.length
    || overview.nodes.filter(node => node.status === "fresh" || node.status === "partial").length;
  const failedCount = overview.failedNodeIds.length
    || overview.nodes.filter(node => node.status === "unavailable").length;

  if (overview.nodes.length === 0 && successfulCount === 0 && failedCount === 0) {
    return { kind: "empty", successfulCount, failedCount, accessibleLabel: "集群尚未配置媒体节点" };
  }
  if (successfulCount === 0 && failedCount === 0) {
    const maintenance = overview.nodes.filter(node => node.state === "maintenance").length;
    const offline = overview.nodes.filter(node => node.state === "offline").length;
    return {
      kind: "inactive",
      successfulCount,
      failedCount,
      accessibleLabel: `集群没有活跃采样节点，${maintenance} 个维护，${offline} 个离线`
    };
  }
  if (successfulCount === 0 && failedCount > 0) {
    return { kind: "unavailable", successfulCount, failedCount, accessibleLabel: `集群不可用，${failedCount} 个节点采集失败` };
  }
  if (overview.partial || failedCount > 0) {
    return {
      kind: "partial",
      successfulCount,
      failedCount,
      accessibleLabel: `集群部分可用，${successfulCount} 个节点成功，${failedCount} 个节点失败`
    };
  }
  return { kind: "healthy", successfulCount, failedCount, accessibleLabel: `集群正常，${successfulCount} 个节点采集成功` };
}

export function nodeOverviewLocation(nodeId: number) {
  return {
    path: `/media/nodes/${nodeId}`,
    query: { view: "overview", nodeId: String(nodeId) }
  };
}

export function streamOverviewLocation(stream: ZLMRuntimeMedia) {
  return {
    path: "/media/monitoring",
    query: {
      view: "streams",
      nodeId: String(stream.nodeId),
      schema: stream.media.schema,
      vhost: stream.media.vhost,
      app: stream.media.app,
      stream: stream.media.stream
    }
  };
}

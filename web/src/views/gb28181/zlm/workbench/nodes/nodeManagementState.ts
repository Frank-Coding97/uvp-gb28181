import type { ZLMNode, ZLMNodeState } from "@/api/gb28181-zlm";

export type NodeDetailView = "overview" | "runtime" | "config";
export type NodeHealth = "healthy" | "warning" | "critical" | "unknown";

export interface NodeListRecord {
  id: number;
  name: string;
  host: string;
  state: ZLMNodeState;
  recoveryRequired?: boolean;
  recoveryReason?: string;
  nearCapacity?: boolean;
  autoOnDemandReady?: boolean;
}

export interface NodeListFilter {
  keyword?: string;
  state?: ZLMNodeState;
  health?: NodeHealth;
}

const NODE_DETAIL_VIEWS: readonly NodeDetailView[] = ["overview", "runtime", "config"];

export function resolveNodeDetailView(value: unknown): NodeDetailView {
  return typeof value === "string" && NODE_DETAIL_VIEWS.includes(value as NodeDetailView)
    ? value as NodeDetailView
    : "overview";
}

export function nodeHealth(node: Pick<ZLMNode, "state"> & Partial<Pick<ZLMNode, "recoveryRequired" | "nearCapacity" | "autoOnDemandReady">>): NodeHealth {
  if (node.recoveryRequired || node.state === "offline") return "critical";
  if (node.state === "maintenance") return "unknown";
  return node.nearCapacity || node.autoOnDemandReady === false ? "warning" : "healthy";
}

export function nodeHealthReason(node: Pick<ZLMNode, "state"> & Partial<Pick<ZLMNode, "recoveryRequired" | "recoveryReason" | "nearCapacity" | "autoOnDemandReady">>) {
  if (node.recoveryRequired) return node.recoveryReason || "配置恢复未完成";
  if (node.state === "offline") return "节点离线";
  if (node.state === "maintenance") return "节点处于维护状态";
  if (node.autoOnDemandReady === false) return "自动按需配置尚未收敛";
  if (node.nearCapacity) return "接近容量";
  return "";
}

export function filterNodeRecords<T extends NodeListRecord>(nodes: readonly T[], filter: NodeListFilter = {}) {
  const keyword = filter.keyword?.trim().toLowerCase() ?? "";
  return nodes.filter(node => {
    if (keyword && !node.name.toLowerCase().includes(keyword) && !node.host.toLowerCase().includes(keyword)) return false;
    if (filter.state && node.state !== filter.state) return false;
    return !filter.health || nodeHealth(node) === filter.health;
  });
}

export function isNodeDetailView(value: unknown): value is NodeDetailView {
  return NODE_DETAIL_VIEWS.includes(value as NodeDetailView);
}

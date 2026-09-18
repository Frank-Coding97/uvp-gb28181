import type { ZLMNode, ZLMNodeState } from "@/api/gb28181-zlm";

export type NodeHealth = "healthy" | "warning" | "critical" | "unknown";

export interface NodeListRecord {
  id: number;
  name: string;
  host: string;
  state: ZLMNodeState;
  enabled?: boolean;
  health?: ZLMNodeState;
  recoveryRequired?: boolean;
  recoveryReason?: string;
  nearCapacity?: boolean;
  autoOnDemandReady?: boolean;
}

export interface NodeListFilter {
  keyword?: string;
  state?: ZLMNodeState;
  health?: NodeHealth;
  enabled?: boolean;
}

export function nodeOnlineState(node: Pick<ZLMNode, "state"> & Partial<Pick<ZLMNode, "health">>) {
  return node.health ?? node.state;
}

export function nodeHealth(node: Pick<ZLMNode, "state"> & Partial<Pick<ZLMNode, "health" | "recoveryRequired" | "nearCapacity" | "autoOnDemandReady" | "enabled">>): NodeHealth {
  if (node.recoveryRequired || nodeOnlineState(node) === "offline") return "critical";
  return node.nearCapacity || node.autoOnDemandReady === false ? "warning" : "healthy";
}

export function nodeHealthReason(node: Pick<ZLMNode, "state"> & Partial<Pick<ZLMNode, "health" | "recoveryRequired" | "recoveryReason" | "nearCapacity" | "autoOnDemandReady" | "enabled">>) {
  if (node.recoveryRequired) return node.recoveryReason || "配置恢复未完成";
  if (nodeOnlineState(node) === "offline") return "节点离线";
  if (node.autoOnDemandReady === false) return "自动按需配置尚未收敛";
  if (node.nearCapacity) return "接近容量";
  return "";
}

export function filterNodeRecords<T extends NodeListRecord>(nodes: readonly T[], filter: NodeListFilter = {}) {
  const keyword = filter.keyword?.trim().toLowerCase() ?? "";
  return nodes.filter(node => {
    if (keyword && !node.name.toLowerCase().includes(keyword) && !node.host.toLowerCase().includes(keyword)) return false;
    if (filter.state && nodeOnlineState(node) !== filter.state) return false;
    if (filter.enabled !== undefined && (node.enabled !== false) !== filter.enabled) return false;
    return !filter.health || nodeHealth(node) === filter.health;
  });
}

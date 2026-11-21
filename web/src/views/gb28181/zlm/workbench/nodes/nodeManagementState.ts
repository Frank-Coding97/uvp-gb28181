import type { ZLMNode, ZLMNodeState } from "@/api/gb28181-zlm";

export type NodeHealth = "healthy" | "warning" | "critical" | "unknown";

export interface NodeListRecord {
  id: number;
  name: string;
  host: string;
  state: ZLMNodeState;
  /** 节点身份 UUID（= 下发到 ZLM 的 general.mediaServerId）。台账要能按它反查现场。 */
  mediaServerUUID?: string;
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

export function nodeHealth(
  node: Pick<ZLMNode, "state"> &
    Partial<Pick<ZLMNode, "health" | "recoveryRequired" | "nearCapacity" | "autoOnDemandReady" | "enabled">>
): NodeHealth {
  if (node.recoveryRequired || nodeOnlineState(node) === "offline") return "critical";
  return node.nearCapacity || node.autoOnDemandReady === false ? "warning" : "healthy";
}

export function nodeHealthReason(
  node: Pick<ZLMNode, "state"> &
    Partial<Pick<ZLMNode, "health" | "recoveryRequired" | "recoveryReason" | "nearCapacity" | "autoOnDemandReady" | "enabled">>
) {
  if (node.recoveryRequired) return node.recoveryReason || "配置恢复未完成";
  if (nodeOnlineState(node) === "offline") return "节点离线";
  if (node.autoOnDemandReady === false) return "自动按需配置尚未收敛";
  if (node.nearCapacity) return "接近容量";
  return "";
}

export function filterNodeRecords<T extends NodeListRecord>(nodes: readonly T[], filter: NodeListFilter = {}) {
  const keyword = filter.keyword?.trim().toLowerCase() ?? "";
  return nodes.filter(node => {
    if (keyword && !matchesNodeKeyword(node, keyword)) return false;
    if (filter.state && nodeOnlineState(node) !== filter.state) return false;
    if (filter.enabled !== undefined && (node.enabled !== false) !== filter.enabled) return false;
    return !filter.health || nodeHealth(node) === filter.health;
  });
}

/**
 * 关键词匹配：别名 / Host / mediaid。
 *
 * mediaid 也参与匹配，是因为台账把它摆在明面上就是为了「拿现场日志里的一串 UUID 反查是哪个节点」——
 * 只能看不能搜，等于还得人眼比对。
 */
function matchesNodeKeyword(node: NodeListRecord, keyword: string) {
  if (node.name.toLowerCase().includes(keyword)) return true;
  if (node.host.toLowerCase().includes(keyword)) return true;
  return (node.mediaServerUUID ?? "").toLowerCase().includes(keyword);
}

import type { ZLMNodeImpactPreflight } from "@/api/gb28181-zlm";

export type NodeImpactAction = ZLMNodeImpactPreflight["action"];
export type NodeDangerAction = NodeImpactAction | "restart";
export type NodeImpactPreflightLike = ZLMNodeImpactPreflight;

const actionCopies: Record<NodeDangerAction, { title: string; verb: string; actionLabel: string }> = {
  maintenance: { title: "切换维护态", verb: "维护", actionLabel: "切换维护态" },
  kick: { title: "驱逐全部会话", verb: "驱逐", actionLabel: "驱逐会话" },
  delete: { title: "删除媒体节点", verb: "删除", actionLabel: "删除节点" },
  restart: { title: "重启 ZLM 服务", verb: "重启", actionLabel: "提交重启" }
};

export function nodeActionCopy(action: NodeDangerAction, nodeName: string) {
  const copy = actionCopies[action];
  return {
    ...copy,
    confirmPhrase: `${copy.verb} ${nodeName}`,
    targetLabel: `${copy.title}：${nodeName}`
  };
}

export function isNodeImpactPreflight(value: unknown, nodeId: number, action: NodeImpactAction): value is ZLMNodeImpactPreflight {
  const candidate = value as Partial<ZLMNodeImpactPreflight> | null;
  return candidate?.nodeId === nodeId
    && candidate.action === action
    && typeof candidate.fingerprint === "string"
    && candidate.fingerprint.trim().length > 0
    && typeof candidate.impact === "object";
}

export function nodeImpactItems(preflight: NodeImpactPreflightLike) {
  const items = [
    `活动流 ${preflight.impact.streams} 路`,
    `录制任务 ${preflight.impact.recordings} 个`,
    `网络会话 ${preflight.impact.sessions} 个`
  ];
  if (preflight.impact.truncated) items.push("影响结果已截断，实际数量可能更多");
  return items;
}

export function nodeImpactItemsFromError(error: unknown) {
  const responseData = (error as { response?: { data?: unknown } } | null)?.response?.data;
  const envelope = responseData as { data?: unknown } | null;
  const candidate = (envelope?.data ?? responseData) as { impact?: ZLMNodeImpactPreflight["impact"] } | null;
  const impact = candidate?.impact;
  if (!impact || ![impact.streams, impact.recordings, impact.sessions].every(Number.isFinite)) return [];
  return nodeImpactItems({
    nodeId: 0,
    action: "maintenance",
    impact,
    fingerprint: "conflict",
    observedAt: ""
  });
}

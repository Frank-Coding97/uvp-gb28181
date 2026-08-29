import type {
  ZLMMediaIdentity,
  ZLMStreamClosePreflight,
  ZLMStreamQuery
} from "@/api/gb28181-zlm-runtime";

export interface StreamRouteFilter extends ZLMMediaIdentity {
  nodeId: number;
}

export interface StreamCloseDecision {
  allowed: boolean;
  mode: "normal" | "force" | "blocked" | "absent";
  reason: string;
}

function first(value: unknown) {
  return Array.isArray(value) ? value[0] : value;
}

function positiveInteger(value: unknown) {
  const raw = first(value);
  if (typeof raw === "number") return Number.isSafeInteger(raw) && raw > 0 ? raw : null;
  if (typeof raw !== "string" || !/^\d+$/.test(raw.trim())) return null;
  const parsed = Number(raw);
  return Number.isSafeInteger(parsed) && parsed > 0 ? parsed : null;
}

function text(value: unknown) {
  const raw = first(value);
  return typeof raw === "string" ? raw.trim() : "";
}

export function streamIdentityKey(nodeId: number, media: ZLMMediaIdentity) {
  return [nodeId, media.schema, media.vhost, media.app, media.stream].join("\u001f");
}

export function streamRouteFilter(query: Record<string, unknown>): StreamRouteFilter | null {
  const nodeId = positiveInteger(query.nodeId);
  const media = {
    schema: text(query.schema),
    vhost: text(query.vhost),
    app: text(query.app),
    stream: text(query.stream)
  };
  return nodeId && Object.values(media).every(Boolean) ? { nodeId, ...media } : null;
}

export function buildStreamQuery(values: ZLMStreamQuery): ZLMStreamQuery {
  const query: ZLMStreamQuery = {};
  const nodeId = positiveInteger(values.nodeId);
  const page = positiveInteger(values.page);
  const pageSize = positiveInteger(values.pageSize);
  if (nodeId) query.nodeId = nodeId;
  if (page) query.page = page;
  if (pageSize) query.pageSize = Math.min(100, pageSize);
  for (const key of ["schema", "vhost", "app", "stream"] as const) {
    const value = text(values[key]);
    if (value) query[key] = value;
  }
  if (Number.isInteger(values.originType)) query.originType = values.originType;
  if (typeof values.recordingMp4 === "boolean") query.recordingMp4 = values.recordingMp4;
  if (typeof values.recordingHls === "boolean") query.recordingHls = values.recordingHls;
  return query;
}

export function streamCloseDecision(
  preflight: ZLMStreamClosePreflight,
  forceRequested: boolean,
  hasForcePermission: boolean
): StreamCloseDecision {
  if (!preflight.freshPresent || preflight.snapshot.status === "absent") {
    return { allowed: false, mode: "absent", reason: "后端回读确认该流已经不存在，无需再次关闭。" };
  }
  if (forceRequested) {
    return hasForcePermission
      ? { allowed: true, mode: "force", reason: "将按后端最新 fingerprint 执行强制关闭。" }
      : { allowed: false, mode: "blocked", reason: "缺少流强制关闭权限，未执行任何操作。" };
  }
  if (preflight.snapshot.status === "managed") {
    return { allowed: true, mode: "normal", reason: "后端确认该流由媒体管理模块持有，可执行普通关闭。" };
  }
  return {
    allowed: false,
    mode: "blocked",
    reason: "后端判定该流受业务持有或归属未知，普通关闭受保护；如确需中断，请单流执行强制关闭。"
  };
}

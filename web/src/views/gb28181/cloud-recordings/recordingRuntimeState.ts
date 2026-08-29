import type {
  ZLMOwnershipSnapshot,
  ZLMOwnershipTarget,
  ZLMRecordingResult
} from "@/api/gb28181-zlm-runtime";

export interface RecordingTargetDraft {
  nodeId: number;
  schema: string;
  vhost: string;
  app: string;
  stream: string;
}

export interface RecordingTargetValidation {
  target: ZLMOwnershipTarget;
  errors: Record<string, string>;
}

const ownershipTypeLabels: Record<string, string> = {
  realtime_playback: "实时点播",
  device_playback: "设备回放",
  talk: "语音对讲",
  cascade: "级联转发",
  recording_plan: "录像计划",
  continuous_recording: "持续录像",
  recording_session: "录像会话",
  managed: "平台管理资源",
  unknown: "未知持有"
};

export function buildRecordingTarget(draft: RecordingTargetDraft): RecordingTargetValidation {
  const nodeId = Number(draft.nodeId);
  const media = {
    schema: draft.schema.trim(),
    vhost: draft.vhost.trim(),
    app: draft.app.trim(),
    stream: draft.stream.trim()
  };
  const errors: Record<string, string> = {};
  if (!Number.isSafeInteger(nodeId) || nodeId <= 0) errors.nodeId = "请选择媒体节点";
  if (!media.schema) errors.schema = "请输入 Schema";
  if (!media.vhost) errors.vhost = "请输入 VHost";
  if (!media.app) errors.app = "请输入 App";
  if (!media.stream) errors.stream = "请输入 Stream ID";
  return { target: { nodeId, media }, errors };
}

function safeText(value: string | undefined) {
  return value?.trim() || "未标识";
}

export function recordingImpactItems(snapshot: ZLMOwnershipSnapshot): string[] {
  const items: string[] = [];
  for (const owner of snapshot.owners ?? []) {
    const label = ownershipTypeLabels[owner.type] ?? owner.type;
    const subject = owner.owner || owner.key || owner.resourceType;
    items.push(`${label}：${safeText(subject)}（${owner.confidence === "proven" ? "已确认" : "待确认"}）`);
  }
  for (const impact of snapshot.impacts ?? []) {
    const subject = impact.resourceKey || impact.owner || impact.resourceType;
    const label = ownershipTypeLabels[impact.resourceType ?? ""] ?? safeText(impact.resourceType);
    items.push(`${label}：${safeText(subject)}${impact.reason ? ` · ${impact.reason}` : ""}`);
  }
  if (items.length === 0) {
    items.push(snapshot.presenceKnown
      ? "未发现录像计划、持续录像或其他业务持有"
      : "媒体存在性或业务持有未知，执行前必须重新确认");
  }
  return items;
}

export type RecordingStatusTone = "neutral" | "success" | "warning" | "danger";

export function recordingStatusPresentation(result: ZLMRecordingResult | null): {
  label: string;
  tone: RecordingStatusTone;
  readyToStopNormally: boolean;
} {
  if (!result) return { label: "尚未回读运行态", tone: "neutral", readyToStopNormally: false };
  if (result.rollbackUncertain) return { label: "回滚状态不确定，请重新回读", tone: "danger", readyToStopNormally: false };
  if (result.recording && result.state === "recording") {
    return { label: "手工录制中（归属已确认）", tone: "success", readyToStopNormally: true };
  }
  if (result.recording) {
    return { label: "ZLM 正在录制，手工归属未知", tone: "warning", readyToStopNormally: false };
  }
  if (result.state === "stopping") return { label: "停止待确认，可安全重试", tone: "warning", readyToStopNormally: true };
  if (result.state === "stopped" || result.externalState === "stopped") {
    return { label: "已停止", tone: "neutral", readyToStopNormally: false };
  }
  return { label: result.retryable ? "状态读取失败，可重试" : "未录制或归属未知", tone: result.retryable ? "danger" : "neutral", readyToStopNormally: false };
}

export function recordingScheduleQuery(target: ZLMOwnershipTarget) {
  return { nodeId: String(target.nodeId), stream: target.media.stream };
}

export function recordingTargetLabel(target: ZLMOwnershipTarget) {
  return `${target.media.schema}://${target.media.vhost}/${target.media.app}/${target.media.stream}`;
}

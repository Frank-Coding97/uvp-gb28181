import type {
  ZLMCapabilityState,
  ZLMFFmpegSourceCreateRequest,
  ZLMFFmpegURLView
} from "@/api/gb28181-zlm-ingress";

export interface FFmpegSourceFormState {
  templateKey: string;
  srcUrl: string;
  dstUrl: string;
  timeoutMs: string;
  enableHls: boolean;
  enableMp4: boolean;
}

const allowedSchemes = new Set(["http", "https", "rtmp", "rtmps", "rtsp", "rtsps", "srt", "udp", "tcp", "ws", "wss"]);
const unsafeURLCharacters = /[\s\u0000-\u001f\u007f;|`$(){}<>\\"']/;

function validURL(raw: string) {
  if (!raw || raw !== raw.trim() || unsafeURLCharacters.test(raw)) return false;
  try {
    const parsed = new URL(raw);
    return allowedSchemes.has(parsed.protocol.slice(0, -1).toLowerCase()) && Boolean(parsed.hostname);
  } catch {
    return false;
  }
}

export function buildFFmpegCreateRequest(form: FFmpegSourceFormState, templates: string[]) {
  const errors: Record<string, string> = {};
  const templateKey = form.templateKey.trim();
  if (!templateKey || !templates.includes(templateKey)) errors.templateKey = "请选择后端登记的 FFmpeg 模板";
  if (!validURL(form.srcUrl)) errors.srcUrl = "请输入受支持且不含命令字符的源地址";
  if (!validURL(form.dstUrl)) errors.dstUrl = "请输入受支持且不含命令字符的目标地址";
  const timeoutText = form.timeoutMs.trim();
  if (!/^\d+$/.test(timeoutText)) {
    errors.timeoutMs = "请输入 1 到 300000 的整数毫秒值";
  } else {
    const timeoutMs = Number(timeoutText);
    if (!Number.isSafeInteger(timeoutMs) || timeoutMs < 1 || timeoutMs > 300000) {
      errors.timeoutMs = "请输入 1 到 300000 的整数毫秒值";
    }
  }
  if (Object.keys(errors).length) return { errors };
  const request: ZLMFFmpegSourceCreateRequest = {
    templateKey,
    srcUrl: form.srcUrl,
    dstUrl: form.dstUrl,
    timeoutMs: Number(timeoutText),
    enableHls: form.enableHls,
    enableMp4: form.enableMp4
  };
  return { errors, request };
}

export function ffmpegCreateDecision(state: ZLMCapabilityState, templates: string[], hasPermission: boolean) {
  if (!hasPermission) return { allowed: false, reason: "缺少 FFmpeg 源管理权限。" };
  if (state === "unsupported") return { allowed: false, reason: "当前节点不支持 FFmpeg 管理能力。" };
  if (state === "unknown") return { allowed: false, reason: "当前节点的 FFmpeg 能力尚未探测，刷新后再试。" };
  if (templates.length === 0) return { allowed: false, reason: "后端尚未登记可用 FFmpeg 模板。" };
  return { allowed: true, reason: "可使用后端登记的类型化模板创建。" };
}

export function ffmpegURLText(view?: ZLMFFmpegURLView) {
  return view?.summary?.trim() || "—";
}

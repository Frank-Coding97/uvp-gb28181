import type {
  ZLMCapabilityState,
  ZLMRTPServerCreateRequest
} from "@/api/gb28181-zlm-ingress";

export interface RTPServerFormState {
  vhost: string;
  app: string;
  stream: string;
  port: string;
  tcpMode: string;
  ssrc: string;
  onlyTrack: string;
  localIp: string;
  reuse: boolean;
}

function integer(raw: string, min: number, max: number, blank?: number) {
  const text = raw.trim();
  if (!text && blank !== undefined) return { value: blank };
  if (!/^\d+$/.test(text)) return { error: `请输入 ${min} 到 ${max} 的整数` };
  const value = Number(text);
  return Number.isSafeInteger(value) && value >= min && value <= max
    ? { value }
    : { error: `请输入 ${min} 到 ${max} 的整数` };
}

function validIPv4(raw: string) {
  const parts = raw.split(".");
  return parts.length === 4 && parts.every(part => /^\d{1,3}$/.test(part) && Number(part) <= 255);
}

function validIPv6(raw: string) {
  if (!/^[0-9a-f:.]+$/i.test(raw) || raw.includes(":::")) return false;
  const compressed = raw.split("::");
  if (compressed.length > 2) return false;
  const tokens = compressed.flatMap(part => part ? part.split(":") : []);
  let groups = 0;
  for (const [index, token] of tokens.entries()) {
    if (token.includes(".")) {
      if (index !== tokens.length - 1 || !validIPv4(token)) return false;
      groups += 2;
    } else {
      if (!/^[0-9a-f]{1,4}$/i.test(token)) return false;
      groups += 1;
    }
  }
  return compressed.length === 2 ? groups < 8 : groups === 8;
}

function validLocalIP(raw: string) {
  if (!raw) return true;
  return raw.includes(":") ? validIPv6(raw) : validIPv4(raw);
}

export function buildRTPCreateRequest(form: RTPServerFormState) {
  const errors: Record<string, string> = {};
  const identity = { vhost: form.vhost.trim(), app: form.app.trim(), stream: form.stream.trim() };
  for (const [key, value] of Object.entries(identity)) {
    if (!value || /[\u0000\n\r\t]/.test(value)) errors[key] = "不能为空且不能包含控制字符";
  }
  const port = integer(form.port, 0, 65535, 0);
  const tcpMode = integer(form.tcpMode, 0, 2);
  const onlyTrack = integer(form.onlyTrack, 0, 2);
  if (port.error) errors.port = port.error;
  if (tcpMode.error) errors.tcpMode = tcpMode.error;
  if (onlyTrack.error) errors.onlyTrack = onlyTrack.error;
  const ssrc = form.ssrc.trim();
  if (ssrc && !/^\d{1,32}$/.test(ssrc)) errors.ssrc = "SSRC 只能包含 1 到 32 位十进制数字";
  const localIp = form.localIp.trim();
  if (!validLocalIP(localIp)) errors.localIp = "请输入有效 IP 地址";
  if (form.reuse && port.value === 0) errors.reuse = "端口复用要求填写明确端口";
  if (Object.keys(errors).length) return { errors };

  const request: ZLMRTPServerCreateRequest = {
    ...identity,
    port: port.value as number,
    tcpMode: tcpMode.value as number,
    onlyTrack: onlyTrack.value as number,
    reuse: form.reuse,
    ...(ssrc ? { ssrc } : {}),
    ...(localIp ? { localIp } : {})
  };
  return { errors, request };
}

export function rtpCloseDecision(
  server: Pick<{ managed: boolean; released: boolean }, "managed" | "released">,
  capability: ZLMCapabilityState,
  hasManagePermission: boolean,
  forceRequested: boolean
) {
  if (capability !== "supported") return { allowed: false, mode: "blocked" as const, reason: "节点能力未明确支持，关闭操作已禁用。" };
  if (server.released) return { allowed: false, mode: "absent" as const, reason: "RTP 服务已释放。" };
  if (!hasManagePermission) return { allowed: false, mode: "blocked" as const, reason: "缺少 RTP 管理权限。" };
  if (forceRequested) return { allowed: true, mode: "force" as const, reason: "将通过独立强制关闭接口执行。" };
  if (server.managed) return { allowed: true, mode: "normal" as const, reason: "管理台创建资源可执行普通关闭。" };
  return { allowed: false, mode: "blocked" as const, reason: "业务持有或归属未知的 RTP 服务禁止普通关闭。" };
}

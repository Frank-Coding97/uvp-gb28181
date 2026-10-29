import type { CreateZLMNodeReq, UpdateZLMNodeReq, ZLMNode } from "@/api/gb28181-zlm";
import { validateOptionalHookIp, validateOptionalSdpIp, validateOptionalStreamIp } from "../sip/mediaNetworkAddress";

export interface NodeFormState {
  name: string;
  host: string;
  hookIp: string;
  sdpIp: string;
  playbackHost: string;
  apiPort: string;
  apiSecret: string;
  weight: string;
  rtpReceiveMode: "single" | "multi";
  rtpProxyPort: string;
  rtpPortStart: string;
  rtpPortEnd: string;
}

export type NodeFormErrors = Partial<Record<keyof NodeFormState, string>>;

export function createNodeFormState(node?: ZLMNode | null): NodeFormState {
  return {
    name: node?.name ?? "",
    host: node?.host ?? "",
    hookIp: node?.hookIp ?? "",
    sdpIp: node?.sdpIp ?? "",
    playbackHost: node?.playbackHost ?? "",
    apiPort: node ? String(node.apiPort) : "",
    apiSecret: "",
    weight: String(node?.weight ?? 50),
    rtpReceiveMode: node?.rtpReceiveMode ?? "multi",
    rtpProxyPort: "",
    rtpPortStart: String(node?.rtpPortStart ?? 30000),
    rtpPortEnd: String(node?.rtpPortEnd ?? 35000)
  };
}

/**
 * 生成「留空则用哪个地址」的 placeholder。
 *
 * 网络地址这三项是**节点级覆盖**：节点留空 ≠ 没配置，而是跟随平台默认
 * （引导页配的那套）。可输入框只会显示节点自己的值，所以引导页填过、这里却是
 * 一片空白 —— 看起来就像「配了没保存 / 没回显」(2026-10-20 现场反馈)。
 *
 * ⛔ 只作 placeholder，绝不回填进 `form`：回填后一保存，「跟随平台默认」就变成
 * 「写死在这个节点上」，之后再改引导页对这台节点不再生效。
 */
export function inheritedPlaceholder(current: string, effective: string | undefined, fallback: string): string {
  const value = (effective ?? "").trim();
  if (current.trim() || !value) return fallback;
  return `留空则使用：${value}`;
}

/**
 * 节点名/别名长度上限，与后端 `meta_node.name` 的 varchar(64) 对齐。
 * ⛔ 按**字符数**算（`[...str].length` 是 code point，与 Go 的 `utf8.RuneCountInString` 同口径），
 * 别用 `str.length` 之外再套字节数 —— 那会让正常中文别名被误拒。
 */
export const NODE_NAME_MAX_LENGTH = 64;

function integerError(value: string, label: string, min: number, max: number) {
  const trimmed = value.trim();
  if (!/^\d+$/.test(trimmed)) return `${label}必须是整数`;
  const parsed = Number(trimmed);
  if (!Number.isSafeInteger(parsed) || parsed < min || parsed > max) return `${label}范围为 ${min}-${max}`;
  return "";
}

/**
 * 解析 ZLM `[rtp_proxy] port_range` 的原始串（形如 `51014-51063`）。
 *
 * ⛔ 判据必须与后端 `node.ParseRTPPortRange` 保持在**同一套**上，否则会出现
 * 「界面上读得出来、后端解析不了」这类只有真机才暴露的静默漂移。
 * 所以这里**不 trim、不宽松**：两侧都必须是非空纯数字，且 1 ≤ start ≤ end ≤ 65535，
 * 与后端逐条对齐（后端也不做 trim）。
 * ✅ 解析不出来一律返回 `null` —— 调用方必须回落到已登记的端口值，
 * 绝不能把 0 / NaN 当端口往表单里塞。
 */
export function parseRTPPortRange(value?: string): { start: number; end: number } | null {
  if (!value) return null;
  const match = /^(\d+)-(\d+)$/.exec(value);
  if (!match) return null;
  const start = Number(match[1]);
  const end = Number(match[2]);
  if (!Number.isSafeInteger(start) || !Number.isSafeInteger(end)) return null;
  if (start < 1 || end > 65535 || start > end) return null;
  return { start, end };
}

export function validateNodeForm(form: NodeFormState, editing: boolean): NodeFormErrors {
  const errors: NodeFormErrors = {};
  if (editing && !form.name.trim()) errors.name = "请输入节点名";
  else if ([...form.name.trim()].length > NODE_NAME_MAX_LENGTH) errors.name = `节点名最长 ${NODE_NAME_MAX_LENGTH} 个字符`;
  if (!form.host.trim()) errors.host = "请输入后端可访问的管理地址";
  if (!editing && !form.apiSecret.trim()) errors.apiSecret = "新建节点必须填写 API Secret";

  const apiPortError = integerError(form.apiPort, "API 端口", 1, 65535);
  const weightError = integerError(form.weight, "权重", 0, 100);
  const startError = integerError(form.rtpPortStart, "RTP 起始端口", 1024, 65535);
  const endError = integerError(form.rtpPortEnd, "RTP 结束端口", 1024, 65535);
  if (apiPortError) errors.apiPort = apiPortError;
  if (weightError) errors.weight = weightError;
  if (form.rtpReceiveMode === "single") {
    const portError = integerError(form.rtpProxyPort, "RTP 单端口", 1024, 65534);
    if (portError) errors.rtpProxyPort = portError;
  } else {
    if (startError) errors.rtpPortStart = startError;
    if (endError) errors.rtpPortEnd = endError;
  }
  const hookIpError = validateOptionalHookIp(form.hookIp);
  if (hookIpError) errors.hookIp = hookIpError;
  // 节点级 sdp_ip 留空时回落到平台默认，所以这里只在填了值时校验格式；
  // 一旦填了回环地址就必须拦下 —— 那会让该节点的点播永远没有画面。
  if (form.sdpIp.trim()) {
    const sdpIpError = validateOptionalSdpIp(form.sdpIp);
    if (sdpIpError) errors.sdpIp = sdpIpError;
  }
  const streamIpError = validateOptionalStreamIp(form.playbackHost);
  if (streamIpError) errors.playbackHost = streamIpError;
  if (form.rtpReceiveMode === "multi" && !startError && !endError && Number(form.rtpPortEnd) <= Number(form.rtpPortStart)) {
    errors.rtpPortEnd = "RTP 结束端口必须大于起始端口";
  }
  return errors;
}

export function buildNodeRequest(form: NodeFormState, editing: boolean): CreateZLMNodeReq | UpdateZLMNodeReq {
  const request: CreateZLMNodeReq = {
    host: form.host.trim(),
    hookIp: form.hookIp.trim(),
    sdpIp: form.sdpIp.trim(),
    playbackHost: form.playbackHost.trim(),
    apiPort: Number(form.apiPort),
    apiSecret: form.apiSecret,
    weight: Number(form.weight),
    rtpReceiveMode: form.rtpReceiveMode,
    ...(form.rtpReceiveMode === "single"
      ? { rtpProxyPort: Number(form.rtpProxyPort) }
      : { rtpPortStart: Number(form.rtpPortStart), rtpPortEnd: Number(form.rtpPortEnd) })
  };
  // 节点名（对外显示为「节点别名」）。**新建也允许填**：留空时后端会用
  // `host:port` 兜底（`service.buildCreateCandidate`），所以这里只在非空时带上，
  // 目的是让「不填」继续走同一条兜底路径，而不是把空串写进库。
  const name = form.name.trim();
  if (name) request.name = name;
  if (editing && !request.apiSecret) {
    const update: UpdateZLMNodeReq = { ...request };
    delete update.apiSecret;
    return update;
  }
  return request;
}

export function clearNodeSecret(form: NodeFormState): NodeFormState {
  return { ...form, apiSecret: "" };
}

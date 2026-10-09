import type { CreateZLMNodeReq, UpdateZLMNodeReq, ZLMNode } from "@/api/gb28181-zlm";
import { validateOptionalHookIp, validateOptionalStreamIp, validateRequiredSdpIp } from "../sip/mediaNetworkAddress";

export interface NodeFormState {
  name: string;
  host: string;
  hookIp: string;
  receiveHost: string;
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
    receiveHost: node?.receiveHost ?? "",
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

function integerError(value: string, label: string, min: number, max: number) {
  const trimmed = value.trim();
  if (!/^\d+$/.test(trimmed)) return `${label}必须是整数`;
  const parsed = Number(trimmed);
  if (!Number.isSafeInteger(parsed) || parsed < min || parsed > max) return `${label}范围为 ${min}-${max}`;
  return "";
}

export function validateNodeForm(form: NodeFormState, editing: boolean): NodeFormErrors {
  const errors: NodeFormErrors = {};
  if (editing && !form.name.trim()) errors.name = "请输入节点名";
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
    const sdpIpError = validateRequiredSdpIp(form.sdpIp);
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
    receiveHost: form.receiveHost.trim(),
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
  if (editing) request.name = form.name.trim();
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

import type { CreateZLMNodeReq, UpdateZLMNodeReq, ZLMNode } from "@/api/gb28181-zlm";

export interface NodeFormState {
  name: string;
  host: string;
  receiveHost: string;
  playbackHost: string;
  apiPort: string;
  apiSecret: string;
  weight: string;
  rtpPortStart: string;
  rtpPortEnd: string;
}

export type NodeFormErrors = Partial<Record<keyof NodeFormState, string>>;

export function createNodeFormState(node?: ZLMNode | null): NodeFormState {
  return {
    name: node?.name ?? "",
    host: node?.host ?? "",
    receiveHost: node?.receiveHost ?? "",
    playbackHost: node?.playbackHost ?? "",
    apiPort: String(node?.apiPort ?? 18080),
    apiSecret: "",
    weight: String(node?.weight ?? 50),
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
  if (!form.name.trim()) errors.name = "请输入节点名";
  if (!form.host.trim()) errors.host = "请输入后端可访问的管理地址";
  if (!editing && !form.apiSecret.trim()) errors.apiSecret = "新建节点必须填写 API Secret";

  const apiPortError = integerError(form.apiPort, "API 端口", 1, 65535);
  const weightError = integerError(form.weight, "权重", 0, 100);
  const startError = integerError(form.rtpPortStart, "RTP 起始端口", 1024, 65535);
  const endError = integerError(form.rtpPortEnd, "RTP 结束端口", 1024, 65535);
  if (apiPortError) errors.apiPort = apiPortError;
  if (weightError) errors.weight = weightError;
  if (startError) errors.rtpPortStart = startError;
  if (endError) errors.rtpPortEnd = endError;
  if (!startError && !endError && Number(form.rtpPortEnd) < Number(form.rtpPortStart)) {
    errors.rtpPortEnd = "RTP 结束端口不能小于起始端口";
  }
  return errors;
}

export function buildNodeRequest(form: NodeFormState, editing: boolean): CreateZLMNodeReq | UpdateZLMNodeReq {
  const request: CreateZLMNodeReq = {
    name: form.name.trim(),
    host: form.host.trim(),
    receiveHost: form.receiveHost.trim(),
    playbackHost: form.playbackHost.trim(),
    apiPort: Number(form.apiPort),
    apiSecret: form.apiSecret,
    weight: Number(form.weight),
    rtpPortStart: Number(form.rtpPortStart),
    rtpPortEnd: Number(form.rtpPortEnd)
  };
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

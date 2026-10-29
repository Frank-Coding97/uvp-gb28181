import type { SipDeploymentMode, SipNetworkAddress } from "@/api/gb28181";

function isConcreteIPv4(value: string): boolean {
  const parts = value.split(".");
  if (parts.length !== 4 || value === "0.0.0.0") return false;
  const octets = parts.map(Number);
  return (
    parts.every((part, index) => /^\d{1,3}$/.test(part) && octets[index] <= 255 && String(octets[index]) === part) &&
    (octets[0] < 224 || octets[0] > 239)
  );
}

function isConcreteIPv6(value: string): boolean {
  if (!value.includes(":") || !/^[\da-f:.]+$/i.test(value)) return false;
  try {
    const hostname = new URL(`http://[${value}]/`).hostname.toLowerCase();
    return hostname !== "[::]" && !hostname.startsWith("[ff");
  } catch {
    return false;
  }
}

export function isConcreteIp(value: string): boolean {
  return isConcreteIPv4(value) || isConcreteIPv6(value);
}

function isDnsHostname(value: string): boolean {
  if (!value || value.length > 253 || /^\d+$/.test(value.replace(/\./g, ""))) return false;
  const hostname = value.endsWith(".") ? value.slice(0, -1) : value;
  if (!hostname) return false;
  return hostname.split(".").every(label => label.length <= 63 && /^[a-z\d](?:[a-z\d-]*[a-z\d])?$/i.test(label));
}

function isLoopbackAddress(value: string): boolean {
  if (value === "::1") return true;
  const octets = value.split(".");
  if (octets.length !== 4) return false;
  return octets[0] === "127";
}

// ---------------------------------------------------------------------------
// 填写规则：由「部署模式 + 当前选中的监听网卡」推导，而不是写死。
//
// 三条规则（老板 2026-10-11 定）：
//   ① 选中**虚拟网卡**或 **0.0.0.0** ⇒ 三个地址都必须手填
//      （虚拟网卡地址对设备侧不可达；0.0.0.0 推断不出唯一地址）
//   ② 选中**具体真实网卡** ⇒ 三个地址可留空，留空即跟随该网卡
//   ③ 公网模式另算：SDP IP / Stream IP 跟随「公网接入 IP」（设备与浏览器在公网侧），
//      Hook IP 仍走监听网卡（那是 ZLM 回调平台，不出本机）
//
// ⛔ 后端 `app/gb28181/setup/media_address_policy.go` 是同一套规则的镜像实现，
//    两边注释互相钉住；改规则必须同时改两处，并跑对应的用例。
// ---------------------------------------------------------------------------

/** 单个媒体地址字段的填写规则。 */
export interface MediaAddressRule {
  /** 是否允许留空。false 时留空会报错（推断不出可用的兜底地址）。 */
  optional: boolean;
  /** 留空时实际生效的地址；`optional` 为 true 时非空，用于 placeholder 与提示文案。 */
  fallback: string;
}

export interface MediaAddressPolicy {
  hookIp: MediaAddressRule;
  sdpIp: MediaAddressRule;
  streamIp: MediaAddressRule;
}

/** 推断不出兜底地址 ⇒ 必须手填。 */
const REQUIRED_RULE: MediaAddressRule = { optional: false, fallback: "" };
/** 只校验格式、允许留空（媒体节点的「覆盖」字段用它：留空 = 继承平台默认）。 */
const FORMAT_ONLY_RULE: MediaAddressRule = { optional: true, fallback: "" };

function ruleFor(fallback: string): MediaAddressRule {
  return fallback ? { optional: true, fallback } : REQUIRED_RULE;
}

export interface MediaAddressPolicyInput {
  deploymentMode: SipDeploymentMode | "";
  /** 当前选中的监听地址（可能是 0.0.0.0）。 */
  listenIp: string;
  /** 公网模式下手填的对外可达地址；局域网模式下等于 listenIp。 */
  advertiseIp: string;
  /** 网卡列表，用于判断选中的是不是虚拟网卡（缺省时按「非虚拟」处理）。 */
  networkItems?: SipNetworkAddress[];
}

export function isVirtualListenIp(listenIp: string, networkItems?: SipNetworkAddress[]): boolean {
  return (networkItems || []).some(item => item.ip === listenIp && item.virtual);
}

/**
 * 推导三个媒体地址的填写规则。
 *
 * 兜底地址的可用性判据（两类都不能用）：
 *   - 不是「具体地址」：0.0.0.0 / 空 / 非法格式 —— 推断不出唯一地址；
 *   - 落在**虚拟网卡**上：那个地址设备侧不可达，托底等于埋雷。
 */
export function resolveMediaAddressPolicy(input: MediaAddressPolicyInput): MediaAddressPolicy {
  const listenFallback =
    isConcreteIp(input.listenIp) && !isVirtualListenIp(input.listenIp, input.networkItems) ? input.listenIp : "";
  const publicFallback = isConcreteIp(input.advertiseIp) ? input.advertiseIp : "";

  if (input.deploymentMode === "public") {
    return {
      // Hook IP 是 ZLM 回调平台用的，不出本机 ⇒ 跟监听网卡走，与设备侧无关。
      hookIp: ruleFor(listenFallback),
      // SDP / Stream 是设备与浏览器要访问的地址 ⇒ 公网模式下只有对外地址可达。
      sdpIp: ruleFor(publicFallback),
      streamIp: ruleFor(publicFallback)
    };
  }

  // 局域网：三者都跟随选中的网卡（选 0.0.0.0 时 listener 无法唯一化 ⇒ 三项必填）。
  return {
    hookIp: ruleFor(listenFallback),
    sdpIp: ruleFor(listenFallback),
    streamIp: ruleFor(listenFallback)
  };
}

// ---------------------------------------------------------------------------
// 校验
// ---------------------------------------------------------------------------

/** Hook IP（界面名「平台回调地址」）：具体 IP（不含域名），允许留空（见规则）。 */
export function validateHookIp(value: string, rule: MediaAddressRule): string {
  const address = value.trim();
  if (!address) return rule.optional ? "" : "请填写平台回调地址：当前监听网卡无法自动推断，需手填流媒体服务器能回调到的地址";
  return isConcreteIp(address) ? "" : "请输入具体的 IPv4 或 IPv6 地址";
}

/** Stream IP（界面名「播放地址」）：IP 或域名，不含协议/端口/路径。 */
export function validateStreamIp(value: string, rule: MediaAddressRule): string {
  const address = value.trim();
  if (!address) return rule.optional ? "" : "请填写播放地址：当前监听网卡无法自动推断，需手填浏览器能访问到的地址";
  return isConcreteIp(address) || isDnsHostname(address) ? "" : "请输入具体 IP 地址或域名，不含协议、端口和路径";
}

/**
 * SDP IP（界面名「设备推流地址」）—— 设备按这个地址把 RTP 流推回来。
 *
 * 为什么规则比 Stream IP 严：留空或填成回环地址时，故障表征是
 * 「信令全成功但没有画面」(INVITE 被接受、SDP 正常协商，10 秒后 play_timeout)，
 * 现场完全看不出是配置问题。所以非空值一律从严；只有「推断得出兜底地址」时才允许留空。
 */
export function validateSdpIp(value: string, rule: MediaAddressRule): string {
  // 容错 undefined：浏览器里可能残留着旧版本持久化的表单结构，
  // 缺字段时按「未填写」处理，绝不能把整个引导页渲染崩掉。
  const address = (value ?? "").trim();
  if (!address) {
    return rule.optional ? "" : "请填写设备推流地址：设备按此地址把视频流推回平台，不填则无法点播";
  }
  if (isLoopbackAddress(address) || address === "::") {
    return "不能填 127.0.0.1 或 0.0.0.0：设备会把流推向平台自身，导致注册成功但没有画面";
  }
  if (isConcreteIp(address)) return "";
  // 单一标签主机名(如 Docker 容器名 polaris-media)在设备侧无法解析。
  if (!address.includes(".")) {
    return "不能是单一主机名（如容器名）：设备无法解析，请填设备能访问到的 IP 或域名";
  }
  return isDnsHostname(address) ? "" : "请输入具体 IP 地址或域名，不含协议、端口和路径";
}

/** 媒体节点「覆盖」字段用：留空表示继承平台默认，只校验填了值的格式。 */
export function validateOptionalHookIp(value: string): string {
  return validateHookIp(value, FORMAT_ONLY_RULE);
}

export function validateOptionalStreamIp(value: string): string {
  return validateStreamIp(value, FORMAT_ONLY_RULE);
}

export function validateOptionalSdpIp(value: string): string {
  return validateSdpIp(value, FORMAT_ONLY_RULE);
}

export function mediaAddressesCanContinue(policy: MediaAddressPolicy, hookIp: string, sdpIp: string, streamIp: string): boolean {
  return (
    !validateHookIp(hookIp, policy.hookIp) && !validateSdpIp(sdpIp, policy.sdpIp) && !validateStreamIp(streamIp, policy.streamIp)
  );
}

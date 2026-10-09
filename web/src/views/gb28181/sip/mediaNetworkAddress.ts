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

export function validateOptionalHookIp(value: string): string {
  const address = value.trim();
  return address && !isConcreteIp(address) ? "请输入具体的 IPv4 或 IPv6 地址" : "";
}

export function validateOptionalStreamIp(value: string): string {
  const address = value.trim();
  return address && !isConcreteIp(address) && !isDnsHostname(address) ? "请输入具体 IP 地址或域名，不含协议、端口和路径" : "";
}

function isLoopbackAddress(value: string): boolean {
  if (value === "::1") return true;
  const octets = value.split(".");
  if (octets.length !== 4) return false;
  return octets[0] === "127";
}

/**
 * 校验必填的 SDP IP —— 设备按这个地址回推 RTP 流。
 *
 * 为什么规则比 Stream IP 严：留空或填成回环地址时，故障表征是
 * 「信令全成功但没有画面」(INVITE 被接受、SDP 正常协商，10 秒后 play_timeout)，
 * 现场完全看不出是配置问题。所以宁可当场拦住，也不放过去。
 */
export function validateRequiredSdpIp(value: string | undefined): string {
  // 容错undefined：浏览器里可能残留着旧版本持久化的表单结构，
  // 缺字段时按"未填写"处理，绝不能把整个引导页渲染崩掉。
  const address = (value ?? "").trim();
  if (!address) return "请填写 SDP IP：设备按此地址把视频流推回平台，不填则无法点播";
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

export function mediaNetworkAddressesCanContinue(hookIp: string, sdpIp: string, streamIp: string): boolean {
  return !validateOptionalHookIp(hookIp) && !validateRequiredSdpIp(sdpIp) && !validateOptionalStreamIp(streamIp);
}

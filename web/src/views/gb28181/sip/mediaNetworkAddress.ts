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

export function mediaNetworkAddressesCanContinue(hookIp: string, streamIp: string): boolean {
  return !validateOptionalHookIp(hookIp) && !validateOptionalStreamIp(streamIp);
}

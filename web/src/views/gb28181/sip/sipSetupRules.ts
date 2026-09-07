import type { SipDeploymentMode, SipNetworkAddress, SipNetworkInterfaces } from "@/api/gb28181";

export function isConcreteIPv4(value: string): boolean {
    const parts = value.split(".");
    if (parts.length !== 4 || value === "0.0.0.0" || value.startsWith("127.")) return false;
    return parts.every(part => /^\d{1,3}$/.test(part) && Number(part) >= 0 && Number(part) <= 255 && String(Number(part)) === part);
}

// Media endpoints may intentionally use loopback during local verification;
// unspecified, multicast, reserved and broadcast ranges are not concrete hosts.
export function isMediaIPv4(value: string): boolean {
    const parts = value.split(".");
    if (parts.length !== 4 || value === "0.0.0.0") return false;
    if (!parts.every(part => /^\d{1,3}$/.test(part) && Number(part) >= 0 && Number(part) <= 255 && String(Number(part)) === part)) {
        return false;
    }
    const firstOctet = Number(parts[0]);
    return firstOctet > 0 && firstOctet < 224;
}

export function validateMediaHost(value: string, label: string): string {
    if (!value) return `${label}必须填写具体 IPv4 地址`;
    if (!isMediaIPv4(value)) return `${label}必须是具体 IPv4 地址，不允许 0.0.0.0、组播或广播地址`;
    return "";
}

export function mediaHostsCanContinue(required: boolean, receiveHost: string, playbackHost: string): boolean {
    if (!required) return true;
    return !validateMediaHost(receiveHost, "媒体接收地址") && !validateMediaHost(playbackHost, "媒体播放地址");
}

export function deriveNetworkSelection(
    mode: SipDeploymentMode,
    listenIp: string,
    advertiseIp: string,
    items: SipNetworkAddress[]
): { listenIp: string; advertiseIp: string; advertiseIpInferred: boolean } {
    void items;
    if (mode === "public") return { listenIp, advertiseIp, advertiseIpInferred: false };
    if (listenIp !== "0.0.0.0") return { listenIp, advertiseIp: listenIp, advertiseIpInferred: false };
    return { listenIp, advertiseIp: "", advertiseIpInferred: false };
}

export function networkCanContinue(mode: SipDeploymentMode | "", listenIp: string, advertiseIp: string): boolean {
    if (!mode) return false;
    const listenValid = listenIp === "0.0.0.0" || isConcreteIPv4(listenIp);
    if (!listenValid) return false;
    if (mode === "lan") return true;
    return isConcreteIPv4(advertiseIp);
}

export function networkOptions(items: SipNetworkAddress[], currentIp: string): Array<SipNetworkAddress & { unavailable?: boolean }> {
    if (!isConcreteIPv4(currentIp) || items.some(item => item.ip === currentIp)) return items;
    return [
        ...items,
        {
            ip: currentIp,
            interfaceName: "已保存地址（当前不可用）",
            cidr: "",
            loopback: false,
            virtual: false,
            recommended: false,
            more: false,
            listenOnly: false,
            unavailable: true
        }
    ];
}

export type SipAddressAvailability = "ok" | "missing" | "unavailable";

function sipAddressTargets(mode: SipDeploymentMode | "", listenIp: string, advertiseIp: string): string[] {
    if (mode === "public") return isConcreteIPv4(listenIp) ? [listenIp] : [];
    if (mode !== "lan") return [];
    const targets: string[] = [];
    if (isConcreteIPv4(listenIp)) targets.push(listenIp);
    if (isConcreteIPv4(advertiseIp) && !targets.includes(advertiseIp)) targets.push(advertiseIp);
    return targets;
}

export function sipAddressAvailability(
    mode: SipDeploymentMode | "",
    listenIp: string,
    advertiseIp: string,
    network: SipNetworkInterfaces | null
): SipAddressAvailability {
    if (!network || network.scanStatus !== "ok") return "unavailable";
    const targets = sipAddressTargets(mode, listenIp, advertiseIp);
    if (!targets.length) return "ok";
    const available = new Set(
        network.items
            .filter(item => !item.loopback && !item.listenOnly && isConcreteIPv4(item.ip))
            .map(item => item.ip)
    );
    return targets.every(ip => available.has(ip)) ? "ok" : "missing";
}

export function activeSipAddresses(
    mode: SipDeploymentMode | "",
    listenIp: string,
    advertiseIp: string,
    items: SipNetworkAddress[]
): string[] {
    if (mode === "public") return isConcreteIPv4(advertiseIp) ? [advertiseIp] : [];
    if (listenIp !== "0.0.0.0") return isConcreteIPv4(listenIp) ? [listenIp] : [];

    const addresses: string[] = [];
    const seen = new Set<string>();
    for (const item of items) {
        if (item.loopback || item.listenOnly || !isConcreteIPv4(item.ip) || seen.has(item.ip)) continue;
        seen.add(item.ip);
        addresses.push(item.ip);
    }
    return addresses;
}

export function deriveDomain(serverId: string): string {
    return /^\d{10,}$/.test(serverId) ? serverId.slice(0, 10) : "";
}

// ===== 密码强度 =====
// 规则跟后端 validator.go checkPasswordStrength 一致:
//   - 长度 >= 12
//   - 至少含大写/小写/数字/特殊字符中的 3 类
//   - 不在常见弱口令黑名单
//   - 不为纯顺序序列
//
// GB28181 SIP 注册暴力破解频发,弱口令是主要入口.

const WEAK_PASSWORDS = new Set([
    "12345678", "123456789", "1234567890", "12345678901", "123456789012",
    "password", "passw0rd", "password123",
    "qwerty", "qwerty123",
    "admin", "admin123", "admin1234", "administrator",
    "root", "root123",
    "sipserver", "gb28181",
    "12345678a", "abc12345",
    "111111111111", "000000000000", "aaaaaaaaaaaa", "iloveyou"
]);

function isSequential(password: string): boolean {
    if (password.length < 12) return false;
    let ascending = true;
    let descending = true;
    for (let i = 1; i < password.length; i++) {
        if (password.charCodeAt(i) !== password.charCodeAt(i - 1) + 1) ascending = false;
        if (password.charCodeAt(i) !== password.charCodeAt(i - 1) - 1) descending = false;
        if (!ascending && !descending) return false;
    }
    return true;
}

export interface PasswordStrength {
    // level: 0=空/太弱,1=弱,2=中,3=强.只有 3 允许通过校验.
    level: 0 | 1 | 2 | 3;
    label: string;
    reason: string; // 空字符串表示合规
}

export function evaluatePasswordStrength(password: string): PasswordStrength {
    if (!password) return { level: 0, label: "未填写", reason: "长度至少 12 位" };
    if (password.length < 12) return { level: 1, label: "太短", reason: "长度至少 12 位" };

    let hasUpper = false, hasLower = false, hasDigit = false, hasSpecial = false;
    for (const ch of password) {
        const code = ch.charCodeAt(0);
        if (code >= 65 && code <= 90) hasUpper = true;
        else if (code >= 97 && code <= 122) hasLower = true;
        else if (code >= 48 && code <= 57) hasDigit = true;
        else if (code > 32 && code < 127) hasSpecial = true;
    }
    const categories = [hasUpper, hasLower, hasDigit, hasSpecial].filter(Boolean).length;

    if (categories < 3) {
        return { level: 1, label: "弱", reason: "需含大写字母、小写字母、数字、特殊字符中至少 3 类" };
    }
    if (WEAK_PASSWORDS.has(password)) {
        return { level: 1, label: "弱", reason: "该密码在常见弱口令列表中,易被暴力破解" };
    }
    if (isSequential(password)) {
        return { level: 1, label: "弱", reason: "密码过于规律,请使用更随机的组合" };
    }
    // 满足所有强规则:进一步区分强/中,给用户视觉反馈.
    // 达到 4 类字符 + 长度 >=16 判"强";否则判"中"(依然合规,只是可以更好).
    if (categories === 4 && password.length >= 16) {
        return { level: 3, label: "强", reason: "" };
    }
    return { level: 2, label: "合格", reason: "" };
}

// 密码是否满足最低合规(能保存).合规 = level >= 2.
// 编辑场景下若用户未改密码(密码为空且已存在密码),则视为通过.
export function passwordAcceptable(password: string, hasExistingPassword: boolean): boolean {
    if (!password) return hasExistingPassword;
    const s = evaluatePasswordStrength(password);
    return s.level >= 2;
}

// ===== 字段级校验 =====
export interface FieldErrors {
    port?: string;
    serverId?: string;
    listenIp?: string;
    advertiseIp?: string;
    password?: string;
}

export function validateServerId(value: string): string {
    if (!value) return "必填";
    if (!/^\d{20}$/.test(value)) return "必须为 20 位数字";
    return "";
}

export function validatePort(value: number): string {
    if (!value || value < 1 || value > 65535) return "端口须在 1 - 65535 之间";
    return "";
}

export function validateAdvertiseIp(value: string, mode: SipDeploymentMode | ""): string {
    if (!value) return mode === "public" ? "请填写公网 IPv4 地址" : "请选择接入网卡";
    if (!isConcreteIPv4(value)) return "必须是合法的 IPv4 地址,不允许 0.0.0.0 或回环";
    return "";
}

export function identityCanContinue(
    port: number,
    serverId: string,
    domain: string,
    password: string,
    hasExistingPassword: boolean
): boolean {
    return port >= 1 && port <= 65535 && /^\d{20}$/.test(serverId) && /^\d{10}$/.test(domain) &&
        passwordAcceptable(password, hasExistingPassword);
}

export function formatRegisterUri(serverId: string, advertiseIp: string, port: number): string {
    return serverId && isConcreteIPv4(advertiseIp) && port >= 1 && port <= 65535
        ? `sip:${serverId}@${advertiseIp}:${port}`
        : "";
}

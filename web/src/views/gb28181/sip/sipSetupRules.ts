import type { SipDeploymentMode, SipNetworkAddress } from "@/api/gb28181";

export function isConcreteIPv4(value: string): boolean {
    const parts = value.split(".");
    if (parts.length !== 4 || value === "0.0.0.0" || value.startsWith("127.")) return false;
    return parts.every(part => /^\d{1,3}$/.test(part) && Number(part) >= 0 && Number(part) <= 255 && String(Number(part)) === part);
}

export function deriveNetworkSelection(
    mode: SipDeploymentMode,
    listenIp: string,
    advertiseIp: string,
    items: SipNetworkAddress[]
): { listenIp: string; advertiseIp: string; advertiseIpInferred: boolean } {
    if (mode === "public") return { listenIp, advertiseIp, advertiseIpInferred: false };
    if (listenIp !== "0.0.0.0") return { listenIp, advertiseIp: listenIp, advertiseIpInferred: false };
    if (isConcreteIPv4(advertiseIp)) return { listenIp, advertiseIp, advertiseIpInferred: false };
    const recommended = items.find(item => item.recommended && isConcreteIPv4(item.ip));
    return {
        listenIp,
        advertiseIp: recommended?.ip || "",
        advertiseIpInferred: Boolean(recommended)
    };
}

export function networkCanContinue(mode: SipDeploymentMode | "", listenIp: string, advertiseIp: string): boolean {
    if (!mode) return false;
    const listenValid = listenIp === "0.0.0.0" || isConcreteIPv4(listenIp);
    return listenValid && isConcreteIPv4(advertiseIp);
}

export function networkOptions(items: SipNetworkAddress[], currentIp: string): Array<SipNetworkAddress & { unavailable?: boolean }> {
    if (!currentIp || items.some(item => item.ip === currentIp)) return items;
    return [{
        ip: currentIp,
        interfaceName: "当前配置（不可用）",
        cidr: "",
        loopback: false,
        virtual: false,
        recommended: false,
        more: true,
        listenOnly: false,
        unavailable: true
    }, ...items];
}

export function deriveDomain(serverId: string): string {
    return /^\d{10,}$/.test(serverId) ? serverId.slice(0, 10) : "";
}

export function identityCanContinue(
    port: number,
    serverId: string,
    domain: string,
    password: string,
    hasExistingPassword: boolean
): boolean {
    return port >= 1 && port <= 65535 && /^\d{20}$/.test(serverId) && /^\d{10}$/.test(domain) &&
        (hasExistingPassword ? password === "" || password.length >= 6 : password.length >= 6);
}

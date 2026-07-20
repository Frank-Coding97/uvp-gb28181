import type { SipRuntimeState } from "@/api/gb28181";

export function runtimeLabel(state: SipRuntimeState): string {
    return {
        disabled: "未启用",
        unconfigured: "未配置",
        starting: "启动中",
        running: "运行中",
        failed: "启动失败",
        restart_required: "待重启"
    }[state];
}

export function runtimeColor(state: SipRuntimeState): string {
    if (state === "running") return "green";
    if (state === "failed") return "red";
    if (state === "restart_required" || state === "starting") return "orange";
    return "gray";
}

export function mayEditSipConfig(canConfigure: boolean, permissions: string[]): boolean {
    return canConfigure && (permissions.includes("*:*:*") || permissions.includes("gb28181:sip:config:update"));
}

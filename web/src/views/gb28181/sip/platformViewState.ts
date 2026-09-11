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

// 2026-07-20 起 canConfigure 语义废弃(后端硬编码 true 无意义),SIP 配置权限只看 permissions.
export function mayEditSipConfig(permissions: string[]): boolean {
    return permissions.includes("*:*:*") || permissions.includes("gb28181:sip:config:update");
}

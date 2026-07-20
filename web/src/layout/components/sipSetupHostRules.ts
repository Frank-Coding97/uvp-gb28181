// SIP 引导相关的权限判定 helper.
// 2026-07-20 硬闸重构后,是否弹窗 / 是否显示铃铛全部由 store.needsAttention + store.shouldAutoOpen 决定;
// 本文件只保留纯权限判定,和一个供测试用的 shouldOpenSipSetup 组合函数.
import type { SipSetupStatus } from "@/api/gb28181";

export function hasSipUpdatePermission(permissions: string[]): boolean {
    return permissions.includes("*:*:*") || permissions.includes("gb28181:sip:config:update");
}

// 组合判定:未配置/失败 + 有权限 → 应该打开引导.
// 主要供测试引用,生产代码通过 useSipSetupStore().shouldAutoOpen 调用.
export function shouldOpenSipSetup(status: SipSetupStatus, permissions: string[]): boolean {
    if (!hasSipUpdatePermission(permissions)) return false;
    const state = status.runtime?.state;
    return state === "unconfigured" || state === "failed";
}

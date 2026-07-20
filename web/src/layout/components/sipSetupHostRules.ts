import type { SipSetupStatus } from "@/api/gb28181";

export function hasSipUpdatePermission(permissions: string[]): boolean {
    return permissions.includes("*:*:*") || permissions.includes("gb28181:sip:config:update");
}

export function shouldOpenSipSetup(status: SipSetupStatus, permissions: string[]): boolean {
    return status.onboardingStatus === "pending" && status.canConfigure && hasSipUpdatePermission(permissions);
}

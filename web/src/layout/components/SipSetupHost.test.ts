import { describe, expect, it } from "vitest";
import type { SipSetupStatus } from "@/api/gb28181";
import { hasSipUpdatePermission, shouldOpenSipSetup } from "./sipSetupHostRules";

const status = (onboardingStatus: SipSetupStatus["onboardingStatus"], canConfigure = true): SipSetupStatus => ({
    onboardingStatus,
    onboardingVersion: 1,
    configStatus: "unconfigured",
    runtime: { state: "unconfigured", updatedAt: "" },
    canConfigure,
    restartRequired: false
});

describe("SipSetupHost rules", () => {
    it("opens only for a pending authorized user", () => {
        const permissions = ["gb28181:sip:config:update"];
        expect(shouldOpenSipSetup(status("pending"), permissions)).toBe(true);
        expect(shouldOpenSipSetup(status("completed"), permissions)).toBe(false);
        expect(shouldOpenSipSetup(status("skipped"), permissions)).toBe(false);
        expect(shouldOpenSipSetup(status("legacy"), permissions)).toBe(false);
        expect(shouldOpenSipSetup(status("pending", false), permissions)).toBe(false);
    });

    it("recognizes explicit and super administrator permissions", () => {
        expect(hasSipUpdatePermission([])).toBe(false);
        expect(hasSipUpdatePermission(["gb28181:sip:config:update"])).toBe(true);
        expect(hasSipUpdatePermission(["*:*:*"])).toBe(true);
    });
});

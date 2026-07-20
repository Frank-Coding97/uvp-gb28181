import { describe, expect, it } from "vitest";
import type { SipRuntimeState, SipSetupStatus } from "@/api/gb28181";
import { hasSipUpdatePermission, shouldOpenSipSetup } from "./sipSetupHostRules";

// 2026-07-20 起 gating 只看 runtime.state,不再有 onboardingStatus 四态.
const status = (state: SipRuntimeState): SipSetupStatus => ({
    configStatus: state === "running" ? "configured" : "unconfigured",
    runtime: { state, updatedAt: "" }
});

describe("SipSetupHost rules", () => {
    it("opens on unconfigured or failed state for authorized user", () => {
        const permissions = ["gb28181:sip:config:update"];
        expect(shouldOpenSipSetup(status("unconfigured"), permissions)).toBe(true);
        expect(shouldOpenSipSetup(status("failed"), permissions)).toBe(true);
    });

    it("stays closed on running / starting / disabled", () => {
        const permissions = ["gb28181:sip:config:update"];
        expect(shouldOpenSipSetup(status("running"), permissions)).toBe(false);
        expect(shouldOpenSipSetup(status("starting"), permissions)).toBe(false);
        expect(shouldOpenSipSetup(status("disabled"), permissions)).toBe(false);
    });

    it("respects permission gating", () => {
        expect(shouldOpenSipSetup(status("unconfigured"), [])).toBe(false);
        expect(shouldOpenSipSetup(status("unconfigured"), ["other:perm"])).toBe(false);
    });

    it("recognizes explicit and super administrator permissions", () => {
        expect(hasSipUpdatePermission([])).toBe(false);
        expect(hasSipUpdatePermission(["gb28181:sip:config:update"])).toBe(true);
        expect(hasSipUpdatePermission(["*:*:*"])).toBe(true);
    });
});

import { beforeEach, describe, expect, it, vi } from "vitest";

const api = vi.hoisted(() => ({
  loadStandaloneSetupStatus: vi.fn(),
  refreshStandaloneSetupStatus: vi.fn()
}));

vi.mock("@/api/standalone-setup", () => ({
  loadStandaloneSetupStatus: api.loadStandaloneSetupStatus,
  refreshStandaloneSetupStatus: api.refreshStandaloneSetupStatus
}));

import { useStandaloneDeveloperCapability } from "./useStandaloneDeveloperCapability";

const standaloneProbe = (phase: "pending_admin" | "pending_sip" | "complete") => ({
  kind: "standalone" as const,
  status: { standalone: true as const, phase }
});

describe("useStandaloneDeveloperCapability", () => {
  beforeEach(() => {
    api.loadStandaloneSetupStatus.mockReset();
    api.refreshStandaloneSetupStatus.mockReset();
  });

  it.each(["pending_admin", "pending_sip", "complete"] as const)("marks standalone phase %s as unsupported", async phase => {
    api.loadStandaloneSetupStatus.mockResolvedValue(standaloneProbe(phase));

    const capability = useStandaloneDeveloperCapability();
    expect(capability.state.value).toBe("checking");
    await capability.load();

    expect(capability.state.value).toBe("unsupported");
    expect(capability.canUse.value).toBe(false);
  });

  it("keeps legacy server mode supported", async () => {
    api.loadStandaloneSetupStatus.mockResolvedValue({ kind: "legacy" });

    const capability = useStandaloneDeveloperCapability();
    await capability.load();

    expect(capability.state.value).toBe("supported");
    expect(capability.canUse.value).toBe(true);
  });

  it("keeps an unavailable probe blocked and retries through the fresh status API", async () => {
    api.loadStandaloneSetupStatus.mockResolvedValue({ kind: "unavailable" });
    api.refreshStandaloneSetupStatus.mockResolvedValue({ kind: "legacy" });

    const capability = useStandaloneDeveloperCapability();
    await capability.load();

    expect(capability.state.value).toBe("unavailable");
    expect(capability.canUse.value).toBe(false);

    await capability.retry();

    expect(api.refreshStandaloneSetupStatus).toHaveBeenCalledTimes(1);
    expect(capability.state.value).toBe("supported");
    expect(capability.canUse.value).toBe(true);
  });

  it("does not treat a probe exception as server mode", async () => {
    api.loadStandaloneSetupStatus.mockRejectedValue(new Error("transport failed"));

    const capability = useStandaloneDeveloperCapability();
    await capability.load();

    expect(capability.state.value).toBe("unavailable");
    expect(capability.canUse.value).toBe(false);
  });
});

import { describe, expect, it, vi } from "vitest";
import { useSipSetup, type SipSetupApi } from "./useSipSetup";

const configuredStatus = {
    configStatus: "configured" as const,
    runtime: { state: "running" as const, updatedAt: "2026-07-19T12:00:00Z" },
    config: {
        deploymentMode: "public" as const,
        listenIp: "192.168.1.10",
        advertiseIp: "203.0.113.10",
        advertiseIpInferred: false,
        port: 5061,
        domain: "3402000000",
        serverId: "34020000002000000001",
        password: "Secret123",
        hasPassword: true
    }
};

function fakeApi(overrides: Partial<SipSetupApi> = {}): SipSetupApi {
    return {
        status: vi.fn().mockResolvedValue({ code: 0, message: "", data: configuredStatus }),
        interfaces: vi.fn().mockResolvedValue({ code: 0, message: "", data: { items: [], scanStatus: "ok" } }),
        save: vi.fn().mockResolvedValue({
            code: 0,
            message: "",
            data: {
                config: configuredStatus.config,
                reloadedOk: true,
                reloadError: "",
                runtime: { state: "running", updatedAt: "" }
            }
        }),
        skip: vi.fn().mockResolvedValue({ code: 0, message: "", data: { acknowledged: true } }),
        ...overrides
    };
}

describe("useSipSetup", () => {
    it("maps an existing configuration without exposing its password", async () => {
        const setup = useSipSetup(fakeApi());
        await setup.loadStatus();
        expect(setup.form.deploymentMode).toBe("public");
        expect(setup.form.password).toBe("");
        expect(setup.hasExistingPassword.value).toBe(true);
    });

    it("clears a stale advertise address for wildcard LAN and omits an empty retained password", async () => {
        const api = fakeApi();
        const setup = useSipSetup(api);
        await setup.loadStatus();
        setup.form.deploymentMode = "lan";
        setup.form.listenIp = "0.0.0.0";
        setup.form.advertiseIp = "192.168.1.20";
        await setup.save();
        expect(api.save).toHaveBeenCalledWith(expect.objectContaining({
            deploymentMode: "lan",
            listenIp: "0.0.0.0",
            advertiseIp: "",
            advertiseIpInferred: false
        }));
        expect(api.save).toHaveBeenCalledWith(expect.not.objectContaining({ password: expect.anything() }));
    });

    it("keeps form values when saving fails", async () => {
        const api = fakeApi({ save: vi.fn().mockRejectedValue(new Error("network down")) });
        const setup = useSipSetup(api);
        setup.form.serverId = "34020000002000000001";
        await expect(setup.save()).rejects.toThrow("network down");
        expect(setup.form.serverId).toBe("34020000002000000001");
        expect(setup.error.value).toBe("network down");
    });

    it("exposes saving state so callers can suppress duplicate submits", async () => {
        type SaveResponse = Awaited<ReturnType<SipSetupApi["save"]>>;
        let resolveRequest!: (value: SaveResponse) => void;
        const api = fakeApi({ save: vi.fn(() => new Promise<SaveResponse>(resolve => { resolveRequest = resolve; })) });
        const setup = useSipSetup(api);
        setup.form.deploymentMode = "lan";
        const pending = setup.save();
        expect(setup.saving.value).toBe(true);
        resolveRequest({
            code: 0,
            message: "",
            data: {
                config: configuredStatus.config,
                reloadedOk: true,
                reloadError: "",
                runtime: configuredStatus.runtime
            }
        });
        await pending;
        expect(setup.saving.value).toBe(false);
    });

    it("reports reload failure via response payload", async () => {
        const api = fakeApi({
            save: vi.fn().mockResolvedValue({
                code: 0,
                message: "",
                data: {
                    config: configuredStatus.config,
                    reloadedOk: false,
                    reloadError: "bind failed on 192.168.1.20:5061",
                    runtime: { state: "failed", updatedAt: "", errorSummary: "bind failed" }
                }
            })
        });
        const setup = useSipSetup(api);
        setup.form.deploymentMode = "lan";
        const result = await setup.save();
        expect(result.reloadedOk).toBe(false);
        expect(result.reloadError).toContain("bind failed");
    });
});

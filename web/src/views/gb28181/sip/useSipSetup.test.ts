import { afterEach, describe, expect, it, vi } from "vitest";
import { useSipSetup, type SipSetupApi } from "./useSipSetup";

const configuredStatus = {
  configStatus: "configured" as const,
  runtime: { state: "running" as const, updatedAt: "2026-07-19T12:00:00Z" },
  config: {
    deploymentMode: "public" as const,
    listenIp: "192.168.1.10",
    advertiseIp: "203.0.113.10",
    advertiseIpInferred: false,
    hookIp: "192.0.2.11",
    streamIp: "media.example.com",
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
  it("sends a custom domain and an eight-character password unchanged", async () => {
    const api = fakeApi();
    const setup = useSipSetup(api);
    await setup.loadStatus();
    setup.form.domain = "4401000000";
    setup.form.password = "K9#nT2xQ";
    await setup.save();
    expect(api.save).toHaveBeenCalledWith(expect.objectContaining({ domain: "4401000000", password: "K9#nT2xQ" }));
  });

  it("loads and saves optional media addresses", async () => {
    const api = fakeApi();
    const setup = useSipSetup(api);
    await setup.loadStatus();
    expect(setup.form.hookIp).toBe("192.0.2.11");
    expect(setup.form.streamIp).toBe("media.example.com");
    setup.form.hookIp = "2001:db8::22";
    setup.form.streamIp = "stream.example.com";
    await setup.save();
    expect(api.save).toHaveBeenCalledWith(expect.objectContaining({ hookIp: "2001:db8::22", streamIp: "stream.example.com" }));
  });
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
    expect(api.save).toHaveBeenCalledWith(
      expect.objectContaining({
        deploymentMode: "lan",
        listenIp: "0.0.0.0",
        advertiseIp: "",
        advertiseIpInferred: false
      })
    );
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
    const api = fakeApi({
      save: vi.fn(
        () =>
          new Promise<SaveResponse>(resolve => {
            resolveRequest = resolve;
          })
      )
    });
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

  it("keeps successful SIP reload separate from media defaults convergence failure", async () => {
    const api = fakeApi({
      save: vi.fn().mockResolvedValue({
        code: 0,
        message: "",
        data: {
          config: configuredStatus.config,
          reloadedOk: true,
          reloadError: "",
          mediaDefaultsApplied: false,
          mediaDefaultsError: "node config changed during convergence",
          runtime: { state: "running", updatedAt: "", errorSummary: "" }
        }
      })
    });
    const setup = useSipSetup(api);
    setup.form.deploymentMode = "lan";
    const result = await setup.save();
    expect(result.reloadedOk).toBe(true);
    expect(result.reloadError).toBe("");
    expect(result.mediaDefaultsApplied).toBe(false);
    expect(result.mediaDefaultsError).toContain("node config changed during convergence");
  });

  // ---- 向导默认端口的来源（绿色包与其它构建形态必须分开）----
  describe("default port", () => {
    afterEach(() => {
      vi.unstubAllEnvs();
    });

    it("prefills the packed default when the build injects VITE_DEFAULT_SIP_PORT", () => {
      // 两个绿色包各用一套端口段（Linux 51000-51064 / Windows 15500-15564，
      // 见 useSipSetup.ts 顶部注释）—— 这里只需证明"注入什么就原样预填什么"，
      // 两套段位都要过，免得有人把某一套当成通用值写进代码。
      for (const packed of ["51064", "15564"]) {
        vi.stubEnv("VITE_DEFAULT_SIP_PORT", packed);
        expect(useSipSetup(fakeApi()).form.port).toBe(Number(packed));
      }
    });

    it("falls back to 5061 when the injected value is missing or invalid", () => {
      for (const bad of ["", "not-a-number", "0", "70000", "15564.5"]) {
        vi.stubEnv("VITE_DEFAULT_SIP_PORT", bad);
        expect(useSipSetup(fakeApi()).form.port).toBe(5061);
      }
    });

    // ⛔⛔ 构建期常量是**打镜像那一刻**定的，安装脚本平移整段后它落在段外：
    //    220 上用 53000 段部署，引导页默认值仍是 51064（防火墙一条规则只放行段内），
    //    设备注册必然不通。所以部署层一旦下发了端口，必须以它为准。
    it("prefers the deployment-provided port over the packed default", async () => {
      vi.stubEnv("VITE_DEFAULT_SIP_PORT", "51064");
      const api = fakeApi({
        status: vi.fn().mockResolvedValue({
          code: 0,
          message: "",
          data: { configStatus: "unconfigured", runtime: { state: "unconfigured", updatedAt: "" }, defaultSipPort: 53064 }
        })
      });
      const setup = useSipSetup(api);
      expect(setup.form.port).toBe(51064);
      await setup.loadStatus();
      expect(setup.form.port).toBe(53064);
    });

    it("keeps the packed default when the deployment does not provide a port", async () => {
      vi.stubEnv("VITE_DEFAULT_SIP_PORT", "15564");
      const api = fakeApi({
        status: vi.fn().mockResolvedValue({
          code: 0,
          message: "",
          data: { configStatus: "unconfigured", runtime: { state: "unconfigured", updatedAt: "" } }
        })
      });
      const setup = useSipSetup(api);
      await setup.loadStatus();
      expect(setup.form.port).toBe(15564);
    });

    it("ignores an invalid deployment-provided port", async () => {
      vi.stubEnv("VITE_DEFAULT_SIP_PORT", "15564");
      for (const bad of [0, 70000, -1, 15564.5]) {
        const api = fakeApi({
          status: vi.fn().mockResolvedValue({
            code: 0,
            message: "",
            data: { configStatus: "unconfigured", runtime: { state: "unconfigured", updatedAt: "" }, defaultSipPort: bad }
          })
        });
        const setup = useSipSetup(api);
        await setup.loadStatus();
        expect(setup.form.port).toBe(15564);
      }
    });

    // 已有配置时，界面必须显示**库里存的**端口，不能被部署默认值覆盖 ——
    // 否则运维改过的端口每次打开页面都会被"改回去"，保存一次就把 SIP 挪到段外。
    it("keeps the configured port instead of the deployment default", async () => {
      vi.stubEnv("VITE_DEFAULT_SIP_PORT", "51064");
      const api = fakeApi({
        status: vi.fn().mockResolvedValue({
          code: 0,
          message: "",
          data: { ...configuredStatus, defaultSipPort: 53064 }
        })
      });
      const setup = useSipSetup(api);
      await setup.loadStatus();
      expect(setup.form.port).toBe(5061);
    });
  });
});

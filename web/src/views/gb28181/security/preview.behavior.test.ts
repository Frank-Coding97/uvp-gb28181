import { enableAutoUnmount, flushPromises, mount } from "@vue/test-utils";
import { afterEach, expect, it, vi } from "vitest";
import { getSecurityPolicy, updateSecurityPolicy, type SecurityPolicy } from "@/api/gb28181-security";
import SecurityPreview from "./preview.vue";

vi.mock("@visactor/vchart", () => ({ default: vi.fn() }));
vi.mock("@/api/gb28181-security", () => {
  const empty = vi.fn().mockResolvedValue({ code: 0, data: { items: [], total: 0 } });
  return {
    createSecurityAccessRule: vi.fn(),
    deleteSecurityAccessRule: vi.fn(),
    getSecurityAgentHealth: vi.fn().mockResolvedValue({ code: 0, data: { connected: false, appliedRules: 0 } }),
    getSecurityPolicy: vi.fn(),
    getSecuritySnapshot: vi.fn().mockResolvedValue({ code: 0, data: { events: [] } }),
    listSecurityAccessRules: empty,
    listSecurityBans: empty,
    listSecurityEvents: empty,
    unbanSecurity: vi.fn(),
    updateSecurityAccessRule: vi.fn(),
    updateSecurityPolicy: vi.fn()
  };
});

enableAutoUnmount(afterEach);
afterEach(() => vi.unstubAllGlobals());

it("returns the visible policy to observe on a firewall failure and allows retry", async () => {
  vi.stubGlobal(
    "ResizeObserver",
    class {
      observe = vi.fn();
      disconnect = vi.fn();
    }
  );
  const observe: SecurityPolicy = {
    mode: "observe",
    window: 10,
    banScore: 100,
    maxPacketBytes: 65536,
    maxUdpPerWindow: 120,
    maxTcpConnections: 32,
    samplePerSource: 3,
    nonceTtl: 60,
    banTTLs: [{ score: 100, ttl: 0 }],
    allowlist: []
  };
  const protect: SecurityPolicy = { ...observe, mode: "protect" };
  const read = vi.mocked(getSecurityPolicy);
  read.mockResolvedValue({ code: 0, data: observe });
  const save = vi.mocked(updateSecurityPolicy);
  save.mockRejectedValueOnce({
    isAxiosError: true,
    response: { data: { errorCode: "firewall_activation_failed", message: "主机防火墙代理未启动。", data: observe } }
  });

  const wrapper = mount(SecurityPreview, {
    global: { stubs: { "a-alert": { template: "<div role='alert'><slot name='title' /><slot /></div>" } } }
  });
  await flushPromises();
  await wrapper.get(".advice-block").trigger("click");
  await wrapper.get("button.mode-protect").trigger("click");
  const saveButton = wrapper.findAll("button").find(button => button.text().includes("保存策略"));
  expect(saveButton).toBeDefined();
  await saveButton!.trigger("click");
  await flushPromises();

  expect(save).toHaveBeenCalledWith(protect);
  expect(wrapper.get("button.mode-observe").attributes("aria-checked")).toBe("true");
  const alert = wrapper.get('[role="alert"]');
  expect(alert.text()).toContain("当前仍为「观察」");
  expect(alert.text()).toContain("sudo ./uvp-gb28181-service-install.sh --firewall-only");
  expect(alert.get("a").attributes("href")).toContain("/blog/tutorials/linux-green-package-sip-firewall");

  save.mockResolvedValueOnce({ code: 0, data: protect });
  read.mockResolvedValue({ code: 0, data: protect });
  await wrapper.get("button.mode-protect").trigger("click");
  await saveButton!.trigger("click");
  await flushPromises();
  expect(wrapper.get("button.mode-protect").attributes("aria-checked")).toBe("true");
  expect(wrapper.find('[role="alert"]').exists()).toBe(false);
});

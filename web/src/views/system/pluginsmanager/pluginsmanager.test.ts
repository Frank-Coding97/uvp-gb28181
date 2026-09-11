import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";

const capability = vi.hoisted(() => ({
  mode: "unsupported" as "supported" | "unsupported" | "unavailable",
  load: vi.fn(),
  retry: vi.fn()
}));

const api = vi.hoisted(() => ({
  getPluginsExportAPI: vi.fn(),
  exportPluginAPI: vi.fn(),
  importPluginAPI: vi.fn(),
  deletePluginAPI: vi.fn()
}));

vi.mock("@/hooks/useStandaloneDeveloperCapability", () => ({
  useStandaloneDeveloperCapability: () => ({
    state: { __v_isRef: true, value: capability.mode },
    canUse: { __v_isRef: true, value: capability.mode === "supported" },
    loading: { __v_isRef: true, value: false },
    load: capability.load,
    retry: capability.retry
  })
}));

vi.mock("@/api/pluginsmanager", () => ({
  getPluginsExportAPI: api.getPluginsExportAPI,
  exportPluginAPI: api.exportPluginAPI,
  importPluginAPI: api.importPluginAPI,
  deletePluginAPI: api.deletePluginAPI
}));

import PluginsManagerPage from "./pluginsmanager.vue";

function mountPage() {
  return mount(PluginsManagerPage, {
    global: {
      stubs: {
        "s-layout-search": { template: "<section><slot name='fields'/><slot name='actions'/><slot name='extra'/></section>" },
        "a-input": { template: "<input />" },
        "a-button": { template: "<button><slot name='icon' /><slot /></button>" },
        "a-row": { template: "<div><slot /></div>" },
        "a-col": { template: "<div><slot /></div>" },
        "a-card": { template: "<div><slot name='cover' /><slot name='title' /><slot /></div>" },
        "a-descriptions": { template: "<div><slot /></div>" },
        "a-descriptions-item": { template: "<div><slot /></div>" },
        "a-modal": { template: "<div><slot name='title' /><slot /></div>" },
        "a-space": { template: "<div><slot /></div>" },
        "a-alert": { template: "<div role='alert'><slot /></div>" },
        "a-link": { template: "<button><slot /></button>" },
        "a-checkbox": { template: "<label><input type='checkbox' /><slot /></label>" },
        "a-popconfirm": { template: "<div><slot /></div>" },
        "a-empty": { template: "<div><slot /></div>" },
        "a-table": { template: "<div><slot name='columns' /></div>" },
        "a-table-column": { template: "<div />" },
        PluginImportModal: { template: "<div data-testid='plugin-import-modal' />" }
      }
    }
  });
}

describe("plugins manager developer capability gate", () => {
  beforeEach(() => {
    capability.mode = "unsupported";
    capability.load.mockReset().mockResolvedValue(undefined);
    capability.retry.mockReset().mockResolvedValue(undefined);
    api.getPluginsExportAPI.mockReset().mockResolvedValue({ data: { list: [] } });
    api.exportPluginAPI.mockReset();
    api.importPluginAPI.mockReset();
    api.deletePluginAPI.mockReset();
  });

  it("does not request plugin structure data or mount import in standalone mode", async () => {
    const wrapper = mountPage();
    await flushPromises();

    expect(wrapper.text()).toContain("Windows 单机版不支持插件结构管理");
    expect(api.getPluginsExportAPI).not.toHaveBeenCalled();
    expect(api.exportPluginAPI).not.toHaveBeenCalled();
    expect(api.importPluginAPI).not.toHaveBeenCalled();
    expect(api.deletePluginAPI).not.toHaveBeenCalled();
    expect(wrapper.find('[data-testid="plugin-import-modal"]').exists()).toBe(false);
  });

  it("keeps an unavailable status blocked and exposes retry", async () => {
    capability.mode = "unavailable";
    const wrapper = mountPage();
    await flushPromises();

    expect(wrapper.text()).toContain("暂时无法确认当前运行模式");
    expect(api.getPluginsExportAPI).not.toHaveBeenCalled();
    await wrapper.get('[data-testid="developer-capability-retry"]').trigger("click");
    expect(capability.retry).toHaveBeenCalledTimes(1);
  });

  it("keeps the legacy page loading its existing plugin list", async () => {
    capability.mode = "supported";
    mountPage();
    await flushPromises();

    expect(api.getPluginsExportAPI).toHaveBeenCalledTimes(1);
  });
});

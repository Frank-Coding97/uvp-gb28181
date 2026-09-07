import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";

const capability = vi.hoisted(() => ({
  mode: "unsupported" as "supported" | "unsupported" | "unavailable",
  load: vi.fn(),
  retry: vi.fn()
}));

const api = vi.hoisted(() => ({
  getSysGenListAPI: vi.fn(),
  batchInsertSysGenAPI: vi.fn(),
  deleteSysGenAPI: vi.fn(),
  refreshFields: vi.fn(),
  getSysGenByIdAPI: vi.fn(),
  updateSysGenAPI: vi.fn(),
  generateCode: vi.fn(),
  getTables: vi.fn(),
  getTableColumns: vi.fn(),
  previewCode: vi.fn(),
  insertmenuandapi: vi.fn()
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

vi.mock("@/api/sysgen", () => ({
  getSysGenListAPI: api.getSysGenListAPI,
  batchInsertSysGenAPI: api.batchInsertSysGenAPI,
  deleteSysGenAPI: api.deleteSysGenAPI,
  refreshFields: api.refreshFields,
  getSysGenByIdAPI: api.getSysGenByIdAPI,
  updateSysGenAPI: api.updateSysGenAPI
}));

vi.mock("@/api/syscodegen", () => ({
  generateCode: api.generateCode,
  getTables: api.getTables,
  getTableColumns: api.getTableColumns,
  previewCode: api.previewCode,
  insertmenuandapi: api.insertmenuandapi
}));

vi.mock("@/globals", () => ({
  arcoMessage: vi.fn(),
  formatTime: (value: unknown) => String(value),
  throttle: <T extends (...args: any[]) => any>(fn: T) => fn
}));

vi.mock("./components/codegen-config-drawer.vue", () => ({
  default: {
    setup() {
      api.getSysGenListAPI({ source: "hidden-drawer" });
      return () => null;
    }
  }
}));

vi.mock("./components/codegen-preview-modal.vue", () => ({
  default: {
    template: "<div />"
  }
}));

import CodegenPage from "./codegen.vue";

function mountPage() {
  return mount(CodegenPage, {
    global: {
      stubs: {
        "s-layout-search": { template: "<section><slot name='fields'/><slot name='actions'/><slot name='extra'/></section>" },
        "a-input": { template: "<input />" },
        "a-button": { template: "<button><slot name='icon' /><slot /></button>" },
        "a-table": { template: "<div><slot name='columns' /></div>" },
        "a-table-column": { template: "<div />" },
        "a-modal": { template: "<div><slot name='title' /><slot /></div>" },
        "a-space": { template: "<div><slot /></div>" },
        "a-alert": { template: "<div role='alert'><slot /></div>" },
        "a-link": { template: "<button><slot /></button>" },
        "a-popconfirm": { template: "<div><slot /></div>" },
        "a-empty": { template: "<div><slot /></div>" },
        "a-spin": { template: "<div><slot /></div>" }
      }
    }
  });
}

describe("codegen developer capability gate", () => {
  beforeEach(() => {
    capability.mode = "unsupported";
    capability.load.mockReset().mockResolvedValue(undefined);
    capability.retry.mockReset().mockResolvedValue(undefined);
    api.getSysGenListAPI.mockReset().mockResolvedValue({ data: { list: [], total: 0 } });
    api.batchInsertSysGenAPI.mockReset();
    api.deleteSysGenAPI.mockReset();
    api.refreshFields.mockReset();
    api.getSysGenByIdAPI.mockReset();
    api.updateSysGenAPI.mockReset();
    api.generateCode.mockReset();
    api.getTables.mockReset().mockResolvedValue({ data: { tables: [] } });
    api.getTableColumns.mockReset();
    api.previewCode.mockReset();
    api.insertmenuandapi.mockReset();
  });

  it("does not mount the hidden drawer or request any structure API in standalone mode", async () => {
    const wrapper = mountPage();
    await flushPromises();

    expect(wrapper.text()).toContain("Windows 单机版不支持开发代码生成");
    expect(api.getSysGenListAPI).not.toHaveBeenCalled();
    expect(api.getTables).not.toHaveBeenCalled();
    expect(api.batchInsertSysGenAPI).not.toHaveBeenCalled();
    expect(api.deleteSysGenAPI).not.toHaveBeenCalled();
    expect(api.refreshFields).not.toHaveBeenCalled();
    expect(api.generateCode).not.toHaveBeenCalled();
    expect(api.getTableColumns).not.toHaveBeenCalled();
    expect(api.previewCode).not.toHaveBeenCalled();
    expect(api.insertmenuandapi).not.toHaveBeenCalled();
  });

  it("keeps an unavailable status blocked and exposes retry", async () => {
    capability.mode = "unavailable";
    const wrapper = mountPage();
    await flushPromises();

    expect(wrapper.text()).toContain("暂时无法确认当前运行模式");
    expect(api.getSysGenListAPI).not.toHaveBeenCalled();
    await wrapper.get('[data-testid="developer-capability-retry"]').trigger("click");
    expect(capability.retry).toHaveBeenCalledTimes(1);
  });

  it("keeps the legacy page loading its existing list", async () => {
    capability.mode = "supported";
    const wrapper = mountPage();
    await flushPromises();

    expect(api.getSysGenListAPI).toHaveBeenCalled();
    expect(wrapper.text()).not.toContain("不支持开发代码生成");
  });
});

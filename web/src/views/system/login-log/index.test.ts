import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";
import LoginLogPage from "./index.vue";

const api = vi.hoisted(() => ({
  getLoginLogsAPI: vi.fn(),
  getLoginLogDetailAPI: vi.fn()
}));

vi.mock("@/api/login-log", () => ({
  getLoginLogsAPI: api.getLoginLogsAPI,
  getLoginLogDetailAPI: api.getLoginLogDetailAPI
}));
vi.mock("@/globals", () => ({ formatTime: (value: string) => value }));

const log = (id = 1) => ({
  id, userId: 7, username: "alice", result: "failure", failureReason: "unknown_reason",
  ip: "10.0.0.1", location: "内网", browser: "Chrome", os: "macOS", createdAt: "2026-08-18T07:00:00Z"
});

function mountPage() {
  return mount(LoginLogPage, {
    global: {
      stubs: {
        "s-layout-search": { template: "<section><slot name='fields'/><slot name='actions'/></section>" },
        "a-input": { template: "<input />" },
        "a-select": { template: "<select><slot /></select>" },
        "a-option": { template: "<option><slot /></option>" },
        "a-range-picker": { template: "<div data-testid='range-picker' />" },
        "a-button": { template: "<button><slot name='icon' /><slot /></button>" },
        "a-table": { template: "<div data-testid='login-table'><slot name='columns' /><slot name='empty' /></div>" },
        "a-table-column": { template: "<div />" },
        "a-tag": { template: "<span><slot /></span>" },
        "a-link": { template: "<button><slot /></button>" },
        "a-modal": { template: "<div><slot name='title' /><slot /></div>" },
        "a-spin": { template: "<div><slot /></div>" },
        "a-descriptions": { template: "<dl><slot /></dl>" },
        "a-descriptions-item": { template: "<div><slot /></div>" },
        "a-empty": { template: "<span>{{ description }}</span>", props: ["description"] },
        Search: true,
        RotateCcw: true
      }
    }
  });
}

describe("login log page", () => {
  beforeEach(() => {
    api.getLoginLogsAPI.mockReset().mockResolvedValue({ code: 0, data: { list: [log()], total: 1 } });
    api.getLoginLogDetailAPI.mockReset().mockResolvedValue({ code: 0, data: { ...log(), failureReason: "password_incorrect", userAgent: "Mozilla/5.0" } });
  });

  it("loads, filters, resets, and falls back for unknown failure reasons", async () => {
    const wrapper = mountPage();
    await flushPromises();
    expect(api.getLoginLogsAPI).toHaveBeenCalledWith({ pageNum: 1, pageSize: 20 });
    expect((wrapper.vm as any).failureLabel("unknown_reason")).toBe("其他失败");

    const vm = wrapper.vm as any;
    vm.form.username = " alice ";
    vm.form.result = "failure";
    vm.form.failureReason = "password_incorrect";
    vm.form.ip = "10.0.0.1";
    vm.dateRange = ["2026-08-18 00:00:00", "2026-08-18 23:59:59"];
    await vm.search();
    expect(api.getLoginLogsAPI).toHaveBeenLastCalledWith({
      pageNum: 1, pageSize: 20, username: "alice", result: "failure", failureReason: "password_incorrect", ip: "10.0.0.1",
      startTime: "2026-08-18 00:00:00", endTime: "2026-08-18 23:59:59"
    });
    await vm.reset();
    expect(api.getLoginLogsAPI).toHaveBeenLastCalledWith({ pageNum: 1, pageSize: 20 });
  });

  it("loads detail separately and exposes a readable user-agent", async () => {
    const wrapper = mountPage();
    await flushPromises();
    await (wrapper.vm as any).viewDetail(log(7));
    expect(api.getLoginLogDetailAPI).toHaveBeenCalledWith(7);
    expect((wrapper.vm as any).currentDetail.userAgent).toBe("Mozilla/5.0");
  });

  it("shows API permission errors without rendering the table", async () => {
    api.getLoginLogsAPI.mockRejectedValueOnce(new Error("权限不足"));
    const wrapper = mountPage();
    await flushPromises();
    expect(wrapper.get('[role="alert"]').text()).toContain("权限不足");
    expect(wrapper.find('[data-testid="login-table"]').exists()).toBe(false);
  });
});

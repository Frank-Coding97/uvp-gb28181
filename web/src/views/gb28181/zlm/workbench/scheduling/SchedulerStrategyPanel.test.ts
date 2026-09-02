import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { flushPromises, mount } from "@vue/test-utils";
import { describe, expect, it, vi } from "vitest";

const api = vi.hoisted(() => ({
  getScheduler: vi.fn(),
  switchScheduler: vi.fn()
}));

vi.mock("@/api/gb28181-zlm", () => ({
  getScheduler: api.getScheduler,
  switchScheduler: api.switchScheduler
}));

vi.mock("@/store/modules/user", () => ({
  useUserStoreHook: () => ({ account: { permissions: [] } })
}));

import SchedulerStrategyPanel from "./SchedulerStrategyPanel.vue";

const stubs = {
  "a-button": { template: "<button><slot name='icon' /><slot /></button>" },
  "a-radio": { props: ["value"], template: "<label><input type='radio' :value='value' /><slot /></label>" },
  "a-radio-group": { template: "<div><slot /></div>" },
  "a-spin": { template: "<div><slot /></div>" },
  "a-tag": { template: "<span><slot /></span>" }
};

describe("SchedulerStrategyPanel", () => {
  it("does not request the scheduler while inactive and loads when activated", async () => {
    api.getScheduler.mockResolvedValue({
      code: 0,
      data: { algorithm: "roundrobin", available: ["roundrobin", "weighted", "leastload"] }
    });
    const wrapper = mount(SchedulerStrategyPanel, {
      props: { active: false, canManage: false },
      global: { stubs }
    });

    await flushPromises();
    expect(api.getScheduler).not.toHaveBeenCalled();
    await wrapper.setProps({ active: true });
    await flushPromises();
    expect(api.getScheduler).toHaveBeenCalledTimes(1);
    wrapper.unmount();
  });

  it("keeps the switch boundary and failed-switch semantics explicit", () => {
    const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/zlm/workbench/scheduling/SchedulerStrategyPanel.vue"), "utf8");
    expect(source).toContain("next_invite");
    expect(source).toContain("只影响新点播");
    expect(source).toContain("switchError");
    expect(source).toContain("旧策略");
    expect(source).toContain("active");
    expect(readFileSync(resolve(process.cwd(), "src/views/gb28181/zlm/SchedulerStrategy.vue"), "utf8")).toContain("SchedulerStrategyPanel");
  });
});

import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";

const api = vi.hoisted(() => ({
  getScheduler: vi.fn(),
  switchScheduler: vi.fn()
}));
const userState = vi.hoisted(() => ({ permissions: [] as string[] }));

vi.mock("@/api/gb28181-zlm", () => ({
  getScheduler: api.getScheduler,
  switchScheduler: api.switchScheduler
}));

vi.mock("@/store/modules/user", () => ({
  useUserStoreHook: () => ({ account: userState })
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
  beforeEach(() => {
    userState.permissions = [];
    api.getScheduler.mockReset();
    api.switchScheduler.mockReset();
  });

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
    expect(source).toContain("只影响新点播");
    expect(source).toContain("switchError");
    expect(source).toContain("旧策略");
    expect(source).toContain("active");
    expect(source).not.toContain("effective-boundary");
    expect(source).not.toContain("effectiveFrom");
    expect(readFileSync(resolve(process.cwd(), "src/views/gb28181/zlm/SchedulerStrategy.vue"), "utf8")).toContain("SchedulerStrategyPanel");
  });

  it("lays out the three scheduling algorithms in one responsive row", () => {
    const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/zlm/workbench/scheduling/SchedulerStrategyPanel.vue"), "utf8");

    expect(source).toMatch(/\.algorithm-list\s*\{[^}]*grid-template-columns:\s*repeat\(3,\s*minmax\(0,\s*1fr\)\)/s);
    expect(source).toMatch(/@media\s*\(max-width:\s*760px\)[\s\S]*\.algorithm-list\s*\{[^}]*grid-template-columns:\s*1fr/s);
  });

  it("uses the defined panel radius token for the strategy card", () => {
    const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/zlm/workbench/scheduling/SchedulerStrategyPanel.vue"), "utf8");

    expect(source).toMatch(/\.scheduler-strategy-panel\s*\{[^}]*border-radius:\s*var\(--zlm-radius-lg\)/s);
    expect(source).not.toContain("var(--zlm-radius-xl)");
  });

  it("uses account permissions when canManage is omitted", async () => {
    userState.permissions = ["gb28181:zlm:scheduler:manage"];
    api.getScheduler.mockResolvedValue({
      code: 0,
      data: { algorithm: "roundrobin", available: ["roundrobin", "weighted", "leastload"] }
    });

    const wrapper = mount(SchedulerStrategyPanel, { global: { stubs } });
    await flushPromises();

    expect(wrapper.find(".permission-state").exists()).toBe(false);
    expect(wrapper.find(".primary-button").exists()).toBe(true);
    wrapper.unmount();
  });

  it("honors an explicit read-only override", async () => {
    userState.permissions = ["gb28181:zlm:scheduler:manage"];
    api.getScheduler.mockResolvedValue({
      code: 0,
      data: { algorithm: "roundrobin", available: ["roundrobin", "weighted", "leastload"] }
    });

    const wrapper = mount(SchedulerStrategyPanel, {
      props: { canManage: false },
      global: { stubs }
    });
    await flushPromises();

    expect(wrapper.find(".permission-state").exists()).toBe(true);
    expect(wrapper.find(".primary-button").exists()).toBe(false);
    wrapper.unmount();
  });
});

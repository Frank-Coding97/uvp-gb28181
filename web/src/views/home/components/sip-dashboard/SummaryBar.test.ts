import { mount } from "@vue/test-utils";
import { describe, expect, it } from "vitest";
import SummaryBar from "./SummaryBar.vue";

describe("SIP summary bar", () => {
  it("只展示有真实语义的汇总指标", () => {
    const wrapper = mount(SummaryBar, {
      props: {
        health: 98.5,
        todayTotal: 1234,
        todayAbnormal: 2
      }
    });

    expect(wrapper.find(".summary-bar__num").text()).toBe("98.5");
    expect(wrapper.findAll(".summary-bar__caption").map(item => item.text())).toEqual(["今日事务", "异常事务"]);
    expect(wrapper.text()).not.toContain("待处理");
  });

  it("空样本态不把汇总指标伪装成真实零值", () => {
    const wrapper = mount(SummaryBar, {
      props: {
        health: -1,
        todayTotal: 0,
        todayAbnormal: 0
      }
    });

    expect(wrapper.find(".summary-bar__num").text()).toBe("--");
    expect(wrapper.findAll(".summary-bar__value").map(item => item.text())).toEqual(["--", "--"]);
  });
});

import { mount } from "@vue/test-utils";
import { describe, expect, it } from "vitest";
import TransactionGrid from "./TransactionGrid.vue";

describe("SIP transaction grid", () => {
  it("保留事务核心数据但不展示无采样依据的趋势", () => {
    const wrapper = mount(TransactionGrid, {
      props: {
        transactions: [
          {
            kind: "REGISTER",
            labelZh: "注册",
            labelEn: "REGISTER",
            todayCount: 12,
            successRate: 0.99,
            trendPct: 18.5,
            alert: false
          }
        ]
      }
    });

    expect(wrapper.find(".tx-cell__name").text()).toContain("注册");
    expect(wrapper.find(".tx-cell__count").text()).toBe("12");
    expect(wrapper.find(".tx-cell__rate").text()).toBe("99.0%");
    expect(wrapper.find(".tx-cell__trend").exists()).toBe(false);
    expect(wrapper.text()).not.toContain("18.5%");
  });
});

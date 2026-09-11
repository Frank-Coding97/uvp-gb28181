import { mount } from "@vue/test-utils";
import { describe, expect, it } from "vitest";

import RecordingSummaryStrip from "./RecordingSummaryStrip.vue";

describe("RecordingSummaryStrip", () => {
  it("renders unavailable values as unknown instead of zero", () => {
    const wrapper = mount(RecordingSummaryStrip, {
      props: {
        items: [
          { key: "files", label: "查询文件", value: 12, note: "当前筛选" },
          { key: "plans", label: "启用计划", value: null, note: "全部可见计划" }
        ]
      }
    });

    expect(wrapper.text()).toContain("12");
    expect(wrapper.text()).toContain("—");
    expect(wrapper.text()).toContain("未加载 / 无权限");
    expect(wrapper.text()).not.toContain("启用计划0");
  });
});

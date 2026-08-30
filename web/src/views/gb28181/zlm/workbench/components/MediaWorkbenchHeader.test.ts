import { mount } from "@vue/test-utils";
import { describe, expect, it } from "vitest";

import MediaWorkbenchHeader from "./MediaWorkbenchHeader.vue";

describe("MediaWorkbenchHeader", () => {
  it("communicates partial and stale states without discarding the last success", async () => {
    const wrapper = mount(MediaWorkbenchHeader, {
      props: {
        title: "媒体总览",
        description: "聚合媒体节点状态",
        status: "partial",
        statusText: "2 个节点采集失败",
        lastSuccessAt: "2026-08-30 11:20:00",
        autoRefresh: true
      }
    });

    expect(wrapper.get("h1").text()).toBe("媒体总览");
    expect(wrapper.get("[role='status']").text()).toContain("部分可用");
    expect(wrapper.text()).toContain("2 个节点采集失败");
    expect(wrapper.text()).toContain("2026-08-30 11:20:00");

    await wrapper.get("[aria-label='关闭媒体总览自动刷新']").trigger("click");
    expect(wrapper.emitted("update:autoRefresh")?.[0]).toEqual([false]);
    await wrapper.get("[aria-label='刷新媒体总览']").trigger("click");
    expect(wrapper.emitted("refresh")).toHaveLength(1);
  });

  it("uses an alert for errors and keeps icon actions keyboard accessible", () => {
    const wrapper = mount(MediaWorkbenchHeader, {
      props: {
        title: "接入管理",
        description: "管理接入任务",
        status: "error",
        autoRefresh: false
      }
    });

    expect(wrapper.get("[role='alert']").text()).toContain("刷新失败");
    expect(wrapper.get("button[aria-label='开启接入管理自动刷新']").attributes("type")).toBe("button");
    expect(wrapper.get("button[aria-label='刷新接入管理']").attributes("type")).toBe("button");
  });
});

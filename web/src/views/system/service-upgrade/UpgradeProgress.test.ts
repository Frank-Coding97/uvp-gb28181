import { mount } from "@vue/test-utils";
import { describe, expect, it } from "vitest";
import UpgradeProgress from "./UpgradeProgress.vue";

describe("升级进度展示", () => {
  it("有限字节显示边界值，未知大小保持不确定进度", async () => {
    const wrapper = mount(UpgradeProgress, {
      props: {
        title: "导入",
        detail: "上传中",
        steps: ["上传", "校验", "就绪"],
        current: 0,
        state: "active",
        showUploadBar: true,
        upload: { filename: "release.tar", loaded: 20, total: 10, state: "uploading" }
      }
    });
    expect(wrapper.get('[role="progressbar"]').attributes("aria-valuenow")).toBe("100");
    expect(wrapper.text()).toContain("10 B / 10 B");
    await wrapper.setProps({ upload: { filename: "release.tar", loaded: 20, total: Number.NaN, state: "uploading" } });
    expect(wrapper.get('[role="progressbar"]').attributes("aria-valuenow")).toBeUndefined();
    expect(wrapper.text()).not.toContain("NaN");
    wrapper.unmount();
  });

  it("故障文字安全按需展示，重连不会把当前阶段标为完成", async () => {
    const wrapper = mount(UpgradeProgress, {
      props: {
        title: "服务升级",
        detail: "验证启动",
        steps: ["准备", "验证", "完成"],
        current: 1,
        state: "error",
        reconnecting: true,
        error: '<script>alert("error")</script>'
      }
    });
    expect(wrapper.text()).toContain("等待重连");
    expect(wrapper.get('[aria-current="step"]').text()).not.toContain("已完成");
    expect(wrapper.find("pre").exists()).toBe(false);
    const details = wrapper.get("details");
    (details.element as HTMLDetailsElement).open = true;
    await details.trigger("toggle");
    expect(wrapper.get("pre").text()).toContain("<script>");
    expect(wrapper.find("script").exists()).toBe(false);
    wrapper.unmount();
  });
});

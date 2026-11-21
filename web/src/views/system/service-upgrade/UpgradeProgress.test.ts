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

  it("有字节数时显示真实下载百分比，不再是静止的阶段条", () => {
    const wrapper = mount(UpgradeProgress, {
      props: {
        title: "服务升级",
        detail: "下载中",
        steps: ["准备与授权", "备份数据"],
        current: 0,
        state: "active",
        transfer: { downloaded_bytes: 25, total_bytes: 100 }
      }
    });
    const bar = wrapper.get('[role="progressbar"]');
    expect(bar.attributes("aria-label")).toBe("升级包下载进度");
    expect(bar.attributes("aria-valuenow")).toBe("25");
    expect(bar.attributes("aria-busy")).toBe("false");
    expect(wrapper.text()).toContain("已下载 25 B / 100 B");
    expect(wrapper.text()).toContain("25%");
    wrapper.unmount();
  });

  it("⛔ 拿不到总量时不能显示 0%，要退回不确定进度", () => {
    const wrapper = mount(UpgradeProgress, {
      props: {
        title: "服务升级",
        detail: "下载中",
        steps: ["准备与授权", "备份数据"],
        current: 0,
        state: "active",
        indeterminate: true
      }
    });
    const bar = wrapper.get('[role="progressbar"]');
    expect(bar.attributes("aria-valuenow")).toBeUndefined();
    expect(bar.attributes("aria-busy")).toBe("true");
    expect(wrapper.find(".unknown-track").exists()).toBe(true);
    expect(wrapper.text()).not.toContain("0%");
    wrapper.unmount();
  });

  it("显示本阶段已用时，用来区分「慢」和「卡死」", () => {
    const wrapper = mount(UpgradeProgress, {
      props: {
        title: "服务升级",
        detail: "下载中",
        steps: ["准备与授权"],
        current: 0,
        state: "active",
        elapsed: "本阶段已用时 3 分 20 秒"
      }
    });
    expect(wrapper.get(".progress-detail").text()).toBe("下载中 · 本阶段已用时 3 分 20 秒");
    wrapper.unmount();
  });

  it("字节越界与非法值都夹紧到 0~100", async () => {
    const wrapper = mount(UpgradeProgress, {
      props: {
        title: "服务升级",
        detail: "下载中",
        steps: ["准备与授权"],
        current: 0,
        state: "active",
        transfer: { downloaded_bytes: 300, total_bytes: 100 }
      }
    });
    expect(wrapper.get('[role="progressbar"]').attributes("aria-valuenow")).toBe("100");
    await wrapper.setProps({ transfer: { downloaded_bytes: Number.NaN, total_bytes: 100 } });
    expect(wrapper.get('[role="progressbar"]').attributes("aria-valuenow")).toBe("0");
    expect(wrapper.text()).not.toContain("NaN");
    wrapper.unmount();
  });
});

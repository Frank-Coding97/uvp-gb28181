import { mount } from "@vue/test-utils";
import { afterEach, describe, expect, it, vi } from "vitest";
import CatalogRefreshProgressDialog from "./CatalogRefreshProgressDialog.vue";
import type { CatalogRefreshProgress } from "./api";

const baseProgress = {
  operationId: "op-1",
  deviceId: "34020000002000000001",
  receivedCount: 0,
  totalCount: null,
  startedAt: "2026-10-01T10:00:00Z",
  updatedAt: "2026-10-01T10:00:00Z"
};

function mountDialog(status: CatalogRefreshProgress["status"], overrides: Record<string, unknown> = {}) {
  return mount(CatalogRefreshProgressDialog, {
    props: {
      visible: true,
      deviceName: "测试设备",
      deviceId: baseProgress.deviceId,
      progress: { ...baseProgress, status, ...overrides }
    },
    global: {
      stubs: {
        "a-modal": {
          props: ["visible", "modalClass"],
          emits: ["cancel"],
          template:
            "<div v-if='visible' class='stub-modal' :data-modal-class='modalClass'><button class='modal-cancel' @click='$emit(`cancel`)' /><slot /></div>"
        },
        "a-progress": {
          template: "<div class='stub-progress' :data-percent='percent' :data-type='type' :data-status='status' />",
          props: ["percent", "type", "status"]
        }
      }
    }
  });
}

describe("CatalogRefreshProgressDialog", () => {
  afterEach(() => {
    vi.useRealTimers();
  });

  it("按 WVP 形态在等待设备回执时显示环形进度", () => {
    const wrapper = mountDialog("waiting");
    expect(wrapper.text()).toContain("等待同步中");
    expect(wrapper.text()).toContain("已刷新 0 个通道");
    expect(wrapper.find(".stub-modal").attributes("data-modal-class")).toContain("uvp-system-dialog");
    expect(wrapper.find(".stub-progress").attributes("data-type")).toBe("circle");
    expect(wrapper.find(".stub-progress").attributes("data-percent")).toBe("0");
  });

  it("按已接收和总数展示 WVP 风格的同步文案", () => {
    const wrapper = mountDialog("receiving", { receivedCount: 2, totalCount: 5 });
    expect(wrapper.text()).toContain("同步中...[2/5]");
    expect(wrapper.text()).toContain("已刷新 2 / 5 个通道");
    expect(wrapper.find(".stub-progress").attributes("data-percent")).toBe("0.4");
  });

  it("完成时仍显示最终通道计数", async () => {
    const wrapper = mountDialog("receiving", { receivedCount: 2, totalCount: 5 });
    // ⛔ 喂给 `a-progress` 的是 0~1 的比值：传 40 会让圆环的 strokeDashoffset
    //    恒为 0（环永远画满），还会因 percent >= 1 提前被当成 success。
    expect(wrapper.find(".stub-progress").attributes("data-percent")).toBe("0.4");
    await wrapper.setProps({ progress: { ...baseProgress, status: "completed", receivedCount: 5, totalCount: 5 } });
    expect(wrapper.text()).toContain("已刷新 5 / 5 个通道");
    expect(wrapper.find(".stub-progress").attributes("data-percent")).toBe("1");
  });

  it("展示失败原因并支持关闭", async () => {
    const wrapper = mountDialog("failed", { errorMessage: "设备拒绝查询" });
    expect(wrapper.text()).toContain("设备拒绝查询");
    await wrapper.get(".modal-cancel").trigger("click");
    expect(wrapper.emitted("close")).toHaveLength(1);
  });

  it("完成后按 WVP 行为延迟关闭", async () => {
    vi.useFakeTimers();
    const wrapper = mountDialog("receiving", { receivedCount: 1, totalCount: 5 });
    await wrapper.setProps({ progress: { ...baseProgress, status: "completed", receivedCount: 5, totalCount: 5 } });
    expect(wrapper.text()).toContain("刷新成功");
    expect(wrapper.emitted("close")).toBeUndefined();
    vi.advanceTimersByTime(2999);
    expect(wrapper.emitted("close")).toBeUndefined();
    vi.advanceTimersByTime(1);
    expect(wrapper.emitted("close")).toHaveLength(1);
    await wrapper.unmount();
  });
});

import { flushPromises, shallowMount } from "@vue/test-utils";
import { ref } from "vue";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { RecordingSchedule } from "./types";

const api = vi.hoisted(() => ({ deleteRecordingPlan: vi.fn(), listRecordingPlans: vi.fn() }));
const modal = vi.hoisted(() => ({ warning: vi.fn() }));
const messages = vi.hoisted(() => ({ success: vi.fn() }));
const permissions = vi.hoisted(() => ({ hasPermission: vi.fn() }));
vi.mock("@/api/gb28181-recording-plan", () => api);
vi.mock("@arco-design/web-vue", () => ({ Modal: modal, Message: messages }));
vi.mock("@/hooks/usePermission", () => permissions);
vi.mock("../useDeviceStatusDict", () => ({ useDeviceStatusLabel: () => () => "在线" }));
vi.mock("./components/RecordingScheduleDrawer.vue", () => ({ default: { template: "<div />" } }));
vi.mock("./components/RecordingScheduleEditorDialog.vue", () => ({ default: { template: "<div />" } }));
vi.mock("./components/ChannelAssignmentDialog.vue", () => ({ default: { template: "<div />" } }));

import RecordingPlansPanel from "./RecordingPlansPanel.vue";

const allowed = ref(true);
const plan = { id: "42", name: "测试002", channelCount: 0 } as RecordingSchedule;
const mounted: ReturnType<typeof shallowMount>[] = [];

function mountPanel() {
  const wrapper = shallowMount(RecordingPlansPanel, {
    global: {
      stubs: {
        "s-layout-search": true,
        "a-alert": true,
        "a-table": true,
        "a-table-column": true,
        "a-link": true,
        "a-switch": true,
        "a-popconfirm": true,
        "a-empty": true,
        "a-badge": true,
        "a-spin": true
      }
    }
  });
  mounted.push(wrapper);
  return wrapper;
}

function requestDelete(wrapper: ReturnType<typeof shallowMount>) {
  (wrapper.vm as unknown as { removePlan: (plan: RecordingSchedule) => void }).removePlan(plan);
}

function confirmation() {
  return modal.warning.mock.calls.at(-1)?.[0];
}

describe("RecordingPlansPanel delete confirmation", () => {
  beforeEach(() => {
    allowed.value = true;
    permissions.hasPermission.mockImplementation(permission => !permission.endsWith(":maintain") || allowed.value);
    api.deleteRecordingPlan.mockReset().mockResolvedValue({ code: 0 });
    api.listRecordingPlans.mockReset().mockResolvedValue({ data: { list: [], total: 0 } });
  });

  afterEach(() => {
    mounted.splice(0).forEach(wrapper => wrapper.unmount());
  });

  it("opens the standard warning confirmation and waits for explicit approval", () => {
    requestDelete(mountPanel());
    expect(modal.warning).toHaveBeenCalledWith(
      expect.objectContaining({
        title: "删除录像计划",
        content: expect.stringContaining("测试002"),
        hideCancel: false,
        okText: "删除",
        cancelText: "取消",
        okButtonProps: { status: "danger" },
        onBeforeOk: expect.any(Function)
      })
    );
    expect(confirmation().content).toContain("请先解除分配");
    expect(api.deleteRecordingPlan).not.toHaveBeenCalled();
  });

  it("does not delete when the confirmation is cancelled", () => {
    requestDelete(mountPanel());
    confirmation().onCancel?.();
    expect(api.deleteRecordingPlan).not.toHaveBeenCalled();
    expect(messages.success).not.toHaveBeenCalled();
  });

  it("deletes the confirmed plan and reloads the ledger and enabled total", async () => {
    const wrapper = mountPanel();
    await wrapper.setProps({ active: true });
    await flushPromises();
    api.listRecordingPlans.mockClear();
    requestDelete(wrapper);
    expect(await confirmation().onBeforeOk()).toBe(true);
    expect(api.deleteRecordingPlan).toHaveBeenCalledExactlyOnceWith(42);
    expect(messages.success).toHaveBeenCalledWith("录像计划“测试002”已删除");
    expect(api.listRecordingPlans).toHaveBeenCalledTimes(2);
    expect(api.listRecordingPlans).toHaveBeenCalledWith(expect.objectContaining({ status: "enabled" }));
  });

  it("blocks duplicate submissions until the delete request finishes", async () => {
    let finish!: () => void;
    api.deleteRecordingPlan.mockImplementation(() => new Promise<void>(resolve => (finish = resolve)));
    const wrapper = mountPanel();
    requestDelete(wrapper);
    const confirm = confirmation().onBeforeOk;
    const pending = confirm();
    expect(await confirm()).toBe(false);
    requestDelete(wrapper);
    expect(modal.warning).toHaveBeenCalledTimes(1);
    expect(api.deleteRecordingPlan).toHaveBeenCalledTimes(1);
    finish();
    expect(await pending).toBe(true);
  });

  it("keeps the confirmation open after a backend refusal and allows retry", async () => {
    api.deleteRecordingPlan.mockRejectedValueOnce(new Error("已分配通道，不能删除"));
    requestDelete(mountPanel());
    const confirm = confirmation().onBeforeOk;
    expect(await confirm()).toBe(false);
    expect(messages.success).not.toHaveBeenCalled();
    expect(await confirm()).toBe(true);
    expect(api.deleteRecordingPlan).toHaveBeenCalledTimes(2);
  });

  it("rechecks permission both when opening and submitting the confirmation", async () => {
    const wrapper = mountPanel();
    allowed.value = false;
    requestDelete(wrapper);
    expect(modal.warning).not.toHaveBeenCalled();
    allowed.value = true;
    requestDelete(wrapper);
    allowed.value = false;
    expect(await confirmation().onBeforeOk()).toBe(false);
    expect(api.deleteRecordingPlan).not.toHaveBeenCalled();
  });

  it("closes after a successful delete even if reloading the ledger fails", async () => {
    const wrapper = mountPanel();
    await wrapper.setProps({ active: true });
    await flushPromises();
    api.listRecordingPlans.mockRejectedValue(new Error("刷新失败"));
    requestDelete(wrapper);
    expect(await confirmation().onBeforeOk()).toBe(true);
    expect(api.deleteRecordingPlan).toHaveBeenCalledTimes(1);
  });
});

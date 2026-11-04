import { flushPromises, shallowMount } from "@vue/test-utils";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { ref } from "vue";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { hasMarkup, squash } from "@/test/source-assert";
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

const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/recording-schedules/RecordingPlansPanel.vue"), "utf-8");

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

/** 气泡点确定后真正执行的函数（确认步骤本身在模板的 `@ok` 上）。 */
function deletePlan(wrapper: ReturnType<typeof shallowMount>, target: RecordingSchedule = plan) {
  return (wrapper.vm as unknown as { deletePlan: (plan: RecordingSchedule) => Promise<void> }).deletePlan(target);
}

describe("RecordingPlansPanel delete confirmation", () => {
  beforeEach(() => {
    allowed.value = true;
    permissions.hasPermission.mockImplementation(permission => !permission.endsWith(":maintain") || allowed.value);
    modal.warning.mockReset();
    api.deleteRecordingPlan.mockReset().mockResolvedValue({ code: 0 });
    api.listRecordingPlans.mockReset().mockResolvedValue({ data: { list: [], total: 0 } });
  });

  afterEach(() => {
    mounted.splice(0).forEach(wrapper => wrapper.unmount());
  });

  // ⛔ 2026-11-04：删除确认从 Modal.warning 改成 a-popconfirm 气泡（全站统一）。
  //    这里钉住"确认只在气泡里、确认按钮是红色危险样式、后果说明还在"。
  it("确认走气泡而不是 Modal，且确认按钮是红色危险样式", () => {
    const markup = squash(source);
    expect(markup).toContain("<a-popconfirm");
    expect(hasMarkup(source, `:ok-button-props="{ status: 'danger' }"`)).toBe(true);
    expect(hasMarkup(source, `ok-text="删除"`)).toBe(true);
    expect(hasMarkup(source, `cancel-text="取消"`)).toBe(true);
    // 后果说明必须保留（原先写在 Modal 的 content 里）
    expect(markup).toContain("请先解除分配");
    expect(markup).toContain("uvp-confirm-title");
    // 删除入口只挂在气泡的 @ok 上；列表行上不能再有 @click 直删
    expect(hasMarkup(source, `@ok="deletePlan(record)"`)).toBe(true);
    expect(markup).not.toContain('@click="deletePlan(record)"');
    // 老的 Modal 确认为删除路径已彻底移除
    expect(markup).not.toContain("Modal.warning");
  });

  it("点确认前不会发起删除，也没有任何弹窗", async () => {
    const wrapper = mountPanel();
    // 只有气泡的 @ok 会调到 deletePlan，模板层的入口不会自行触发
    expect(api.deleteRecordingPlan).not.toHaveBeenCalled();
    expect(modal.warning).not.toHaveBeenCalled();
    expect(wrapper.emitted()).toEqual({});
  });

  it("deletes the confirmed plan and reloads the ledger and enabled total", async () => {
    const wrapper = mountPanel();
    await wrapper.setProps({ active: true });
    await flushPromises();
    api.listRecordingPlans.mockClear();

    await deletePlan(wrapper);

    expect(api.deleteRecordingPlan).toHaveBeenCalledExactlyOnceWith(42);
    expect(messages.success).toHaveBeenCalledWith("录像计划“测试002”已删除");
    expect(api.listRecordingPlans).toHaveBeenCalledTimes(2);
    expect(api.listRecordingPlans).toHaveBeenCalledWith(expect.objectContaining({ status: "enabled" }));
  });

  it("blocks duplicate submissions until the delete request finishes", async () => {
    let finish!: () => void;
    api.deleteRecordingPlan.mockImplementation(() => new Promise<void>(resolve => (finish = resolve)));
    const wrapper = mountPanel();

    const pending = deletePlan(wrapper);
    await deletePlan(wrapper);
    await deletePlan(wrapper);
    expect(api.deleteRecordingPlan).toHaveBeenCalledTimes(1);

    finish();
    await pending;
    expect(api.deleteRecordingPlan).toHaveBeenCalledTimes(1);
  });

  it("keeps the row usable after a backend refusal so the user can retry", async () => {
    api.deleteRecordingPlan.mockRejectedValueOnce(new Error("已分配通道，不能删除"));
    const wrapper = mountPanel();

    await deletePlan(wrapper);
    expect(messages.success).not.toHaveBeenCalled();

    // 后端拒绝后 deletingPlanId 必须归零，否则"删除"永远灰着、用户没法重试
    await deletePlan(wrapper);
    expect(api.deleteRecordingPlan).toHaveBeenCalledTimes(2);
  });

  it("rechecks permission on submit and forwards it to the bubble trigger", async () => {
    const wrapper = mountPanel();
    allowed.value = false;
    await deletePlan(wrapper);
    expect(api.deleteRecordingPlan).not.toHaveBeenCalled();
    expect(modal.warning).not.toHaveBeenCalled();

    // 无权限时连气泡都不该弹（disabled 要转发给 a-popconfirm，否则灰按钮还能弹确认）
    expect(hasMarkup(source, `:disabled="!canMaintain || deletingPlanId !== null"`)).toBe(true);

    allowed.value = true;
    await deletePlan(wrapper);
    expect(api.deleteRecordingPlan).toHaveBeenCalledTimes(1);
  });

  it("closes after a successful delete even if reloading the ledger fails", async () => {
    const wrapper = mountPanel();
    await wrapper.setProps({ active: true });
    await flushPromises();
    api.listRecordingPlans.mockRejectedValue(new Error("刷新失败"));

    await expect(deletePlan(wrapper)).resolves.toBeUndefined();
    expect(api.deleteRecordingPlan).toHaveBeenCalledTimes(1);
    expect(messages.success).toHaveBeenCalledTimes(1);
  });
});

import { defineComponent, h } from "vue";
import { mount } from "@vue/test-utils";
import { describe, expect, it } from "vitest";
import DeviceFirmwareUpgradeDialog from "./DeviceFirmwareUpgradeDialog.vue";

const device = {
  id: 31,
  deviceId: "34020000001320000001",
  name: "园区 NVR",
  alias: "北门录像机",
  manufacturer: "海康",
  model: "DS-7608",
  firmware: "V5.8.0",
  effectiveVersion: "2022",
  transport: "TCP",
  ip: "192.0.2.31",
  port: 5060,
  status: 1,
  online: true,
  channelCount: 8,
  channelOnlineCount: 7,
  onlineRate: 87.5,
  createdAt: "2026-09-05T01:00:00Z",
  updatedAt: "2026-09-05T02:00:00Z",
  ownerDeptId: 1
};

const PanelStub = defineComponent({
  emits: ["busy", "firmware-updated", "view-records", "operation-updated", "submission-uncertain", "close-blocked"],
  setup(_, { emit }) {
    return () =>
      h("div", { "data-testid": "drawer-panel-stub" }, [
        h("button", { "data-testid": "panel-block-close", onClick: () => emit("close-blocked", true) }, "block"),
        h("button", { "data-testid": "panel-emit-busy", onClick: () => emit("busy", true) }, "busy"),
        h("button", { "data-testid": "panel-view-records", onClick: () => emit("view-records", "op-1") }, "records")
      ]);
  }
});

function mountDialog(attrs: Record<string, unknown> = {}) {
  return mount(DeviceFirmwareUpgradeDialog, {
    props: { visible: true, device, canUpgrade: true, rebootBusy: false, ...attrs },
    global: {
      stubs: {
        "a-modal": {
          props: ["visible", "width", "closable", "maskClosable", "modalClass"],
          template:
            "<div v-if='visible' data-testid='dialog-stub' :data-width='width' :data-closable='closable' :data-mask-closable='maskClosable' :data-modal-class='modalClass'><slot name='title' /><slot /><button data-testid='dialog-cancel' @click='$emit(`cancel`)'>close</button></div>"
        },
        DeviceFirmwareUpgradePanel: PanelStub
      }
    }
  });
}

describe("DeviceFirmwareUpgradeDialog", () => {
  it("renders the device/version title and constrained responsive width", () => {
    const wrapper = mountDialog();
    expect(wrapper.attributes("data-width")).toBe("min(760px, calc(100vw - 32px))");
    expect(wrapper.attributes("data-modal-class")).toContain("uvp-system-dialog");
    expect(wrapper.text()).toContain("北门录像机 · 固件升级");
    expect(wrapper.text()).toContain("当前版本 V5.8.0");
    expect(wrapper.get("[data-testid='firmware-upgrade-device-identity']").text()).toContain("34020000001320000001");
    expect(wrapper.text()).toContain("在线");
    expect(wrapper.text()).toContain("海康 / DS-7608");
    wrapper.unmount();
  });

  it("forwards visibility and operation events while only blocking close during POST", async () => {
    const wrapper = mountDialog();
    const panel = wrapper.getComponent(PanelStub);
    await panel.get("[data-testid='panel-emit-busy']").trigger("click");
    expect(wrapper.emitted("busy")).toEqual([[true]]);
    await panel.get("[data-testid='panel-view-records']").trigger("click");
    expect(wrapper.emitted("viewRecords")).toEqual([["op-1"]]);

    await wrapper.get("[data-testid='dialog-cancel']").trigger("click");
    expect(wrapper.emitted("update:visible")).toEqual([[false]]);

    await panel.get("[data-testid='panel-block-close']").trigger("click");
    expect(wrapper.attributes("data-closable")).toBe("false");
    await wrapper.get("[data-testid='dialog-cancel']").trigger("click");
    expect(wrapper.emitted("update:visible")).toHaveLength(1);
    wrapper.unmount();
  });
});

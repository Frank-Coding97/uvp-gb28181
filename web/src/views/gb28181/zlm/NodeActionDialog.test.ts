import { defineComponent, h } from "vue";
import { flushPromises, mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({
  setMaintenance: vi.fn(),
  deleteNode: vi.fn(),
  kickSessions: vi.fn(),
  restartNode: vi.fn()
}));

vi.mock("@/api/gb28181-zlm", () => ({
  setZLMNodeMaintenance: mocks.setMaintenance,
  deleteZLMNode: mocks.deleteNode,
  kickZLMNodeSessions: mocks.kickSessions,
  restartZLMNode: mocks.restartNode
}));

import ZLMNodeActionDialog from "./ZLMNodeActionDialog.vue";

const node = {
  id: 7,
  revision: 1,
  name: "zlm-a",
  host: "10.0.0.7",
  receiveHost: "",
  playbackHost: "",
  apiPort: 18080,
  mediaServerUUID: "uuid-a",
  weight: 50,
  state: "active" as const,
  recoveryRequired: false,
  rtpPortStart: 30000,
  rtpPortEnd: 35000,
  stats: {
    lastHeartbeatAt: "2026-08-30T00:00:00Z",
    mediaSourceCount: 3,
    sessionCount: 5,
    netThreadLoadAvg: 0,
    workThreadLoadAvg: 0,
    memoryUsageBytes: 0,
    totalBytesIn: 0,
    totalBytesOut: 0
  },
  autoOnDemandReady: true,
  createdAt: "2026-08-30T00:00:00Z",
  updatedAt: "2026-08-30T00:00:00Z"
};

const DangerStub = defineComponent({
  name: "DangerStub",
  props: {
    visible: Boolean,
    fingerprint: { type: String, default: "" },
    impacts: { type: Array, default: () => [] }
  },
  emits: ["confirm", "update:visible", "stale"],
  setup(props, { emit }) {
    return () => props.visible
      ? h("button", {
        class: "confirm-danger",
        onClick: () => emit("confirm", { fingerprint: props.fingerprint, reason: "", nodeId: 7, targetKey: "node" })
      }, (props.impacts as string[]).join("|"))
      : null;
  }
});

describe("ZLMNodeActionDialog", () => {
  beforeEach(() => {
    sessionStorage.clear();
    mocks.setMaintenance.mockReset();
    mocks.deleteNode.mockReset();
    mocks.kickSessions.mockReset();
    mocks.restartNode.mockReset();
  });

  it("preflights first and only executes with the exact backend fingerprint", async () => {
    mocks.setMaintenance
      .mockResolvedValueOnce({
        code: 0,
        data: {
          nodeId: 7,
          action: "maintenance",
          impact: { streams: 3, recordings: 2, sessions: 5, truncated: false },
          fingerprint: "fp-7",
          observedAt: "2026-08-30T00:00:00Z"
        }
      })
      .mockResolvedValueOnce({ code: 0, data: { ok: true } });

    const pinia = createPinia();
    setActivePinia(pinia);
    const wrapper = mount(ZLMNodeActionDialog, {
      props: { visible: true, node, action: "maintenance" },
      global: {
        plugins: [pinia],
        stubs: {
          ZLMDangerActionDialog: DangerStub,
          "a-modal": { props: ["visible"], template: "<section v-if='visible'><slot name='title' /><slot /></section>" },
          "a-spin": { template: "<span />" }
        }
      }
    });

    await flushPromises();
    expect(mocks.setMaintenance).toHaveBeenNthCalledWith(1, 7);
    expect(wrapper.text()).toContain("活动流 3 路");
    expect(wrapper.text()).toContain("录制任务 2 个");

    await wrapper.get(".confirm-danger").trigger("click");
    await flushPromises();

    expect(mocks.setMaintenance).toHaveBeenNthCalledWith(2, 7, "fp-7");
    expect(wrapper.emitted("done")).toEqual([[{ action: "maintenance" }]]);
  });

  it("fails closed when the backend does not return a fingerprint", async () => {
    mocks.setMaintenance.mockResolvedValueOnce({ code: 0, data: { ok: true } });
    const pinia = createPinia();
    setActivePinia(pinia);
    const wrapper = mount(ZLMNodeActionDialog, {
      props: { visible: true, node, action: "maintenance" },
      global: {
        plugins: [pinia],
        stubs: {
          ZLMDangerActionDialog: DangerStub,
          "a-modal": { props: ["visible"], template: "<section v-if='visible'><slot name='title' /><slot /></section>" },
          "a-spin": { template: "<span />" }
        }
      }
    });

    await flushPromises();
    expect(wrapper.text()).toContain("未执行任何管理动作");
    expect(wrapper.find(".confirm-danger").exists()).toBe(false);
    expect(mocks.setMaintenance).toHaveBeenCalledTimes(1);
  });
});

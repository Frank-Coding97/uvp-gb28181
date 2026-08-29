import { flushPromises, mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it } from "vitest";

import { useZLMContextStore } from "@/store/modules/zlm-context";
import ZLMDangerActionDialog from "./ZLMDangerActionDialog.vue";

const stubs = {
  "a-modal": {
    props: ["visible"],
    emits: ["cancel", "update:visible"],
    template: "<section v-if='visible'><slot name='title' /><slot /></section>"
  },
  "a-textarea": { template: "<textarea />" },
  "a-input": { template: "<input />" },
  "a-button": { template: "<button><slot /></button>" }
};

describe("ZLMDangerActionDialog", () => {
  beforeEach(() => sessionStorage.clear());

  it("shows the captured node, target, impact and confirmation phrase", () => {
    const testPinia = createPinia();
    setActivePinia(testPinia);
    useZLMContextStore().initialize([{ id: 7, name: "边缘节点 A", state: "active" }], "7");
    const wrapper = mount(ZLMDangerActionDialog, {
      props: {
        visible: true,
        nodeId: 7,
        nodeName: "边缘节点 A",
        targetKey: "target-a",
        targetLabel: "live/camera",
        impacts: ["中断 3 个观看会话", "停止 MP4 录像"],
        confirmPhrase: "关闭 live/camera"
      },
      global: { plugins: [testPinia], stubs }
    });

    expect(wrapper.text()).toContain("边缘节点 A（#7）");
    expect(wrapper.text()).toContain("live/camera");
    expect(wrapper.text()).toContain("中断 3 个观看会话");
    expect(wrapper.text()).toContain("停止 MP4 录像");
    expect(wrapper.text()).toContain("关闭 live/camera");
    expect(wrapper.text()).toContain("操作理由");
  });

  it("closes as stale when the selected node changes", async () => {
    const testPinia = createPinia();
    setActivePinia(testPinia);
    const context = useZLMContextStore();
    context.initialize([
      { id: 7, name: "边缘节点 A", state: "active" },
      { id: 8, name: "边缘节点 B", state: "active" }
    ], "7");
    const wrapper = mount(ZLMDangerActionDialog, {
      props: {
        visible: true,
        nodeId: 7,
        nodeName: "边缘节点 A",
        targetKey: "target-a",
        targetLabel: "目标 A",
        confirmPhrase: "确认"
      },
      global: { plugins: [testPinia], stubs }
    });

    context.selectNode(8);
    await flushPromises();
    expect(wrapper.emitted("stale")).toHaveLength(1);
    expect(wrapper.emitted("update:visible")?.at(-1)).toEqual([false]);
  });
});

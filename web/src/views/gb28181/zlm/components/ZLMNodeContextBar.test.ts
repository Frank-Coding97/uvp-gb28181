import { flushPromises, mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it } from "vitest";

import ZLMNodeContextBar from "./ZLMNodeContextBar.vue";

const nodes = [
  { id: 1, name: "主节点", state: "active" as const },
  { id: 2, name: "离线节点", state: "offline" as const }
];

describe("ZLMNodeContextBar", () => {
  beforeEach(() => {
    sessionStorage.clear();
  });

  it("keeps and labels an offline query selection, then falls back after removal", async () => {
    const testPinia = createPinia();
    setActivePinia(testPinia);
    const wrapper = mount(ZLMNodeContextBar, {
      props: { nodes, queryNodeId: "2" },
      global: { plugins: [testPinia] }
    });

    expect(wrapper.text()).toContain("离线节点");
    expect(wrapper.text()).toContain("节点已离线，保留当前选择以便排查");

    await wrapper.setProps({ nodes: [nodes[0]] });
    await flushPromises();
    expect(wrapper.text()).toContain("主节点");
    expect(wrapper.text()).toContain("节点在线，运行态数据会自动刷新");
  });
});

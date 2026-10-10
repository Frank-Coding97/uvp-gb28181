import { flushPromises, shallowMount } from "@vue/test-utils";
import { describe, expect, it, vi } from "vitest";
import NodeForm from "./NodeForm.vue";

const api = vi.hoisted(() => ({ getConfig: vi.fn() }));
vi.mock("@/api/gb28181-zlm", () => ({
  getZLMNodeConfig: api.getConfig,
  createZLMNode: vi.fn(),
  updateZLMNode: vi.fn(),
  probeZLMNode: vi.fn()
}));

describe("node fixed listener readback", () => {
  it("replaces a stale registered port with the live port and renders it readonly", async () => {
    api.getConfig.mockResolvedValue({ code: 0, data: { groups: [{ items: [{ key: "rtp_proxy.port", value: "40000" }] }] } });
    const wrapper = shallowMount(NodeForm, {
      props: {
        visible: true,
        node: { id: 7, name: "node", host: "192.0.2.1", apiPort: 18080, rtpReceiveMode: "single", rtpProxyPort: 10000 } as any
      },
      global: {
        renderStubDefaultSlot: true,
        stubs: { "a-input": { props: ["modelValue"], template: '<input :value="modelValue" />' } }
      }
    });
    await flushPromises();
    expect(api.getConfig).toHaveBeenCalledWith(7);
    const input = wrapper.get('input[aria-label="RTP 单端口"]');
    expect((input.element as HTMLInputElement).value).toBe("40000");
    expect(input.attributes()).toHaveProperty("readonly");
    wrapper.unmount();
  });
});

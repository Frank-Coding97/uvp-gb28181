import { defineComponent, h } from "vue";
import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({
  preflight: vi.fn(),
  forceClose: vi.fn(),
  confirm: vi.fn(),
  closeModal: vi.fn(),
  refresh: vi.fn()
}));

vi.mock("@/api/gb28181-zlm-runtime", () => ({
  preflightCloseZLMStream: mocks.preflight,
  forceCloseZLMStream: mocks.forceClose
}));
vi.mock("@/store/modules/user", () => ({
  useUserStoreHook: () => ({ account: { permissions: ["gb28181:zlm:stream:force-close"] } })
}));
vi.mock("@arco-design/web-vue", () => ({
  Modal: { confirm: mocks.confirm },
  Message: { success: vi.fn(), warning: vi.fn(), error: vi.fn() }
}));
vi.mock("../../composables/useZLMRuntimePolling", () => ({
  useZLMRuntimePolling: () => ({ refresh: mocks.refresh })
}));

import StreamPanel from "./StreamPanel.vue";

const media = { schema: "rtsp", vhost: "__defaultVhost__", app: "live", stream: "camera-1" };
const stream = {
  nodeId: 7,
  nodeName: "边缘节点 A",
  media,
  ownership: { status: "owned" },
  readerCount: 1,
  totalReaderCount: 1,
  bytesSpeed: 0,
  aliveSecond: 10,
  recordingMp4: false,
  recordingHls: false
};

const TableStub = defineComponent({
  setup(_, { slots }) {
    return () => h("div", slots.columns?.());
  }
});
const ColumnStub = defineComponent({
  setup(_, { slots }) {
    return () => h("div", slots.cell?.({ record: stream }));
  }
});

describe("stream force close", () => {
  beforeEach(() => {
    Object.values(mocks).forEach(mock => mock.mockReset());
  });

  it("preflights a single stream and submits force close only after one confirmation", async () => {
    mocks.preflight.mockResolvedValue({
      code: 0,
      data: {
        target: { nodeId: 7, media },
        snapshot: { status: "owned", present: true, presenceKnown: true },
        fingerprint: "sha256:fresh",
        freshPresent: true
      }
    });
    mocks.forceClose.mockResolvedValue({
      code: 0,
      data: { closed: true, alreadyAbsent: false, uncertain: false }
    });
    mocks.confirm.mockReturnValue({ close: mocks.closeModal });

    const wrapper = mount(StreamPanel, {
      props: { active: true, scope: "all", nodeId: null },
      global: {
        stubs: {
          "s-layout-search": true,
          "a-table": TableStub,
          "a-table-column": ColumnStub,
          "a-link": { template: "<button><slot /></button>" },
          "a-pagination": true,
          "a-drawer": true
        }
      }
    });
    await wrapper
      .findAll("button")
      .find(button => button.text() === "强关")
      ?.trigger("click");
    await flushPromises();

    expect(mocks.preflight).toHaveBeenCalledWith(7, media);
    expect(mocks.confirm).toHaveBeenCalledTimes(1);
    expect(mocks.forceClose).not.toHaveBeenCalled();

    const confirmation = mocks.confirm.mock.calls[0][0];
    await confirmation.onBeforeOk();
    expect(mocks.forceClose).toHaveBeenCalledWith(7, media, "sha256:fresh", "用户确认强关");
    wrapper.unmount();
    expect(mocks.closeModal).toHaveBeenCalledOnce();
  });

  it("renders the node name as the stream source", () => {
    const wrapper = mount(StreamPanel, {
      props: { active: true, scope: "all", nodeId: null },
      global: {
        stubs: {
          "s-layout-search": true,
          "a-table": TableStub,
          "a-table-column": ColumnStub,
          "a-link": { template: "<button><slot /></button>" },
          "a-pagination": true,
          "a-drawer": true
        }
      }
    });

    expect(wrapper.text()).toContain("边缘节点 A");
    expect(wrapper.text()).not.toContain("#7");
    wrapper.unmount();
  });
});

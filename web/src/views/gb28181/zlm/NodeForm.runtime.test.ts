import { flushPromises, shallowMount } from "@vue/test-utils";
import { defineComponent, h } from "vue";
import { afterEach, describe, expect, it, vi } from "vitest";
import NodeForm from "./NodeForm.vue";

const api = vi.hoisted(() => ({ getConfig: vi.fn(), probe: vi.fn() }));
vi.mock("@/api/gb28181-zlm", () => ({
  getZLMNodeConfig: api.getConfig,
  createZLMNode: vi.fn(),
  updateZLMNode: vi.fn(),
  probeZLMNode: api.probe
}));

/** 全局的 a-input-password 桩不回写 modelValue，新建流程填不了 Secret —— 这里补一个会回写的。 */
const PasswordInputStub = defineComponent({
  props: { modelValue: String },
  emits: ["update:modelValue"],
  setup:
    (props, { emit }) =>
    () =>
      h("input", {
        type: "password",
        value: props.modelValue,
        onInput: (event: Event) => emit("update:modelValue", (event.target as HTMLInputElement).value)
      })
});

/**
 * ⛔ 全局的 `a-modal` 桩只渲染 title + 默认插槽，**footer 整个丢掉** ——
 * 底部的「连接并读取 / 确认添加」在单测里根本不存在，会让人误判成"按钮没渲染"。
 */
const createStubs = {
  "a-input-password": PasswordInputStub,
  "a-modal": { template: "<div><slot name='title' /><slot /><slot name='footer' /></div>" }
};

/** 关掉弹窗后组件里的定时器/请求会留在同一个 jsdom 里，逐个卸载避免串台。 */
const mounted: ReturnType<typeof shallowMount>[] = [];
function track<T extends ReturnType<typeof shallowMount>>(wrapper: T): T {
  mounted.push(wrapper);
  return wrapper;
}
afterEach(() => {
  mounted.splice(0).forEach(wrapper => wrapper.unmount());
});

const multiNode = {
  id: 7,
  name: "node",
  host: "192.0.2.1",
  apiPort: 18080,
  rtpReceiveMode: "multi" as const,
  rtpPortStart: 30000,
  rtpPortEnd: 35000
};

function mountEdit(config) {
  api.getConfig.mockResolvedValue(config);
  return track(
    shallowMount(NodeForm, {
      props: { visible: true, node: multiNode as any },
      global: { renderStubDefaultSlot: true }
    })
  );
}

describe("node fixed listener readback", () => {
  it("replaces a stale registered port with the live port and renders it readonly", async () => {
    api.getConfig.mockResolvedValue({ code: 0, data: { groups: [{ items: [{ key: "rtp_proxy.port", value: "40000" }] }] } });
    const wrapper = track(
      shallowMount(NodeForm, {
        props: {
          visible: true,
          node: { id: 7, name: "node", host: "192.0.2.1", apiPort: 18080, rtpReceiveMode: "single", rtpProxyPort: 10000 } as any
        },
        global: {
          renderStubDefaultSlot: true,
          stubs: { "a-input": { props: ["modelValue"], template: '<input :value="modelValue" />' } }
        }
      })
    );
    await flushPromises();
    expect(api.getConfig).toHaveBeenCalledWith(7);
    const input = wrapper.get('input[aria-label="RTP 单端口"]');
    expect((input.element as HTMLInputElement).value).toBe("40000");
    expect(input.attributes()).toHaveProperty("readonly");
  });
});

/**
 * 收流端口模式只在编辑态出现：新建时固定多端口（后端 `buildCreateCandidate` 的默认），
 * 界面不给人选 —— 新建阶段还核不到 ZLM 的单端口监听，选了单端口只会把节点建成一个收不到流的配置。
 */
describe("receive port mode is edit-only", () => {
  it("hides the mode switch while adding a node but still shows the multi-port range", async () => {
    const wrapper = track(
      shallowMount(NodeForm, {
        props: { visible: true, node: null },
        global: { renderStubDefaultSlot: true, stubs: createStubs }
      })
    );
    await flushPromises();

    expect(wrapper.html()).not.toContain('aria-label="收流端口模式"');
    expect(wrapper.find('input[aria-label="RTP 起始端口"]').exists()).toBe(true);
    expect(wrapper.find('input[aria-label="RTP 单端口"]').exists()).toBe(false);
  });

  it("keeps the mode switch when editing a node", async () => {
    const wrapper = mountEdit({ code: 0, data: { groups: [{ items: [] }] } });
    await flushPromises();

    expect(wrapper.html()).toContain('aria-label="收流端口模式"');
  });
});

/**
 * 多端口收流范围与单端口同规则：**只读**，值来自 ZLM 实测。
 * 真源在 ZLM 的 `[rtp_proxy] port_range`，平台这边只是镜像，而且平台从不下发这个键 ——
 * 所以这个框本来就不该让人改，要改去「服务配置」。
 */
describe("node multi-port range mirror", () => {
  const items = (port?: string, range?: string) => ({
    code: 0,
    data: {
      groups: [
        {
          items: [
            ...(port ? [{ key: "rtp_proxy.port", value: port }] : []),
            ...(range ? [{ key: "rtp_proxy.port_range", value: range }] : [])
          ]
        }
      ]
    }
  });

  it("fills the range from ZLM instead of the stale registered mirror", async () => {
    const wrapper = mountEdit(items("10000", "51014-51063"));
    await flushPromises();

    expect((wrapper.get('input[aria-label="RTP 起始端口"]').element as HTMLInputElement).value).toBe("51014");
    expect((wrapper.get('input[aria-label="RTP 结束端口"]').element as HTMLInputElement).value).toBe("51063");
  });

  it("renders both range inputs readonly so the only way to change it is 服务配置", async () => {
    const wrapper = mountEdit(items("10000", "51014-51063"));
    await flushPromises();

    expect(wrapper.get('input[aria-label="RTP 起始端口"]').attributes()).toHaveProperty("readonly");
    expect(wrapper.get('input[aria-label="RTP 结束端口"]').attributes()).toHaveProperty("readonly");
  });

  it("keeps the registered range when ZLM does not return one", async () => {
    const wrapper = mountEdit(items("10000"));
    await flushPromises();

    expect((wrapper.get('input[aria-label="RTP 起始端口"]').element as HTMLInputElement).value).toBe("30000");
    expect((wrapper.get('input[aria-label="RTP 结束端口"]').element as HTMLInputElement).value).toBe("35000");
  });

  it("keeps the registered range when the returned value is unusable", async () => {
    for (const unusable of ["", "51014", "0-0", "51299-51200", "30000-70000"]) {
      const wrapper = mountEdit(items("10000", unusable));
      await flushPromises();

      expect((wrapper.get('input[aria-label="RTP 起始端口"]').element as HTMLInputElement).value).toBe("30000");
      expect((wrapper.get('input[aria-label="RTP 结束端口"]').element as HTMLInputElement).value).toBe("35000");
    }
  });

  it("shows the probed range on the create confirmation step instead of the hardcoded default", async () => {
    api.probe.mockResolvedValue({
      code: 0,
      data: {
        online: true,
        mediaServerId: "uvp-media-server-0001",
        serverConfig: {
          httpPort: 51004,
          rtpProxyPort: 10000,
          rtpPortRangeStart: 51014,
          rtpPortRangeEnd: 51063
        }
      }
    });

    const wrapper = track(
      shallowMount(NodeForm, {
        props: { visible: true, node: null },
        global: { renderStubDefaultSlot: true, stubs: createStubs }
      })
    );
    await flushPromises();

    await wrapper.get('input[placeholder="后端可访问的 ZLM IP"]').setValue("10.0.0.7");
    await wrapper.get('input[placeholder="1-65535"]').setValue("18080");
    await wrapper.get('input[type="password"]').setValue("s3cret");

    const connect = wrapper.findAll("button").find(button => button.text().includes("连接并读取"));
    expect(connect, "找不到「连接并读取」按钮，新建流程的用例已经失去意义").toBeTruthy();
    await connect!.trigger("click");
    await flushPromises();

    expect(api.probe).toHaveBeenCalledTimes(1);
    expect(wrapper.text()).toContain("51014 - 51063");
  });

  it("falls back to a usable range on the create step when the probe cannot report one", async () => {
    api.probe.mockResolvedValue({
      code: 0,
      data: {
        online: true,
        mediaServerId: "",
        serverConfig: { httpPort: 51004, rtpProxyPort: 10000, rtpPortRangeStart: 0, rtpPortRangeEnd: 0 }
      }
    });

    const wrapper = track(
      shallowMount(NodeForm, {
        props: { visible: true, node: null },
        global: { renderStubDefaultSlot: true, stubs: createStubs }
      })
    );
    await flushPromises();

    await wrapper.get('input[placeholder="后端可访问的 ZLM IP"]').setValue("10.0.0.7");
    await wrapper.get('input[placeholder="1-65535"]').setValue("18080");
    await wrapper.get('input[type="password"]').setValue("s3cret");

    await wrapper
      .findAll("button")
      .find(button => button.text().includes("连接并读取"))!
      .trigger("click");
    await flushPromises();

    // ⛔ 不能写 0：那会让用户卡在一个自己根本改不了的校验错误上。
    expect(wrapper.text()).toContain("30000 - 35000");
  });
});

/**
 * 引导页配的媒体地址是「平台默认」，节点级留空是完全正常的状态。
 * 但输入框必须把这个「实际生效值」显示出来，否则用户看到三个空框，
 * 会以为引导步骤里配的地址没保存/没回显（2026-10-20 现场反馈）。
 */
describe("inherited media address hint", () => {
  const effectiveNode = {
    ...multiNode,
    hookIp: "",
    sdpIp: "",
    playbackHost: "",
    effectiveHookIp: "192.168.10.220",
    effectiveSdpIp: "192.168.10.220",
    effectivePlaybackHost: "192.168.10.220"
  };

  it("surfaces the effective address while the node-level field is empty", async () => {
    const wrapper = track(
      shallowMount(NodeForm, {
        props: { visible: true, node: effectiveNode as any },
        global: { renderStubDefaultSlot: true, stubs: createStubs }
      })
    );
    await flushPromises();

    // 三项都留空 ⇒ 三处提示，各自指向实际生效的地址。
    expect(wrapper.html().match(/留空则使用：192\.168\.10\.220/g)?.length).toBe(3);
  });

  it("keeps the plain hint once the node has its own override", async () => {
    const wrapper = track(
      shallowMount(NodeForm, {
        props: { visible: true, node: { ...effectiveNode, sdpIp: "198.51.100.21" } as any },
        global: { renderStubDefaultSlot: true, stubs: createStubs }
      })
    );
    await flushPromises();

    // sdpIp 有覆盖值 ⇒ 只剩 hook / playback 两条提示。
    expect(wrapper.html().match(/留空则使用：192\.168\.10\.220/g)?.length).toBe(2);
  });
});

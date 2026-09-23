import { flushPromises, mount } from "@vue/test-utils";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createMemoryHistory, createRouter } from "vue-router";
import { SNAPSHOT_LIBRARY_PATH } from "../snapshot-library/snapshotLibraryState";
import SnapshotConfigPanel from "./SnapshotConfigPanel.vue";

/**
 * 图像抓拍配置面板（2026-09-20 从播放控制台侧栏「高级」搬到设备详情抽屉）的单测。
 *
 * ⛔ 它是**会话**不是单条命令：`POST` 只负责把"张数 + 间隔"下发下去，
 *    设备随后一张张把 JPEG 传回来，所以要轮询到 `completed` / `failed` 才有结论。
 *    别把它写成"发完就说成功"。
 * ⛔ 下发前要求目标通道**在线**：离线时配置根本下不去，禁用比让用户点了没反应好。
 */
const api = vi.hoisted(() => ({
  createDeviceSnapshotSession: vi.fn(),
  getDeviceSnapshotSession: vi.fn()
}));

vi.mock("@/api/gb28181", () => api);

const CHANNEL_ID = 1;

function option(value: number, online = true) {
  return { value, label: `通道 ${value}`, online };
}

function session(overrides: Record<string, unknown> = {}) {
  return {
    sessionId: "snap-1",
    operationId: "snap-op-1",
    channelId: "34020000001320000003",
    channelCode: "0411212755",
    deviceCode: "34020000001320000003",
    snapNum: 2,
    interval: 3,
    state: "receiving",
    receivedCount: 1,
    notifiedCount: 1,
    files: [],
    ...overrides
  };
}

function file(name: string) {
  return { name, size: 2048, receivedAt: "2026-09-20T10:00:00Z", url: `https://example.test/shots/${name}` };
}

/**
 * 面板要 useRouter()，而图像库路由是**菜单树动态生成**的 ——
 * `libraryRoute=false` 模拟"后端还没重启应用迁移"：那会儿 `/gb28181/snapshot-library`
 * 根本没有注册，组件必须自己发现这件事（否则跳过去是 404 白屏）。
 */
function mountPanelWithRouter(props: Record<string, unknown> = {}, libraryRoute = true) {
  const router = createRouter({
    history: createMemoryHistory(),
    // 根路由只是为了让 router 初始化时那个空 location 有地方落，
    // 免得每个用例都刷一条 "No match found for location with path ''"。
    // ⛔ 不能改用 catch-all 兜底：那样 `/gb28181/snapshot-library` 也会"匹配上"，
    // 上面那条"菜单没生效就置灰"的门禁断言会恒真、形同虚设。
    routes: libraryRoute
      ? [
          { path: "/", component: { render: () => null } },
          { path: SNAPSHOT_LIBRARY_PATH, component: { render: () => null } }
        ]
      : [{ path: "/", component: { render: () => null } }]
  });
  const push = vi.spyOn(router, "push");
  const wrapper = mount(SnapshotConfigPanel, {
    props: {
      channelId: CHANNEL_ID,
      channelOptions: [option(CHANNEL_ID)],
      canSnapshot: true,
      ...props
    },
    global: { plugins: [router] }
  });
  return { wrapper, router, push };
}

function mountPanel(props: Record<string, unknown> = {}) {
  return mountPanelWithRouter(props).wrapper;
}

describe("SnapshotConfigPanel", () => {
  beforeEach(() => {
    api.createDeviceSnapshotSession.mockReset();
    api.getDeviceSnapshotSession.mockReset();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("下发 2022 图像抓拍配置后轮询会话，把设备上传的图片列出来", async () => {
    api.createDeviceSnapshotSession.mockResolvedValue({ code: 0, message: "", data: session() });
    api.getDeviceSnapshotSession.mockResolvedValue({
      code: 0,
      message: "",
      data: session({
        state: "completed",
        receivedCount: 2,
        files: [file("shot-1.jpg"), file("shot-2.jpg")]
      })
    });

    const wrapper = mountPanel();
    await flushPromises();
    expect(wrapper.get("[data-testid='snapshot-status']").text()).toBe("配置后由设备上传 JPEG");

    await wrapper.get("[data-testid='snapshot-count']").setValue("2");
    await wrapper.get("[data-testid='snapshot-interval']").setValue("3");
    await wrapper.get("[data-testid='snapshot-submit']").trigger("click");
    await flushPromises();
    await flushPromises();

    // 张数/间隔按整数透传，不是字符串
    expect(api.createDeviceSnapshotSession).toHaveBeenCalledWith(CHANNEL_ID, { snapNum: 2, interval: 3 });
    expect(api.getDeviceSnapshotSession).toHaveBeenCalledWith(CHANNEL_ID, "snap-1");
    expect(wrapper.get("[data-testid='snapshot-status']").text()).toBe("已完成 2/2");
    const images = wrapper.get("[data-testid='snapshot-results']").findAll("img");
    expect(images).toHaveLength(2);
    expect(images[0].attributes("src")).toContain("shot-1.jpg");
    wrapper.unmount();
  });

  it("会话没结束时按间隔继续轮询，直到 completed", async () => {
    vi.useFakeTimers();
    api.createDeviceSnapshotSession.mockResolvedValue({ code: 0, message: "", data: session() });
    api.getDeviceSnapshotSession
      .mockResolvedValueOnce({ code: 0, message: "", data: session({ receivedCount: 1 }) })
      .mockResolvedValueOnce({
        code: 0,
        message: "",
        data: session({ state: "completed", receivedCount: 2, files: [file("shot-1.jpg"), file("shot-2.jpg")] })
      });

    const wrapper = mountPanel();
    await flushPromises();
    await wrapper.get("[data-testid='snapshot-submit']").trigger("click");
    await flushPromises();
    expect(api.getDeviceSnapshotSession).toHaveBeenCalledTimes(1);
    expect(wrapper.get("[data-testid='snapshot-status']").text()).toBe("接收中 1/2");

    await vi.advanceTimersByTimeAsync(800);
    await flushPromises();
    expect(api.getDeviceSnapshotSession).toHaveBeenCalledTimes(2);
    expect(wrapper.get("[data-testid='snapshot-status']").text()).toBe("已完成 2/2");

    // 到终态就停，不空转
    await vi.advanceTimersByTimeAsync(8000);
    await flushPromises();
    expect(api.getDeviceSnapshotSession).toHaveBeenCalledTimes(2);
    wrapper.unmount();
  });

  it("会话失败时展示服务端给的原因，而不是笼统的失败", async () => {
    api.createDeviceSnapshotSession.mockResolvedValue({ code: 0, message: "", data: session() });
    api.getDeviceSnapshotSession.mockResolvedValue({
      code: 0,
      message: "",
      data: session({ state: "failed", receivedCount: 0, error: "设备未响应抓拍请求" })
    });

    const wrapper = mountPanel();
    await flushPromises();
    await wrapper.get("[data-testid='snapshot-submit']").trigger("click");
    await flushPromises();
    await flushPromises();

    expect(wrapper.get("[data-testid='snapshot-status']").text()).toBe("设备未响应抓拍请求");
    expect(wrapper.find("[data-testid='snapshot-results']").exists()).toBe(false);
    wrapper.unmount();
  });

  it("张数越界时不下发，并给出人能读懂的取值范围", async () => {
    const wrapper = mountPanel();
    await flushPromises();

    await wrapper.get("[data-testid='snapshot-count']").setValue("99");
    await flushPromises();
    expect(wrapper.get("[data-testid='snapshot-blocked']").text()).toContain("张数需为 1-10");

    await wrapper.get("[data-testid='snapshot-submit']").trigger("click");
    await flushPromises();
    expect(api.createDeviceSnapshotSession).not.toHaveBeenCalled();
    wrapper.unmount();
  });

  it("目标通道离线时不下发并说明原因", async () => {
    const wrapper = mountPanel({ channelOptions: [option(CHANNEL_ID, false)] });
    await flushPromises();

    expect(wrapper.get("[data-testid='snapshot-blocked']").text()).toBe("目标通道离线，配置无法下发");
    await wrapper.get("[data-testid='snapshot-submit']").trigger("click");
    await flushPromises();
    expect(api.createDeviceSnapshotSession).not.toHaveBeenCalled();
    wrapper.unmount();
  });

  it("没有抓拍权限时给出原因并拦下下发", async () => {
    const wrapper = mountPanel({ canSnapshot: false });
    await flushPromises();

    expect(wrapper.get("[data-testid='snapshot-blocked']").text()).toBe("当前账号没有图像抓拍权限");
    await wrapper.get("[data-testid='snapshot-submit']").trigger("click");
    await flushPromises();
    expect(api.createDeviceSnapshotSession).not.toHaveBeenCalled();
    wrapper.unmount();
  });

  it("没选通道时给出空态，也不读任何会话", async () => {
    const wrapper = mountPanel({ channelId: null, channelOptions: [] });
    await flushPromises();

    expect(wrapper.get("[data-testid='snapshot-blocked']").text()).toBe("该设备下暂无通道，无法下发抓拍配置");
    expect(api.createDeviceSnapshotSession).not.toHaveBeenCalled();
    expect(api.getDeviceSnapshotSession).not.toHaveBeenCalled();
    wrapper.unmount();
  });

  it("换通道后清掉上一台的会话与结果，不残留", async () => {
    api.createDeviceSnapshotSession.mockResolvedValue({ code: 0, message: "", data: session() });
    api.getDeviceSnapshotSession.mockResolvedValue({
      code: 0,
      message: "",
      data: session({ state: "completed", receivedCount: 2, files: [file("shot-1.jpg"), file("shot-2.jpg")] })
    });

    const wrapper = mountPanel();
    await flushPromises();
    await wrapper.get("[data-testid='snapshot-submit']").trigger("click");
    await flushPromises();
    await flushPromises();
    expect(wrapper.find("[data-testid='snapshot-results']").exists()).toBe(true);

    await wrapper.setProps({ channelId: 2, channelOptions: [option(2)] });
    await flushPromises();

    expect(wrapper.find("[data-testid='snapshot-results']").exists()).toBe(false);
    expect(wrapper.get("[data-testid='snapshot-status']").text()).toBe("配置后由设备上传 JPEG");
    wrapper.unmount();
  });

  it("下发请求失败时把服务端文案露出来", async () => {
    api.createDeviceSnapshotSession.mockRejectedValue(new Error("通道不支持抓拍配置"));
    const wrapper = mountPanel();
    await flushPromises();

    await wrapper.get("[data-testid='snapshot-submit']").trigger("click");
    await flushPromises();

    expect(wrapper.get("[data-testid='snapshot-error']").text()).toBe("通道不支持抓拍配置");
    wrapper.unmount();
  });

  it("卸载后停止轮询", async () => {
    vi.useFakeTimers();
    api.createDeviceSnapshotSession.mockResolvedValue({ code: 0, message: "", data: session() });
    api.getDeviceSnapshotSession.mockResolvedValue({ code: 0, message: "", data: session({ receivedCount: 1 }) });

    const wrapper = mountPanel();
    await flushPromises();
    await wrapper.get("[data-testid='snapshot-submit']").trigger("click");
    await flushPromises();
    expect(api.getDeviceSnapshotSession).toHaveBeenCalledTimes(1);

    wrapper.unmount();
    await vi.advanceTimersByTimeAsync(8000);
    await flushPromises();
    expect(api.getDeviceSnapshotSession).toHaveBeenCalledTimes(1);
  });

  it("会话开始后给出图像库入口，并把会话 id 带进 URL", async () => {
    api.createDeviceSnapshotSession.mockResolvedValue({ code: 0, message: "", data: session() });
    api.getDeviceSnapshotSession.mockResolvedValue({ code: 0, message: "", data: session({ receivedCount: 1 }) });

    const { wrapper, push } = mountPanelWithRouter();
    await flushPromises();
    // 还没下发时没有可回看的东西，入口不该先摆出来。
    expect(wrapper.find("[data-testid='snapshot-library-open']").exists()).toBe(false);

    await wrapper.get("[data-testid='snapshot-submit']").trigger("click");
    await flushPromises();

    const open = wrapper.get("[data-testid='snapshot-library-open']");
    expect(open.attributes("disabled")).toBeUndefined();
    await open.trigger("click");

    // 会话只活在内存里，刷新即丢；把 sessionId 写进 URL，图像库才聚得起这一批图。
    expect(push).toHaveBeenCalledWith({
      path: SNAPSHOT_LIBRARY_PATH,
      query: { sessionId: "snap-1", channelCode: "0411212755" }
    });
    wrapper.unmount();
  });

  it("图像库菜单还没生效时置灰入口并说明原因，而不是跳到不存在的路由", async () => {
    api.createDeviceSnapshotSession.mockResolvedValue({ code: 0, message: "", data: session() });
    api.getDeviceSnapshotSession.mockResolvedValue({ code: 0, message: "", data: session({ receivedCount: 1 }) });

    const { wrapper, push } = mountPanelWithRouter({}, false);
    await flushPromises();
    await wrapper.get("[data-testid='snapshot-submit']").trigger("click");
    await flushPromises();

    const open = wrapper.get("[data-testid='snapshot-library-open']");
    expect(open.attributes("disabled")).toBeDefined();
    expect(wrapper.get("[data-testid='snapshot-library-blocked']").text()).toContain("后端重启");

    await open.trigger("click");
    expect(push).not.toHaveBeenCalled();
    wrapper.unmount();
  });
});

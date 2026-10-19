import { readFile } from "node:fs/promises";
import { join } from "node:path";
import { flushPromises, mount } from "@vue/test-utils";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import * as maplibreMock from "maplibre-gl";
import type { MapCluster } from "./api";
import DeviceMgmt from "./index.vue";

// maplibre 替身暴露的实例登记处(见下方 vi.mock)。
// 类型上不存在,只能绕过;这些登记处让"地图点位是否真的画出来"变成可断言的事实。
const mapInstances = (maplibreMock as unknown as { __mapInstances: any[] }).__mapInstances;
const markerInstances = (maplibreMock as unknown as { __markerInstances: any[] }).__markerInstances;

// 一个真实形态的带坐标通道(取自 220 开发库:通道 34020000001320000010,坐标由位置订阅回写)。
// 它落在济南,不在默认视野(北京 zoom10)内 —— 正是"视野外数据看不到"的复现条件。
const coordinatedChannel = {
  id: 3539,
  channelId: "34020000001320000010",
  name: "后置摄像头",
  latitude: 36.662311,
  longitude: 116.995036,
  status: 1,
  positionSource: "mobile"
};

// cross-review round-2 修复回归测试:
// #3 地图兜底定时器销毁清理 / #5 统计只拉当前资产类型 / #6 统计单失败隔离

const storage = new Map<string, string>();
Object.defineProperty(window, "localStorage", {
  configurable: true,
  value: {
    get length() {
      return storage.size;
    },
    clear: () => storage.clear(),
    getItem: (key: string) => storage.get(key) ?? null,
    key: (index: number) => [...storage.keys()][index] ?? null,
    removeItem: (key: string) => storage.delete(key),
    setItem: (key: string, value: string) => storage.set(key, String(value))
  }
});

const api = vi.hoisted(() => ({
  batchDeleteChannels: vi.fn(),
  batchDeleteDevices: vi.fn(),
  removeDevicesFromGroup: vi.fn(),
  createDevice: vi.fn(),
  deleteChannel: vi.fn(),
  deleteDevice: vi.fn(),
  getChannel: vi.fn(),
  getChannelTimeline: vi.fn(),
  getDevice: vi.fn(),
  listChannelMounts: vi.fn(),
  listChannels: vi.fn(),
  listDevices: vi.fn(),
  listDeviceStatusEvents: vi.fn(),
  listMapClusters: vi.fn(),
  listMapMarkers: vi.fn(),
  listDeviceSubscriptions: vi.fn(),
  listMaintenanceOperations: vi.fn(),
  listFirmwareUpgrades: vi.fn(),
  refreshDeviceCatalog: vi.fn(),
  updateCloudRecording: vi.fn(),
  updateChannelStreamTransport: vi.fn(),
  updateChannel: vi.fn(),
  updateDevice: vi.fn(),
  stopPlay: vi.fn()
}));

const user = vi.hoisted(() => ({
  account: { permissions: ["gb28181:device:view"] as string[] }
}));

vi.mock("./api", () => api);

vi.mock("maplibre-gl", () => {
  // 实例登记处:地图"首轮不按视野过滤 + 拿到数据自动定位"这条链路要能断言,
  // 否则它整段都是测试盲区(历史上 FakeMap 连 getBounds/fitBounds 都没有)。
  const mapInstances: any[] = [];
  const markerInstances: any[] = [];
  class FakeMap {
    // 可调:用例需要 zoom≥阈值 才能验证"标记点/聚合气泡互斥"
    zoomValue = 12;
    onceCbs: Record<string, (() => void) | undefined> = {};
    onCbs: Record<string, (() => void) | undefined> = {};
    fitBoundsCalls: Array<{ options: Record<string, unknown> }> = [];
    flyToCalls: Array<{ center: [number, number]; zoom: number }> = [];
    /** 相机"刚好框住某个包围盒"时给出的缩放。undefined 模拟算不出来。 */
    cameraForBoundsZoom: number | undefined = 15.4;
    constructor() {
      mapInstances.push(this);
    }
    addControl() {
      return this;
    }
    // 只同步触发 load:让兜底定时器在挂载后立即存在;idle 不触发,
    // 保持 mapFirstRender=false 以便验证 12s 兜底路径
    once(evt: string, cb: () => void) {
      this.onceCbs[evt] = cb;
      if (evt === "load") cb();
    }
    on(evt: string, cb: () => void) {
      this.onCbs[evt] = cb;
    }
    off() {}
    remove() {}
    getZoom() {
      return this.zoomValue;
    }
    getBounds() {
      return {
        getSouth: () => 39.3,
        getNorth: () => 40.5,
        getWest: () => 115.4,
        getEast: () => 117.4
      };
    }
    fitBounds(_bounds: unknown, options: Record<string, unknown>) {
      this.fitBoundsCalls.push({ options });
    }
    cameraForBounds() {
      if (this.cameraForBoundsZoom === undefined) return undefined;
      return { center: { lng: 116.995, lat: 36.6623 }, zoom: this.cameraForBoundsZoom };
    }
    // 真实 MapLibre 在飞行动画结束后发 moveend。这里同步发,
    // "点击聚合 → 放大 → 重绘"整条链路才能在用例里跑完 —— 否则点击之后的
    // 行为全是盲区(历史上正是这条链路出问题:"点了这个数字之后点就没了")。
    flyTo(options: { center: [number, number]; zoom: number }) {
      this.flyToCalls.push(options);
      this.zoomValue = options.zoom;
      this.onCbs.moveend?.();
    }
    resize() {}
  }
  class FakeMarker {
    element: HTMLElement;
    constructor(options: { element: HTMLElement }) {
      this.element = options.element;
    }
    setLngLat() {
      return this;
    }
    addTo() {
      // 只有真正挂上地图才算"在地图上"。remove() 里摘掉 —— 这样
      // __markerInstances 表达的是**当前**的标记集合,而不是"历史上创建过的
      // 所有标记"。否则断言会读到已被移除的残留(上一版 remove() 是空实现,
      // 于是"互斥"这类断言其实一直在拿历史数据比对)。
      if (!markerInstances.includes(this)) markerInstances.push(this);
      return this;
    }
    remove() {
      const index = markerInstances.indexOf(this);
      if (index >= 0) markerInstances.splice(index, 1);
    }
  }
  return {
    default: { Map: FakeMap, NavigationControl: class {}, AttributionControl: class {} },
    LngLatBounds: class {
      extend() {}
    },
    Marker: FakeMarker,
    __mapInstances: mapInstances,
    __markerInstances: markerInstances
  };
});

vi.mock("@visactor/vchart", () => ({
  default: class {
    renderSync() {}
    release() {}
    resize() {}
    on() {}
  }
}));

const routeQuery = vi.hoisted(() => ({ returnKey: undefined as string | undefined }));
vi.mock("vue-router", () => ({
  useRoute: () => ({ query: routeQuery, params: {}, path: "/gb28181/devices" }),
  useRouter: () => ({ push: vi.fn(), replace: vi.fn() }),
  createRouter: () => ({
    install: vi.fn(),
    push: vi.fn(),
    replace: vi.fn(),
    beforeEach: vi.fn(),
    afterEach: vi.fn(),
    onError: vi.fn(),
    addRoute: vi.fn(),
    getRoutes: () => [],
    currentRoute: { value: { query: {} } }
  }),
  createWebHashHistory: () => ({})
}));

vi.mock("@/api/dictionary", () => ({
  getDictItemsByDictCodeAPI: vi.fn().mockResolvedValue({ code: 0, message: "", data: [] })
}));

vi.mock("@/store/modules/theme-config", async () => {
  const { ref } = await import("vue");
  return { useThemeConfig: () => ({ darkMode: ref(false) }) };
});

vi.mock("@/store/modules/user", () => ({
  useUserStoreHook: () => user
}));

vi.mock("@/store/modules/playback-console", () => ({
  usePlaybackConsoleStore: () => ({ open: vi.fn(), close: vi.fn() })
}));

vi.mock("@arco-design/web-vue", () => ({
  Message: { success: vi.fn(), error: vi.fn(), warning: vi.fn(), info: vi.fn() },
  Modal: { confirm: vi.fn() }
}));

function okList(total = 0, list: unknown[] = []) {
  return { code: 0, message: "", data: { total, list } };
}

function lastDevicePageRequest() {
  return api.listDevices.mock.calls
    .map(([params]) => params)
    .filter(params => params.pageSize !== 1)
    .at(-1);
}

/**
 * 读页面源码本身。用于"样式挂在哪"这类只能从源码判断的断言(运行时拿不到
 * scoped 编译结果)。兼容从仓库根或 web/ 启动 vitest 两种 cwd。
 */
async function readPageSource() {
  for (const prefix of ["", "web/"]) {
    try {
      return await readFile(join(process.cwd(), `${prefix}src/views/gb28181/device-mgmt/index.vue`), "utf8");
    } catch {
      // 换个候选前缀继续
    }
  }
  throw new Error("找不到 index.vue,无法校验样式作用域");
}

/**
 * 聚合气泡的夹具。默认两路通道 —— **count>1 才叫聚合**。
 * count===1 走的是"落单通道直接画标记点"那条分支,不该出现在气泡用例里。
 */
function clusterOf(overrides: Partial<MapCluster> & Pick<MapCluster, "centerLat" | "centerLng">): MapCluster {
  return {
    count: 2,
    onlineCount: 2,
    onlineRate: 1,
    minLat: overrides.centerLat,
    maxLat: overrides.centerLat,
    minLng: overrides.centerLng,
    maxLng: overrides.centerLng,
    single: null,
    ...overrides
  };
}

// 未注册的全局组件(unplugin-vue-components 注入)在测试环境不会渲染 slot,
// 统一透传 slot 让模板内容可断言
const passthroughStub = {
  template: "<div><slot /><slot name='fields' /><slot name='title' /><slot name='footer' /></div>"
};

function mountPage() {
  return mount(DeviceMgmt, {
    shallow: true,
    global: {
      stubs: {
        DirectoryPanel: {
          name: "DirectoryPanel",
          props: ["modelValue", "resourceFilter", "selectedDeviceId"],
          emits: ["select", "deviceSelect", "clearSelection", "update:modelValue", "viewChange"],
          setup(props: any, { emit }: { emit: (event: string, ...args: any[]) => void }) {
            const chooseView = (view: string) => {
              emit("update:modelValue", {
                ...props.modelValue,
                view,
                listReset: { page: 1, clearSelection: true }
              });
              emit("viewChange", view);
            };
            const chooseDirectory = () => {
              const node = {
                key: `${props.modelValue.view}:test-root`,
                name: "测试目录",
                type: "area",
                count: 12,
                onlineCount: 8,
                depth: 1,
                children: []
              };
              emit("update:modelValue", {
                ...props.modelValue,
                selectedKey: { ...props.modelValue.selectedKey, [props.modelValue.view]: node.key },
                listReset: { page: 1, clearSelection: true }
              });
              emit("select", node);
            };
            return {
              refresh: vi.fn(),
              sync: vi.fn(),
              chooseView,
              chooseDirectory,
              chooseDevice: () =>
                emit("deviceSelect", {
                  id: 31,
                  name: "园区 NVR",
                  alias: "",
                  deviceId: "34020000002000000031",
                  online: true,
                  status: 1,
                  channelCount: 4,
                  channelOnlineCount: 3
                })
            };
          },
          template: `<section>
            <button data-testid="select-device-a" @click="chooseDevice">目录内设备 A</button>
            <button data-testid="select-directory" @click="chooseDirectory">选择目录</button>
            <button data-testid="directory-administrative" @click="chooseView('administrative')">行政区划</button>
            <button data-testid="directory-department" @click="chooseView('department')">部门目录</button>
            <button data-testid="directory-business" @click="chooseView('business')">业务分组</button>
          </section>`
        },
        SubscriptionDialog: true,
        CustomGroupEditor: true,
        AddToGroupDialog: true,
        "s-layout-search": passthroughStub,
        "a-table": passthroughStub,
        "a-table-column": passthroughStub,
        "a-drawer": passthroughStub,
        "a-modal": passthroughStub,
        "a-form": passthroughStub,
        "a-form-item": passthroughStub,
        "a-input": passthroughStub,
        "a-input-password": passthroughStub,
        "a-button": { props: ["disabled"], template: `<button :disabled="disabled"><slot /></button>` },
        "a-select": {
          props: ["modelValue"],
          emits: ["update:modelValue"],
          template: `<div><button type="button" data-testid="select-online" @click="$emit('update:modelValue', 'online')">在线</button><slot /></div>`
        },
        "a-option": passthroughStub,
        "a-doption": { props: ["disabled"], template: `<button :disabled="disabled"><slot /></button>` },
        "a-pagination": {
          props: ["current", "pageSize", "total", "pageSizeOptions"],
          emits: ["change", "pageSizeChange"],
          template: `<div class="stub-pagination" :data-current="current" :data-page-size="pageSize" :data-page-size-options="pageSizeOptions?.join(',')">
            <button type="button" data-testid="go-page-10" @click="$emit('change', 10)">10</button>
          </div>`
        },
        "a-tooltip": passthroughStub,
        "a-dropdown": { template: "<div><slot /><slot name='content' /></div>" },
        "a-spin": passthroughStub,
        "a-empty": passthroughStub,
        "a-switch": passthroughStub,
        "a-slider": passthroughStub,
        "a-timeline": passthroughStub,
        "a-timeline-item": passthroughStub,
        "a-link": passthroughStub,
        "a-image": passthroughStub
      }
    }
  });
}

describe("device-mgmt round-2 修复回归", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    routeQuery.returnKey = undefined;
    user.account.permissions = ["gb28181:device:view"];
    // happy-dom 元素无布局尺寸:容器永远 0 宽会让 ensureMap 无限 rAF 重试
    Object.defineProperty(HTMLElement.prototype, "clientWidth", { get: () => 800, configurable: true });
    Object.defineProperty(HTMLElement.prototype, "clientHeight", { get: () => 600, configurable: true });
    // 登记处跨用例累积,必须清空
    mapInstances.length = 0;
    markerInstances.length = 0;
    api.listDevices.mockReset().mockResolvedValue(okList());
    api.getDevice.mockReset().mockResolvedValue({
      code: 0,
      data: {
        id: 31,
        name: "园区 NVR",
        alias: "",
        deviceId: "34020000002000000031",
        online: true,
        status: 1,
        channelCount: 4,
        channelOnlineCount: 3
      }
    });
    api.listChannels.mockReset().mockResolvedValue(okList());
    api.listMapMarkers.mockReset().mockResolvedValue(okList());
    api.listMapClusters.mockReset().mockResolvedValue({ code: 0, message: "", data: { clusters: [] } });
    api.listDeviceStatusEvents.mockReset().mockResolvedValue(okList());
    api.listDeviceSubscriptions.mockReset().mockResolvedValue(okList());
    api.listMaintenanceOperations.mockReset().mockResolvedValue(okList());
    api.listFirmwareUpgrades.mockReset().mockResolvedValue(okList());
  });

  afterEach(() => {
    window.localStorage.clear();
    vi.useRealTimers();
    vi.unstubAllGlobals();
  });

  it("列表不显示工作区提示，选择设备后保留设备信息和操作", async () => {
    const wrapper = mountPage();
    await flushPromises();
    expect(wrapper.find(".channel-workspace-header").exists()).toBe(false);
    await wrapper.get('[data-asset-kind="channel"]').trigger("click");
    await flushPromises();
    expect(wrapper.find(".channel-workspace-header").exists()).toBe(false);
    await wrapper.get('[data-testid="select-device-a"]').trigger("click");
    await flushPromises();
    const header = wrapper.get(".channel-workspace-header");
    expect(header.text()).toContain("园区 NVR");
    expect(header.text()).toContain("设备详情");
    await wrapper.get('[data-asset-kind="device"]').trigger("click");
    await flushPromises();
    expect(wrapper.find(".channel-workspace-header").exists()).toBe(false);
    wrapper.unmount();
  });

  it("keeps frequent device actions visible and secondary actions in the more menu", async () => {
    const wrapper = mountPage();
    await flushPromises();
    await wrapper.get('[data-testid="select-device-a"]').trigger("click");
    await flushPromises();
    const actions = wrapper.get(".device-context-actions");
    expect(actions.findAll(".device-action").map(button => button.text())).toEqual(["设备详情", "刷新目录", "运行监控", "更多"]);
    expect(actions.find('[aria-label="设备更多操作"]').exists()).toBe(true);
    expect(actions.findAll("button:not(.device-action)").map(button => button.text())).toEqual([
      "订阅管理",
      "编辑设备",
      "固件升级",
      "设备重启",
      "维护记录",
      "删除设备"
    ]);
    expect(
      actions
        .findAll("button")
        .find(button => button.text() === "删除设备")!
        .attributes("disabled")
    ).toBeDefined();
    wrapper.unmount();
  });

  it("没有历史偏好时默认选中卡片视图", async () => {
    api.listDevices.mockResolvedValue(okList(32, []));
    const wrapper = mountPage();
    await flushPromises();

    const cardButton = wrapper.get('[aria-label="卡片视图"]');
    expect(cardButton.attributes("aria-pressed")).toBe("true");
    expect(wrapper.find(".card-view").exists()).toBe(true);
    expect(lastDevicePageRequest()).toMatchObject({ page: 1, pageSize: 9 });
    expect(lastDevicePageRequest()).not.toHaveProperty("sort");
    expect(wrapper.get(".card-pagination").attributes("data-page-size-options")).toContain("9");
    wrapper.unmount();
  });

  it("keeps device asset type while switching directory views and selecting directory nodes", async () => {
    const wrapper = mountPage();
    await flushPromises();
    const channelCallsBefore = api.listChannels.mock.calls.length;

    for (const view of ["department", "business", "administrative"]) {
      await wrapper.get(`[data-testid="directory-${view}"]`).trigger("click");
      await flushPromises();
      expect(wrapper.get('[data-asset-kind="device"]').classes()).toContain("active");
      expect(lastDevicePageRequest()).toMatchObject({ page: 1, pageSize: 9 });
    }

    await wrapper.get('[data-testid="select-directory"]').trigger("click");
    await flushPromises();
    expect(wrapper.get('[data-asset-kind="device"]').classes()).toContain("active");
    expect(lastDevicePageRequest()).toMatchObject({
      directoryView: "administrative",
      directoryKey: "administrative:test-root"
    });
    expect(api.listChannels).toHaveBeenCalledTimes(channelCallsBefore);

    await wrapper.get('[data-testid="select-device-a"]').trigger("click");
    await flushPromises();
    expect(wrapper.get('[data-asset-kind="channel"]').classes()).toContain("active");
    expect(api.listChannels.mock.calls.at(-1)?.[0]).toMatchObject({ deviceId: "34020000002000000031" });
    wrapper.unmount();
  });

  it("returns from a device card to the exact device-list page and filters", async () => {
    api.listDevices.mockResolvedValue(
      okList(100, [
        {
          id: 31,
          deviceId: "34020000002000000031",
          name: "园区 NVR",
          online: true,
          status: 1,
          channelCount: 4,
          channelOnlineCount: 3
        }
      ])
    );
    const wrapper = mountPage();
    await flushPromises();

    await wrapper.get('[aria-label="搜索设备名称或通道名称或编码"]').setValue("门口");
    await wrapper.get('[aria-label="搜索设备名称或通道名称或编码"]').trigger("keydown.enter");
    await wrapper.get('[data-testid="select-online"]').trigger("click");
    await flushPromises();
    await wrapper.get('[data-testid="directory-department"]').trigger("click");
    await flushPromises();
    await wrapper.get('[data-testid="select-directory"]').trigger("click");
    await flushPromises();
    await wrapper.get('[data-testid="go-page-10"]').trigger("click");
    await flushPromises();

    expect(lastDevicePageRequest()).toMatchObject({
      q: "门口",
      status: "online",
      directoryView: "department",
      directoryKey: "department:test-root",
      page: 10,
      pageSize: 9
    });
    await wrapper.get("article.device-summary-card").trigger("dblclick");
    await flushPromises();
    expect(wrapper.find('[aria-label="返回设备列表"]').exists()).toBe(true);
    const returnChip = wrapper.get('.filter-chips [aria-label="返回设备列表"]');
    expect(returnChip.classes()).toContain("device-drilldown-chip");
    expect(wrapper.find('[aria-label="清除所属设备筛选"]').exists()).toBe(false);
    expect(wrapper.find(".clear-all").exists()).toBe(false);
    expect(api.listChannels.mock.calls.at(-1)?.[0]).toMatchObject({ deviceId: "34020000002000000031" });

    await returnChip.trigger("click");
    await flushPromises();
    await vi.advanceTimersByTimeAsync(500);
    await flushPromises();
    expect(wrapper.get('[data-asset-kind="device"]').classes()).toContain("active");
    expect(lastDevicePageRequest()).toMatchObject({
      q: "门口",
      status: "online",
      directoryView: "department",
      directoryKey: "department:test-root",
      page: 10,
      pageSize: 9
    });
    expect(wrapper.get('[data-testid="directory-department"]')).toBeTruthy();
    wrapper.unmount();
  });

  it("clearing channel filters after device drilldown returns to the original device page", async () => {
    api.listDevices.mockResolvedValue(
      okList(100, [{ id: 31, deviceId: "34020000002000000031", name: "园区 NVR", online: true }])
    );
    const wrapper = mountPage();
    await flushPromises();
    await wrapper.get('[data-testid="go-page-10"]').trigger("click");
    await flushPromises();
    await wrapper.get("article.device-summary-card").trigger("dblclick");
    await flushPromises();
    await wrapper.get('[data-testid="select-online"]').trigger("click");
    await flushPromises();
    await wrapper.get(".clear-all").trigger("click");
    await flushPromises();
    expect(wrapper.get('[data-asset-kind="device"]').classes()).toContain("active");
    expect(lastDevicePageRequest()).toMatchObject({ page: 10, pageSize: 9 });
    wrapper.unmount();
  });

  it("discards a pending device-list return snapshot when the user changes the presentation view", async () => {
    api.listDevices.mockResolvedValue(
      okList(1, [
        {
          id: 31,
          deviceId: "34020000002000000031",
          name: "园区 NVR",
          online: true,
          status: 1,
          channelCount: 4,
          channelOnlineCount: 3
        }
      ])
    );
    const wrapper = mountPage();
    await flushPromises();
    await wrapper.get("article.device-summary-card").trigger("dblclick");
    await flushPromises();
    expect(wrapper.find('[aria-label="返回设备列表"]').exists()).toBe(true);

    await wrapper.get('[aria-label="列表视图"]').trigger("click");
    await flushPromises();
    expect(wrapper.find('[aria-label="返回设备列表"]').exists()).toBe(false);
    wrapper.unmount();
  });

  it("满在线设备的通道进度条使用绿色填充状态", async () => {
    api.listDevices.mockResolvedValue(
      okList(1, [
        {
          id: 31,
          deviceId: "34020000002000000031",
          name: "园区 NVR",
          online: true,
          status: 1,
          channelCount: 2,
          channelOnlineCount: 2
        }
      ])
    );

    const wrapper = mountPage();
    await flushPromises();

    const progressBar = wrapper.get(".device-card-channel .progress-bar");
    expect(progressBar.classes()).toContain("healthy");
    expect(progressBar.get(".progress-fill").attributes("style")).toContain("width: 100%");
    wrapper.unmount();
  });

  it("keeps device and channel lists available while directory selection opens the device channel context", async () => {
    const wrapper = mountPage();
    await flushPromises();

    expect(wrapper.find(".asset-kind-switch").exists()).toBe(true);
    expect(wrapper.find('[data-asset-kind="device"]').exists()).toBe(true);
    expect(api.listDevices).toHaveBeenCalled();
    await wrapper.get('[data-asset-kind="channel"]').trigger("click");
    await flushPromises();
    expect(api.listChannels).toHaveBeenCalled();

    expect(wrapper.find('[data-testid="select-device-a"]').exists()).toBe(true);
    await wrapper.get('[data-testid="select-device-a"]').trigger("click");
    await flushPromises();
    expect(api.listChannels).toHaveBeenLastCalledWith(expect.objectContaining({ deviceId: "34020000002000000031" }));
    expect(wrapper.text()).toContain("园区 NVR");

    await wrapper.get('[aria-label="清除所属设备筛选"]').trigger("click");
    await flushPromises();
    expect(api.listChannels.mock.calls.some(([params]) => !params.deviceId && !params.directoryKey && !params.status)).toBe(true);
  });

  it("renders channel cards as visible content rather than inert templates", async () => {
    api.listChannels.mockResolvedValue(
      okList(1, [{ id: 1, channelId: "channel-a", deviceId: "device-a", name: "入口通道", status: 1 }])
    );
    const wrapper = mountPage();
    await flushPromises();
    await wrapper.get('[data-asset-kind="channel"]').trigger("click");
    await flushPromises();
    expect(wrapper.findAll("article.channel-summary-card")).toHaveLength(1);
    expect(wrapper.get("article.channel-summary-card").text()).toContain("入口通道");
    expect(wrapper.find(".card-grid > template").exists()).toBe(false);
    wrapper.unmount();
  });

  it("forwards the selected device to both map endpoints", async () => {
    window.localStorage.setItem("uvp.gb28181.device-mgmt.view-mode", "map");
    const wrapper = mountPage();
    await flushPromises();
    await wrapper.get('[data-testid="select-device-a"]').trigger("click");
    await flushPromises();
    expect(api.listMapMarkers).toHaveBeenLastCalledWith(expect.objectContaining({ deviceId: "34020000002000000031" }));
    expect(api.listMapClusters).toHaveBeenLastCalledWith(expect.objectContaining({ deviceId: "34020000002000000031" }));
    wrapper.unmount();
  });

  it("ignores late channel responses after changing device scope", async () => {
    const wrapper = mountPage();
    await flushPromises();
    let resolveOld!: (value: any) => void;
    api.listChannels.mockImplementation((params: any) =>
      params.deviceId && !params.status
        ? new Promise(resolve => {
            resolveOld = resolve;
          })
        : Promise.resolve(okList(1, [{ id: 2, channelId: "new-channel", name: "全部通道结果" }]))
    );
    await wrapper.get('[data-testid="select-device-a"]').trigger("click");
    await wrapper.get('[aria-label="清除所属设备筛选"]').trigger("click");
    await flushPromises();
    resolveOld(okList(1, [{ id: 1, channelId: "old-channel", name: "迟到的设备通道" }]));
    await flushPromises();
    expect(wrapper.text()).toContain("全部通道结果");
    expect(wrapper.text()).not.toContain("迟到的设备通道");
    wrapper.unmount();
  });

  it("keeps device and department filtering mutually exclusive", async () => {
    const wrapper = mountPage();
    await flushPromises();
    await wrapper.get('[data-testid="select-device-a"]').trigger("click");
    await flushPromises();
    const directory = wrapper.findComponent({ name: "DirectoryPanel" });
    directory.vm.$emit("update:modelValue", {
      view: "department",
      selectedKey: { administrative: null, department: "department:dept:3", business: null }
    });
    directory.vm.$emit("select", { key: "department:dept:3", name: "园区", type: "organization", readOnly: true });
    await flushPromises();
    expect(api.listChannels).toHaveBeenLastCalledWith(
      expect.objectContaining({ directoryView: "department", directoryKey: "department:dept:3" })
    );
    expect(api.listChannels.mock.calls.at(-1)?.[0].deviceId).toBeUndefined();
    directory.vm.$emit("deviceSelect", {
      id: 31,
      name: "园区 NVR",
      alias: "",
      deviceId: "34020000002000000031",
      online: true
    });
    await flushPromises();
    expect(api.listChannels).toHaveBeenLastCalledWith(expect.objectContaining({ deviceId: "34020000002000000031" }));
    expect(api.listChannels.mock.calls.at(-1)?.[0].directoryKey).toBeUndefined();
    await wrapper.get('[aria-label="清除所属设备筛选"]').trigger("click");
    await flushPromises();
    expect(api.listChannels.mock.calls.some(([params]) => !params.deviceId && !params.directoryKey && !params.status)).toBe(true);
    wrapper.unmount();
  });

  it.each(["national", "custom"] as const)("maps legacy %s return snapshots to administrative", async directoryView => {
    routeQuery.returnKey = "device-return";
    window.sessionStorage.setItem(
      "uvp.device-record-return:device-return",
      JSON.stringify({
        version: 1,
        viewMode: "list",
        assetKind: "channel",
        navigationMode: "device",
        selectedDeviceId: 31,
        deviceIdFilter: "34020000002000000031",
        keyword: "入口",
        directoryView,
        directorySelectedKey: `${directoryView}:legacy-selection`,
        page: 2,
        listPageSize: 20,
        cardPageSize: 12
      })
    );
    api.getDevice.mockResolvedValue({
      code: 0,
      data: { id: 31, deviceId: "34020000002000000031", name: "返回的设备", online: true }
    });
    const wrapper = mountPage();
    await flushPromises();
    const directory = wrapper.findComponent({ name: "DirectoryPanel" });
    expect(directory.props("modelValue").view).toBe("administrative");
    expect(directory.props("modelValue").selectedKey.administrative).toBeNull();
    expect(api.getDevice).toHaveBeenCalledWith(31);
    expect(wrapper.text()).toContain("返回的设备");
    expect(api.listChannels).toHaveBeenCalledWith(
      expect.objectContaining({ deviceId: "34020000002000000031", q: "入口", page: 2, pageSize: 20 })
    );
    expect(wrapper.find(".asset-kind-switch").exists()).toBe(true);
    wrapper.unmount();
  });

  it("保留用户已保存的列表视图偏好", async () => {
    window.localStorage.setItem("uvp.gb28181.device-mgmt.view-mode", "list");
    const wrapper = mountPage();
    await flushPromises();

    expect(wrapper.get('[aria-label="列表视图"]').attributes("aria-pressed")).toBe("true");
    expect(wrapper.find(".table-view").exists()).toBe(true);
    wrapper.unmount();
  });

  it("defaults to the device list and refreshes statistics for the active asset type", async () => {
    const wrapper = mountPage();
    await flushPromises();

    expect(api.listDevices).toHaveBeenCalledTimes(3); // 主列表 1 + 统计 online/offline 2
    expect(api.listChannels).not.toHaveBeenCalled();
    expect(wrapper.find(".device-management-entry").exists()).toBe(false);
    expect(wrapper.text()).not.toContain("设备管理");
    wrapper.unmount();
  });

  it("includes the current search in online/offline statistics", async () => {
    const wrapper = mountPage();
    await flushPromises();
    await wrapper.get('[data-asset-kind="channel"]').trigger("click");
    await flushPromises();
    api.listChannels.mockClear();

    const search = wrapper.get('[aria-label="搜索设备名称或通道名称或编码"]');
    await search.setValue("门岗");
    await search.trigger("keydown.enter");
    await flushPromises();

    expect(api.listChannels).toHaveBeenCalledWith(expect.objectContaining({ status: "online", q: "门岗" }));
    expect(api.listChannels).toHaveBeenCalledWith(expect.objectContaining({ status: "offline", q: "门岗" }));
    wrapper.unmount();
  });

  it("#6 单个统计请求失败不拖累另一个", async () => {
    // 按 status 参数区分,不依赖调用顺序(主列表无 status)
    api.listChannels.mockImplementation(async (params: { status?: string }) => {
      if (params?.status === "online") throw new Error("online count down");
      if (params?.status === "offline") return okList(7);
      return okList();
    });

    const wrapper = mountPage();
    await flushPromises();
    await wrapper.get('[data-asset-kind="channel"]').trigger("click");
    await flushPromises();

    expect(wrapper.text()).toContain("离线 7");
    wrapper.unmount();
  });

  it("#3a 未销毁时 12s 兜底把底图标记为超时", async () => {
    window.localStorage.setItem("uvp.gb28181.device-mgmt.view-mode", "map");
    const wrapper = mountPage();
    await flushPromises();

    // FakeMap 同步触发 load → 12s 兜底定时器已挂
    expect(vi.getTimerCount()).toBeGreaterThan(0);

    vi.advanceTimersByTime(12000);
    await flushPromises();
    expect(wrapper.text()).toContain("底图加载超时");
    wrapper.unmount();
  });

  it("#3b 销毁地图清除仍挂起的 12s 兜底定时器", async () => {
    window.localStorage.setItem("uvp.gb28181.device-mgmt.view-mode", "map");
    const wrapper = mountPage();
    await flushPromises();

    // 兜底定时器尚挂起时:先推进 800ms,让一次性 resize rAF 与 750ms ring 动画
    // 自然结束,此时仍有地图兜底和自动刷新倒计时两个 timer
    vi.advanceTimersByTime(800);
    await flushPromises();
    expect(vi.getTimerCount()).toBeGreaterThan(1);

    wrapper.unmount();
    // destroyMap 和自动刷新都必须清理,不能留跨实例存活
    expect(vi.getTimerCount()).toBe(0);

    vi.advanceTimersByTime(12000);
    await flushPromises();
    expect(vi.getTimerCount()).toBe(0);
  });

  it("passes top resource filters to the tree and refreshes it manually", async () => {
    const wrapper = mountPage();
    await flushPromises();
    await wrapper.get('[aria-label="搜索设备名称或通道名称或编码"]').setValue(" Gate ");
    await wrapper.get('[data-asset-kind="channel"]').trigger("click");
    await flushPromises();
    const directory = wrapper.findComponent({ name: "DirectoryPanel" });
    expect(directory.props("resourceFilter")).toMatchObject({ assetKind: "channel", q: "Gate" });
    await wrapper.get('[data-testid="refresh-control"]').trigger("click");
    expect(directory.vm.refresh).toHaveBeenCalled();
    wrapper.unmount();
  });

  it("shows a 10-second countdown and refreshes the current view automatically", async () => {
    const wrapper = mountPage();
    await flushPromises();
    await wrapper.get('[data-asset-kind="channel"]').trigger("click");
    await flushPromises();

    const refresh = wrapper.get("[data-testid='refresh-control']");
    expect(refresh.text()).toContain("10s");
    const initialChannelCalls = api.listChannels.mock.calls.length;

    await vi.advanceTimersByTimeAsync(1000);
    expect(refresh.text()).toContain("9s");

    await vi.advanceTimersByTimeAsync(9000);
    await flushPromises();
    expect(api.listChannels.mock.calls.length).toBeGreaterThan(initialChannelCalls);
    expect(wrapper.findComponent({ name: "DirectoryPanel" }).vm.sync).toHaveBeenCalled();
    expect(refresh.text()).toContain("10s");
    wrapper.unmount();
  });

  it("切换设备立即刷新该设备通道统计", async () => {
    const wrapper = mountPage();
    await flushPromises();
    api.listChannels.mockClear();

    await wrapper.get('[data-testid="select-device-a"]').trigger("click");
    await flushPromises();

    // 主列表 1 次 + 统计 online/offline 2 次
    expect(api.listChannels).toHaveBeenCalledTimes(3);
    wrapper.unmount();
  });
  it("opens each device context action directly and preserves upgrade interlock after closing", async () => {
    user.account.permissions = ["*:*:*"];
    window.localStorage.setItem("uvp.gb28181.device-mgmt.view-mode", "card");
    const device = {
      id: 31,
      deviceId: "34020000002000000031",
      name: "北门录像机",
      online: true,
      channelCount: 8,
      channelOnlineCount: 0,
      firmware: "v1",
      effectiveVersion: "2022"
    };
    api.listDevices.mockResolvedValue(okList(1, [device]));
    api.getDevice.mockResolvedValue({ code: 0, data: device });
    const wrapper = mountPage();
    await flushPromises();
    await wrapper.get('[data-testid="select-device-a"]').trigger("click");
    await flushPromises();
    await wrapper
      .findAll("button")
      .find(option => option.text() === "固件升级")!
      .trigger("click");
    await flushPromises();
    const upgrade = wrapper.findComponent({ name: "DeviceFirmwareUpgradeDialog" });
    expect(upgrade.props("visible")).toBe(true);
    const operation = { operationId: "upgrade-31", deviceId: 31, status: "accepted", firmware: "v2" };
    upgrade.vm.$emit("operationUpdated", operation);
    upgrade.vm.$emit("update:visible", false);
    api.listFirmwareUpgrades.mockResolvedValue(okList(1, [operation]));
    await flushPromises();
    expect(wrapper.text()).toContain("升级处理中");
    await wrapper
      .findAll("button")
      .find(option => option.text() === "设备重启")!
      .trigger("click");
    await flushPromises();
    const reboot = wrapper.findComponent({ name: "DeviceRebootDialog" });
    expect(reboot.props("visible")).toBe(true);
    expect(reboot.props("blockedReason")).toContain("升级");
    expect(upgrade.props("visible")).toBe(false);
    reboot.vm.$emit("viewRecords", "reboot-31");
    await flushPromises();
    const records = wrapper.findComponent({ name: "DeviceMaintenanceRecordsDialog" });
    expect(records.props("visible")).toBe(true);
    expect(records.props("initialType")).toBe("reboot");
    expect(records.props("operationId")).toBe("reboot-31");
    expect(reboot.props("visible")).toBe(false);
    wrapper.unmount();
  });

  // ── 地图点位可见性(2026-09-21)───────────────────────────────────────────
  // 现象:只有一个通道带坐标、且该坐标落在默认视野(北京 zoom10)之外时,
  // 地图上什么都看不到。根因是"首轮按视野查询"与"自动定位被 gate 在非空列表上"
  // 构成的死锁:数据在视野外 ⇒ 列表空 ⇒ 不 fit ⇒ 视野不动 ⇒ 数据永远进不了视野。

  it("首轮不按视野过滤,拿到数据后自动定位,随后恢复按视野查询", async () => {
    window.localStorage.setItem("uvp.gb28181.device-mgmt.view-mode", "map");
    api.listMapMarkers.mockReset().mockResolvedValue(okList(1, [coordinatedChannel]));

    const wrapper = mountPage();
    await flushPromises();

    // 首轮查询必须不带视野参数,否则视野外的点位永远查不出来
    const first = api.listMapMarkers.mock.calls[0][0] as Record<string, unknown>;
    expect(first.minLat).toBeUndefined();
    expect(first.maxLat).toBeUndefined();
    expect(first.minLng).toBeUndefined();
    expect(first.maxLng).toBeUndefined();

    // 拿到数据就必须定位,否则点永远进不了视野
    const map = mapInstances[0];
    expect(map.fitBoundsCalls).toHaveLength(1);

    // 定位完成(moveend)后要回到按视野查询,不能每轮都退化成全量
    map.onCbs.moveend?.();
    await flushPromises();
    const calls = api.listMapMarkers.mock.calls;
    const afterFit = calls[calls.length - 1][0] as Record<string, unknown>;
    expect(afterFit.minLat).toBe(39.3);
    expect(afterFit.maxLat).toBe(40.5);
    expect(afterFit.minLng).toBe(115.4);
    expect(afterFit.maxLng).toBe(117.4);

    wrapper.unmount();
  });

  it("单点自动定位钳到街区级,不会一路顶到 maxZoom", async () => {
    window.localStorage.setItem("uvp.gb28181.device-mgmt.view-mode", "map");
    api.listMapMarkers.mockReset().mockResolvedValue(okList(1, [coordinatedChannel]));

    const wrapper = mountPage();
    await flushPromises();

    // 单点视野是退化矩形,不钳制会顶到 mapMaxZoom(22) 直接落到街景级;
    // 但上限又必须高于"单点标记渲染阈值"(14),否则定位完反而看不到点。
    expect(mapInstances[0].fitBoundsCalls[0].options.maxZoom).toBe(16);
    wrapper.unmount();
  });

  it("落单的通道在低 zoom 也直接画标记点,不画写着 1 的气泡", async () => {
    window.localStorage.setItem("uvp.gb28181.device-mgmt.view-mode", "map");
    api.listMapMarkers.mockReset().mockResolvedValue(okList(1, [coordinatedChannel]));
    api.listMapClusters.mockReset().mockResolvedValue({
      code: 0,
      message: "",
      data: {
        clusters: [
          clusterOf({
            centerLat: 36.662311,
            centerLng: 116.995036,
            count: 1,
            onlineCount: 1,
            single: { id: 3539, channelId: "34020000001320000010", name: "后置摄像头", status: 1 }
          })
        ]
      }
    });

    const wrapper = mountPage();
    await flushPromises();
    const map = mapInstances[0];

    map.zoomValue = 12;
    map.onCbs.moveend?.();
    await flushPromises();

    // 行业惯例(Leaflet.markercluster / Supercluster / 高德):count==1 不聚合。
    // 让唯一那路通道顶着一个写着 "1" 的气泡,用户会以为平台聚合了一个不该聚合的东西,
    // 而且点它只会放大 —— 观感就是"点了这个数字之后标记点没了"。
    expect(markerInstances.map(instance => instance.element.className)).toEqual(["map-channel-marker online"]);

    wrapper.unmount();
  });

  it("低 zoom 多路画聚合气泡,高 zoom 只画标记点,两者互斥", async () => {
    window.localStorage.setItem("uvp.gb28181.device-mgmt.view-mode", "map");
    api.listMapMarkers.mockReset().mockResolvedValue(okList(1, [coordinatedChannel]));
    api.listMapClusters.mockReset().mockResolvedValue({
      code: 0,
      message: "",
      data: {
        clusters: [clusterOf({ centerLat: 36.6623, centerLng: 116.995, count: 3, onlineCount: 2, onlineRate: 2 / 3 })]
      }
    });

    const wrapper = mountPage();
    await flushPromises();
    const map = mapInstances[0];

    // 低 zoom:多路通道画聚合气泡
    map.zoomValue = 12;
    map.onCbs.moveend?.();
    await flushPromises();
    expect(markerInstances.map(instance => instance.element.className)).toEqual(["map-cluster-marker"]);

    // 高 zoom:换成通道标记点,不能再叠一层气泡(同屏两种呈现就是"互斥"要挡的事)
    map.zoomValue = 16;
    map.onCbs.moveend?.();
    await flushPromises();
    expect(markerInstances.map(instance => instance.element.className)).toEqual(["map-channel-marker online"]);

    wrapper.unmount();
  });

  it("点击聚合一次放大到标记点层,不会停在气泡层让人以为没反应", async () => {
    window.localStorage.setItem("uvp.gb28181.device-mgmt.view-mode", "map");
    api.listMapMarkers.mockReset().mockResolvedValue(okList(1, [coordinatedChannel]));
    api.listMapClusters.mockReset().mockResolvedValue({
      code: 0,
      message: "",
      data: {
        clusters: [
          clusterOf({
            centerLat: 36.6623,
            centerLng: 116.995,
            count: 4,
            minLat: 36.65,
            maxLat: 36.67,
            minLng: 116.98,
            maxLng: 117.01
          })
        ]
      }
    });

    const wrapper = mountPage();
    await flushPromises();
    const map = mapInstances[0];

    const bubbleAt = async (zoom: number) => {
      map.zoomValue = zoom;
      map.onCbs.moveend?.();
      await flushPromises();
      // remove() 会摘掉上一批标记,所以这里拿到的一定是"当前地图上"的那一个
      expect(markerInstances).toHaveLength(1);
      return markerInstances[0].element as HTMLElement;
    };

    // 相机说"框住这一簇要 17.5"(点很密) —— 就用它。这一条同时证明真的参考了
    // 簇的包围盒,而不是"当前 zoom 加个固定档位"。
    map.cameraForBoundsZoom = 17.5;
    map.flyToCalls.length = 0;
    (await bubbleAt(10)).click();
    expect(map.flyToCalls[0].zoom).toBe(17.5);

    // 相机说"框住它只要 11.2"(点太散,人还在气泡层) —— 必须抬到标记点层以上,
    // 否则用户点一下跟没点一样(就是"点完这个数字标记点就没了"的由来)。
    map.cameraForBoundsZoom = 11.2;
    map.flyToCalls.length = 0;
    (await bubbleAt(10)).click();
    expect(map.flyToCalls[0].zoom).toBeGreaterThanOrEqual(15);

    // 飞过去之后必须真的画出标记点,而不是又一轮气泡或干脆什么都没有
    await flushPromises();
    expect(markerInstances.map(instance => instance.element.className)).toEqual(["map-channel-marker online"]);

    wrapper.unmount();
  });

  it("地图覆盖物的样式必须走 :deep(),否则 scoped 一条都不生效", async () => {
    // 元素由 JS 动态 createElement、再交给 MapLibre 插进 .map-container 内部,
    // 拿不到 scoped 的 data-v-* 属性 ⇒ 裸选择器**一条都不生效**,
    // 气泡/标记点直接退回浏览器默认 button:白底黑字的方块(历史上地图上那个 "1")。
    const source = await readPageSource();
    for (const cls of ["map-cluster-marker", "map-channel-marker", "map-channel-pip"]) {
      expect(source, `${cls} 的样式必须写成 :deep(.${cls})`).toContain(`:deep(.${cls})`);
      expect(source, `${cls} 不能再出现裸选择器`).not.toMatch(new RegExp(`^\\s*\\.${cls}\\s*[,{]`, "m"));
    }
  });
});

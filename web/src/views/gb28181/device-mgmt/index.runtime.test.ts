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

vi.mock("vue-router", () => ({
  useRoute: () => ({ query: {}, params: {}, path: "/gb28181/devices" }),
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
        SubscriptionDialog: true,
        DirectoryPanel: true,
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
        "a-button": passthroughStub,
        "a-select": passthroughStub,
        "a-option": passthroughStub,
        "a-doption": passthroughStub,
        "a-pagination": passthroughStub,
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
    user.account.permissions = ["gb28181:device:view"];
    // happy-dom 元素无布局尺寸:容器永远 0 宽会让 ensureMap 无限 rAF 重试
    Object.defineProperty(HTMLElement.prototype, "clientWidth", { get: () => 800, configurable: true });
    Object.defineProperty(HTMLElement.prototype, "clientHeight", { get: () => 600, configurable: true });
    // 登记处跨用例累积,必须清空
    mapInstances.length = 0;
    markerInstances.length = 0;
    api.listDevices.mockReset().mockResolvedValue(okList());
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

  it("没有历史偏好时默认选中卡片视图", async () => {
    const wrapper = mountPage();
    await flushPromises();

    const cardButton = wrapper.get('[aria-label="卡片视图"]');
    expect(cardButton.attributes("aria-pressed")).toBe("true");
    expect(wrapper.find(".card-view").exists()).toBe(true);
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

  it("#5 统计轮询只拉当前资产类型(设备视图只查设备)", async () => {
    const wrapper = mountPage();
    await flushPromises();

    expect(api.listDevices).toHaveBeenCalledTimes(3); // 主列表 1 + 统计 online/offline 2
    expect(api.listChannels).not.toHaveBeenCalled(); // 统计不再为通道类型发起全量计数
    expect(wrapper.text()).toContain("设备");
    wrapper.unmount();
  });

  it("#6 单个统计请求失败不拖累另一个", async () => {
    // 按 status 参数区分,不依赖调用顺序(主列表无 status)
    api.listDevices.mockImplementation(async (params: { status?: string }) => {
      if (params?.status === "online") throw new Error("online count down");
      if (params?.status === "offline") return okList(7);
      return okList();
    });
    api.listChannels.mockResolvedValue(okList());

    const wrapper = mountPage();
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

  it("shows a 10-second countdown and refreshes the current view automatically", async () => {
    const wrapper = mountPage();
    await flushPromises();

    const refresh = wrapper.get("[data-testid='refresh-control']");
    expect(refresh.text()).toContain("10s");
    const initialDeviceCalls = api.listDevices.mock.calls.length;

    await vi.advanceTimersByTimeAsync(1000);
    expect(refresh.text()).toContain("9s");

    await vi.advanceTimersByTimeAsync(9000);
    await flushPromises();
    expect(api.listDevices.mock.calls.length).toBeGreaterThan(initialDeviceCalls);
    expect(refresh.text()).toContain("10s");
    wrapper.unmount();
  });

  it("切换资产类型立即刷新当前类型统计", async () => {
    const wrapper = mountPage();
    await flushPromises();
    api.listChannels.mockClear();

    const channelBtn = wrapper.findAll("button").find(btn => btn.text() === "通道");
    expect(channelBtn).toBeTruthy();
    await channelBtn!.trigger("click");
    await flushPromises();

    // 主列表 1 次 + 统计 online/offline 2 次
    expect(api.listChannels).toHaveBeenCalledTimes(3);
    wrapper.unmount();
  });
  it("opens each card action directly and preserves upgrade interlock after closing", async () => {
    user.account.permissions = ["*:*:*"];
    window.localStorage.setItem("uvp.gb28181.device-mgmt.view-mode", "card");
    const device = {
      id: 31,
      deviceId: "34020000001320000001",
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
    const menu = wrapper.findComponent({ name: "DeviceMaintenanceMenu" });
    expect(menu.exists()).toBe(true);
    menu.vm.$emit("upgrade");
    await flushPromises();
    const upgrade = wrapper.findComponent({ name: "DeviceFirmwareUpgradeDrawer" });
    expect(upgrade.props("visible")).toBe(true);
    const operation = { operationId: "upgrade-31", deviceId: 31, status: "accepted", firmware: "v2" };
    upgrade.vm.$emit("operationUpdated", operation);
    upgrade.vm.$emit("update:visible", false);
    api.listFirmwareUpgrades.mockResolvedValue(okList(1, [operation]));
    await flushPromises();
    expect(wrapper.text()).toContain("升级处理中");
    menu.vm.$emit("reboot");
    await flushPromises();
    const reboot = wrapper.findComponent({ name: "DeviceRebootDialog" });
    expect(reboot.props("visible")).toBe(true);
    expect(reboot.props("blockedReason")).toContain("升级");
    expect(upgrade.props("visible")).toBe(false);
    reboot.vm.$emit("viewRecords", "reboot-31");
    await flushPromises();
    const records = wrapper.findComponent({ name: "DeviceMaintenanceRecordsDrawer" });
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

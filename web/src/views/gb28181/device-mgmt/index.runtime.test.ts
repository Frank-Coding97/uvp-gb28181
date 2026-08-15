import { flushPromises, mount } from "@vue/test-utils";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import DeviceMgmt from "./index.vue";

// cross-review round-2 修复回归测试:
// #3 地图兜底定时器销毁清理 / #5 统计只拉当前资产类型 / #6 统计单失败隔离

const storage = new Map<string, string>();
Object.defineProperty(window, "localStorage", {
    configurable: true,
    value: {
        get length() { return storage.size; },
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
    refreshDeviceCatalog: vi.fn(),
    updateCloudRecording: vi.fn(),
    updateChannelStreamTransport: vi.fn(),
    updateChannel: vi.fn(),
    updateDevice: vi.fn(),
    stopPlay: vi.fn()
}));

vi.mock("./api", () => api);

vi.mock("maplibre-gl", () => {
    class FakeMap {
        onceCbs: Record<string, (() => void) | undefined> = {};
        onCbs: Record<string, (() => void) | undefined> = {};
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
            return 12;
        }
        resize() {}
    }
    return {
        default: { Map: FakeMap, NavigationControl: class {}, AttributionControl: class {} },
        LngLatBounds: class {
            extend() {}
        },
        Marker: class {
            setLngLat() {
                return this;
            }
            addTo() {
                return this;
            }
        }
    };
});

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
    useUserStoreHook: () => ({ account: { permissions: [] } })
}));

vi.mock("@arco-design/web-vue", () => ({
    Message: { success: vi.fn(), error: vi.fn(), warning: vi.fn(), info: vi.fn() },
    Modal: { confirm: vi.fn() }
}));

function okList(total = 0, list: unknown[] = []) {
    return { code: 0, message: "", data: { total, list } };
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
                PlayConsoleLinked: true,
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
                "a-dropdown": passthroughStub,
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
        // happy-dom 元素无布局尺寸:容器永远 0 宽会让 ensureMap 无限 rAF 重试
        Object.defineProperty(HTMLElement.prototype, "clientWidth", { get: () => 800, configurable: true });
        Object.defineProperty(HTMLElement.prototype, "clientHeight", { get: () => 600, configurable: true });
        api.listDevices.mockReset().mockResolvedValue(okList());
        api.listChannels.mockReset().mockResolvedValue(okList());
        api.listMapMarkers.mockReset().mockResolvedValue(okList());
        api.listMapClusters.mockReset().mockResolvedValue({ code: 0, message: "", data: { clusters: [] } });
        api.listDeviceStatusEvents.mockReset().mockResolvedValue(okList());
        api.listDeviceSubscriptions.mockReset().mockResolvedValue(okList());
    });

    afterEach(() => {
        window.localStorage.clear();
        vi.useRealTimers();
        vi.unstubAllGlobals();
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
});

import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { RecordQueryOptions } from "../../gb28181/device-mgmt/api";

const state = vi.hoisted(() => ({
  permissions: ["*:*:*"] as string[],
  route: { params: {} as Record<string, string>, query: {} as Record<string, string>, path: "/recording-center/device" },
  router: { push: vi.fn(), replace: vi.fn() }
}));

const api = vi.hoisted(() => ({ getRecordQueryOptions: vi.fn() }));

// ⛔ 只能部分 mock：`@/utils/http` 在模块顶层会 import `@/router` 并调用 createRouter，
//    整体替换 vue-router 会让那几个导出消失，页面还没挂载就先炸在 import 阶段。
vi.mock("vue-router", async importOriginal => {
  const actual = await importOriginal<typeof import("vue-router")>();
  return {
    ...actual,
    useRoute: () => state.route,
    useRouter: () => state.router
  };
});
vi.mock("../../gb28181/device-mgmt/api", () => api);
vi.mock("@/hooks/usePermission", () => ({
  hasPermission: (permission: string) => state.permissions.includes("*:*:*") || state.permissions.includes(permission)
}));
vi.mock("../../gb28181/useDeviceStatusDict", () => ({
  useDeviceStatusLabel: () => (value: unknown) => (value ? "在线" : "离线")
}));
vi.mock("@arco-design/web-vue", () => ({
  Message: { error: vi.fn(), info: vi.fn(), success: vi.fn(), warning: vi.fn() }
}));
vi.mock("@/api/recordCache", () => ({ createRecordCacheTask: vi.fn() }));

import DeviceRecordPage from "./index.vue";

function stubChildren() {
  return {
    ChannelSourceTree: { template: "<div data-testid='channel-tree-stub' />" },
    PlayWindow: { template: "<div data-test='play-window-stub' />" },
    RecordCacheProgressDialog: { template: "<div />" }
  };
}

function mountPage() {
  return mount(DeviceRecordPage, { global: { stubs: stubChildren() } });
}

function queryOptions(overrides: Partial<RecordQueryOptions> = {}): RecordQueryOptions {
  return {
    device: { id: 1, code: "34020000001110000001", name: "东门设备", online: true },
    channel: { id: 11, code: "34020000001320000001", name: "东门出入口" },
    timezone: "Asia/Shanghai",
    serverNow: "2026-11-20T16:00:00",
    maxRangeHours: 24,
    timeoutSeconds: 15,
    supportedTypes: ["all", "manual", "alarm"],
    ...overrides
  };
}

describe("设备录像页", () => {
  beforeEach(() => {
    state.permissions = ["*:*:*"];
    state.route.params = {};
    state.route.query = {};
    state.router.push.mockReset();
    state.router.replace.mockReset();
    api.getRecordQueryOptions.mockReset();
  });

  it("没有查询权限时整页提示，连通道树都不渲染", () => {
    state.permissions = [];
    const wrapper = mountPage();

    expect(wrapper.find("[data-testid=device-record-permission-denied]").exists()).toBe(true);
    expect(wrapper.find("[data-testid=channel-tree-stub]").exists()).toBe(false);
  });

  it("还没选通道时，播放器提示先从左边选一个通道", () => {
    const wrapper = mountPage();

    expect(wrapper.find("[data-testid=channel-tree-stub]").exists()).toBe(true);
    expect(wrapper.get("[data-testid=playback-empty]").text()).toContain("从左侧选择一个通道");
    expect(api.getRecordQueryOptions).not.toHaveBeenCalled();
  });

  it("深链带通道号时自动加载配置，并按「今天」截断查询范围", async () => {
    state.route.params = { channelId: "11" };
    api.getRecordQueryOptions.mockResolvedValue({
      code: 0,
      data: {
        ...queryOptions(),
        serverNow: "2026-11-20T16:00:00",
        supportedTypes: ["all"]
      }
    });
    const wrapper = mountPage();
    await flushPromises();

    expect(api.getRecordQueryOptions).toHaveBeenCalledWith(11);
    expect(wrapper.get("[data-testid=device-record-date]").text()).toBe("2026-11-20");
  });

  it("刷新页面时从 ?channel= 恢复选中的通道（菜单入口不带路径参数）", async () => {
    state.route.query = { channel: "11", date: "2026-11-20" };
    api.getRecordQueryOptions.mockResolvedValue({ code: 0, data: queryOptions() });
    mountPage();
    await flushPromises();

    expect(api.getRecordQueryOptions).toHaveBeenCalledWith(11);
  });

  it("设备离线时给出「去云端录像看看」的出口", async () => {
    state.route.params = { channelId: "11" };
    api.getRecordQueryOptions.mockResolvedValue({
      code: 0,
      data: queryOptions({ device: { id: 1, code: "34020000001110000001", name: "东门设备", online: false } })
    });
    const wrapper = mountPage();
    await flushPromises();

    expect(wrapper.get("[data-testid=playback-empty]").text()).toContain("设备离线");
    expect(wrapper.get("[data-testid=playback-empty]").text()).toContain("去云端录像看看");
  });

  it("配置加载失败时给出可重试的错误条", async () => {
    state.route.params = { channelId: "11" };
    api.getRecordQueryOptions.mockRejectedValue({ response: { status: 403 } });
    const wrapper = mountPage();
    await flushPromises();

    expect(wrapper.text()).toContain("没有权限执行该操作");
    expect(wrapper.text()).toContain("重试");
  });
});

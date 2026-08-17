import { flushPromises, mount } from "@vue/test-utils";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { OnlineUserSession } from "@/api/online-user";
import OnlineUserPage from "./index.vue";

const api = vi.hoisted(() => ({
  getOnlineUsersAPI: vi.fn(),
  getDivisionAPI: vi.fn()
}));

vi.mock("@/api/online-user", async importOriginal => ({
  ...await importOriginal<typeof import("@/api/online-user")>(),
  getOnlineUsersAPI: api.getOnlineUsersAPI
}));
vi.mock("@/api/department", () => ({ getDivisionAPI: api.getDivisionAPI }));
vi.mock("@/store/modules/user", () => ({
  useUserStoreHook: () => ({ account: { permissions: ["system:online-user:force-logout"] } })
}));

const session = (sid: string, username = "admin"): OnlineUserSession => ({
  sid,
  userId: 1,
  username,
  nickName: "管理员",
  departmentId: 2,
  departmentName: "平台运维部",
  clientIp: "10.0.0.8",
  loginLocation: "内网",
  browser: "Chrome 140",
  os: "macOS",
  loginAt: "2026-08-17T10:00:00+08:00",
  lastActiveAt: "2026-08-17T10:10:00+08:00",
  sessionExpiresAt: "2026-08-17T12:00:00+08:00",
  status: "active"
});

function response(list = [session("current")], currentSid = "current") {
  return Promise.resolve({ code: 0, message: "", data: { list, total: list.length, currentSid } });
}

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>(done => { resolve = done; });
  return { promise, resolve };
}

const mountedWrappers: Array<{ unmount: () => void }> = [];

function mountPage() {
  const wrapper = mount(OnlineUserPage, {
    global: {
      stubs: {
        "s-layout-search": { template: "<section><slot name='fields'/><slot name='actions'/><slot name='extra'/></section>" },
        "a-input": { template: "<input />" },
        "a-tree-select": { template: "<select />" },
        "a-select": { template: "<select><slot /></select>" },
        "a-option": { template: "<option><slot /></option>" },
        "a-button": { template: "<button><slot name='icon'/><slot /></button>" },
        "a-tooltip": { template: "<span><slot /></span>" },
        "a-table": { name: "ATable", props: ["data", "loading", "pagination"], template: "<div data-testid='online-table'><slot name='columns'/><slot name='empty'/></div>" },
        "a-table-column": { template: "<div />" },
        "a-empty": { props: ["description"], template: "<span>{{ description }}</span>" },
        OnlineUserAction: { template: "<span />" },
        Search: true,
        RotateCcw: true,
        RefreshCw: true
      }
    }
  });
  mountedWrappers.push(wrapper);
  return wrapper;
}

describe("online user page", () => {
  beforeEach(() => {
    api.getOnlineUsersAPI.mockReset().mockImplementation(() => response());
    api.getDivisionAPI.mockReset().mockResolvedValue({ code: 0, data: { list: [] } });
    Object.defineProperty(document, "visibilityState", { configurable: true, value: "visible" });
  });

  afterEach(() => {
    mountedWrappers.splice(0).forEach(wrapper => wrapper.unmount());
    vi.useRealTimers();
  });

  it("loads initially and keeps filters stable across search, reset and pagination", async () => {
    const wrapper = mountPage();
    await flushPromises();
    expect(api.getOnlineUsersAPI).toHaveBeenLastCalledWith({ pageNum: 1, pageSize: 20 });

    const vm = wrapper.vm as any;
    vm.form.username = "alice";
    vm.form.departmentId = 7;
    vm.form.clientIp = "10.0.0.9";
    vm.form.status = "idle";
    await vm.search();
    expect(api.getOnlineUsersAPI).toHaveBeenLastCalledWith({
      pageNum: 1, pageSize: 20, username: "alice", departmentId: 7, clientIp: "10.0.0.9", status: "idle"
    });

    await vm.handlePageChange(3);
    expect(api.getOnlineUsersAPI).toHaveBeenLastCalledWith(expect.objectContaining({ pageNum: 3, pageSize: 20, username: "alice" }));
    await vm.handlePageSizeChange(50);
    expect(api.getOnlineUsersAPI).toHaveBeenLastCalledWith(expect.objectContaining({ pageNum: 1, pageSize: 50, username: "alice" }));

    await vm.reset();
    expect(api.getOnlineUsersAPI).toHaveBeenLastCalledWith({ pageNum: 1, pageSize: 50 });
  });

  it("keeps the newest response when an older request finishes last", async () => {
    const older = deferred<any>();
    api.getOnlineUsersAPI.mockReset().mockReturnValueOnce(older.promise).mockImplementationOnce(() => response([session("new", "newest")]));
    const wrapper = mountPage();
    await (wrapper.vm as any).search();
    await flushPromises();
    expect((wrapper.vm as any).sessions[0].username).toBe("newest");

    older.resolve({ code: 0, data: { list: [session("old", "stale")], total: 1, currentSid: "old" } });
    await flushPromises();
    expect((wrapper.vm as any).sessions[0].username).toBe("newest");
  });

  it("refreshes every 30 seconds only while the page is visible", async () => {
    vi.useFakeTimers();
    const wrapper = mountPage();
    await flushPromises();
    expect(api.getOnlineUsersAPI).toHaveBeenCalledTimes(1);

    await vi.advanceTimersByTimeAsync(30_000);
    expect(api.getOnlineUsersAPI).toHaveBeenCalledTimes(2);
    Object.defineProperty(document, "visibilityState", { configurable: true, value: "hidden" });
    document.dispatchEvent(new Event("visibilitychange"));
    await vi.advanceTimersByTimeAsync(60_000);
    expect(api.getOnlineUsersAPI).toHaveBeenCalledTimes(2);

    Object.defineProperty(document, "visibilityState", { configurable: true, value: "visible" });
    document.dispatchEvent(new Event("visibilitychange"));
    await flushPromises();
    expect(api.getOnlineUsersAPI).toHaveBeenCalledTimes(3);
    wrapper.unmount();
  });

  it("renders a retryable alert when loading fails", async () => {
    api.getOnlineUsersAPI.mockRejectedValueOnce(new Error("会话库暂不可用"));
    const wrapper = mountPage();
    await flushPromises();
    expect(wrapper.get('[role="alert"]').text()).toContain("会话库暂不可用");
    expect(wrapper.text()).toContain("重试");
  });
});

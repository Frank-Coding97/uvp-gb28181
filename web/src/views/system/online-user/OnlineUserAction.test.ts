import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { OnlineUserSession } from "@/api/online-user";
import OnlineUserAction from "./OnlineUserAction.vue";

const mocks = vi.hoisted(() => ({
  forceLogout: vi.fn(),
  confirm: vi.fn(),
  success: vi.fn(),
  error: vi.fn()
}));

vi.mock("@/api/online-user", async importOriginal => ({
  ...await importOriginal<typeof import("@/api/online-user")>(),
  forceLogoutOnlineSessionAPI: mocks.forceLogout
}));
vi.mock("@arco-design/web-vue", () => ({
  Modal: { confirm: mocks.confirm },
  Message: { success: mocks.success, error: mocks.error }
}));

const record: OnlineUserSession = {
  sid: "target-session",
  userId: 2,
  username: "alice",
  nickName: "Alice",
  departmentId: 1,
  departmentName: "运维部",
  clientIp: "10.0.0.2",
  loginLocation: "内网",
  browser: "Chrome",
  os: "Windows",
  loginAt: "2026-08-17T10:00:00+08:00",
  lastActiveAt: "2026-08-17T10:10:00+08:00",
  sessionExpiresAt: "2026-08-17T12:00:00+08:00",
  status: "active"
};

function mountAction(props: Record<string, unknown> = {}) {
  return mount(OnlineUserAction, {
    props: { session: record, currentSid: "current-session", canForce: true, ...props },
    global: {
      stubs: {
        "a-button": { props: ["disabled", "loading"], template: "<button :disabled='disabled' :data-loading='loading'><slot name='icon'/><slot /></button>" },
        "a-tooltip": { props: ["content"], template: "<span :title='content'><slot /></span>" },
        LogOut: true
      }
    }
  });
}

describe("OnlineUserAction", () => {
  beforeEach(() => {
    mocks.forceLogout.mockReset();
    mocks.confirm.mockReset();
    mocks.success.mockReset();
    mocks.error.mockReset();
  });

  it("hides without permission and disables the current session", () => {
    expect(mountAction({ canForce: false }).find("button").exists()).toBe(false);
    const current = mountAction({ currentSid: record.sid });
    expect(current.get("button").attributes()).toHaveProperty("disabled");
    expect(current.get("span").attributes("title")).toContain("当前会话");
  });

  it("does not call the API until the confirmation accepts", async () => {
    const wrapper = mountAction();
    await wrapper.get("button").trigger("click");
    expect(mocks.confirm).toHaveBeenCalledOnce();
    expect(mocks.forceLogout).not.toHaveBeenCalled();
  });

  it("shows row loading and removes the session after a successful force logout", async () => {
    let resolve!: (value: unknown) => void;
    mocks.forceLogout.mockReturnValue(new Promise(done => { resolve = done; }));
    const wrapper = mountAction();
    await wrapper.get("button").trigger("click");
    const onOk = mocks.confirm.mock.calls[0][0].onOk;
    const pending = onOk();
    await wrapper.vm.$nextTick();
    expect(wrapper.get("button").attributes("data-loading")).toBe("true");

    resolve({ code: 0, message: "", data: { offline: true } });
    await pending;
    await flushPromises();
    expect(mocks.forceLogout).toHaveBeenCalledWith(record.sid);
    expect(wrapper.emitted("success")?.[0]).toEqual([record.sid]);
    expect(mocks.success).toHaveBeenCalledWith("强制下线成功");
  });

  it("keeps the row and exposes an alert when force logout fails", async () => {
    mocks.forceLogout.mockRejectedValue(new Error("会话库暂不可用"));
    const wrapper = mountAction();
    await wrapper.get("button").trigger("click");
    await expect(mocks.confirm.mock.calls[0][0].onOk()).rejects.toThrow("会话库暂不可用");
    await flushPromises();
    expect(wrapper.emitted("success")).toBeUndefined();
    expect(wrapper.get('[role="alert"]').text()).toContain("会话库暂不可用");
    expect(mocks.error).toHaveBeenCalledWith("会话库暂不可用");
  });
});

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const axiosRequest = vi.hoisted(() => vi.fn());
const messageError = vi.hoisted(() => vi.fn());
const interceptorHandlers = vi.hoisted(() => ({
  responseRejected: undefined as undefined | ((error: any) => Promise<never>)
}));
const logout = vi.hoisted(() => vi.fn());
const push = vi.hoisted(() => vi.fn());

vi.mock("axios", () => ({
  default: {
    create: vi.fn(() => ({
      interceptors: {
        request: { use: vi.fn() },
        response: {
          use: vi.fn((_fulfilled: unknown, rejected: (error: any) => Promise<never>) => {
            interceptorHandlers.responseRejected = rejected;
          })
        }
      },
      request: axiosRequest
    })),
    isCancel: vi.fn(() => false)
  }
}));
vi.mock("@arco-design/web-vue", () => ({ Message: { error: messageError } }));
vi.mock("@/globals", () => ({ throttle: (callback: unknown) => callback }));
vi.mock("@/utils/app", () => ({ throttledModalConfirm: vi.fn() }));
vi.mock("@/utils/auth", () => ({
  getAccessToken: vi.fn(),
  getRefreshToken: vi.fn(),
  formatToken: vi.fn()
}));
vi.mock("@/store/modules/user", () => ({ useUserStoreHook: () => ({ logOut: logout }) }));
vi.mock("@/router", () => ({ default: { push, currentRoute: { value: { fullPath: "/online" } } } }));

import { http } from "./index";

describe("HTTP error messages", () => {
  beforeEach(() => {
    axiosRequest.mockReset();
    messageError.mockReset();
    logout.mockReset();
    push.mockReset();
    vi.spyOn(console, "error").mockImplementation(() => undefined);
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("shows the server error by default", async () => {
    axiosRequest.mockRejectedValueOnce({ response: { data: { message: "请求失败" } } });

    await expect(http.request("get", "/failure")).rejects.toBeTruthy();

    expect(messageError).toHaveBeenCalledWith("请求失败");
  });

  it("suppresses the global message for an explicitly silent request", async () => {
    axiosRequest.mockRejectedValueOnce({ response: { data: { message: "后台加载失败" } } });

    await expect(
      http.request("get", "/background-failure", undefined, { showErrorMessage: false })
    ).rejects.toBeTruthy();

    expect(messageError).not.toHaveBeenCalled();
  });

  it("clears authentication and redirects when a heartbeat receives 401", async () => {
    expect(interceptorHandlers.responseRejected).toBeTypeOf("function");

    await expect(interceptorHandlers.responseRejected!({
      response: { status: 401 },
      config: { url: "/api/users/session/heartbeat" }
    })).rejects.toBeTruthy();

    expect(logout).toHaveBeenCalledTimes(1);
    expect(push).toHaveBeenCalledWith({ path: "/login", query: { redirect: "/online" } });
  });
});

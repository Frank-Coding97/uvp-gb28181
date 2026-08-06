import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const axiosRequest = vi.hoisted(() => vi.fn());
const messageError = vi.hoisted(() => vi.fn());

vi.mock("axios", () => ({
  default: {
    create: vi.fn(() => ({
      interceptors: {
        request: { use: vi.fn() },
        response: { use: vi.fn() }
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
vi.mock("@/store/modules/user", () => ({ useUserStoreHook: vi.fn() }));
vi.mock("@/router", () => ({ default: { push: vi.fn(), currentRoute: { value: { fullPath: "/" } } } }));

import { http } from "./index";

describe("HTTP error messages", () => {
  beforeEach(() => {
    axiosRequest.mockReset();
    messageError.mockReset();
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
});

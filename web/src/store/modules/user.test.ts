import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";

const authMocks = vi.hoisted(() => ({
  removeAccessToken: vi.fn(),
  removeRefreshToken: vi.fn(),
  removeLocalStorage: vi.fn()
}));
const heartbeatMocks = vi.hoisted(() => ({ start: vi.fn(), stop: vi.fn() }));
const apiMocks = vi.hoisted(() => ({
  getLogin: vi.fn(),
  refreshTokenApi: vi.fn(),
  getProfileAPI: vi.fn()
}));

vi.mock("@/utils/auth", () => ({
  UserInfoKey: "user-info",
  removeAccessToken: authMocks.removeAccessToken,
  removeRefreshToken: authMocks.removeRefreshToken,
  setAccessToken: vi.fn(),
  setRefreshToken: vi.fn()
}));
vi.mock("@/utils/app", () => ({
  getLocalStorage: vi.fn(() => undefined),
  setLocalStorage: vi.fn(),
  removeLocalStorage: authMocks.removeLocalStorage,
  handleUrl: vi.fn((value: string) => value)
}));
vi.mock("@/api/user", () => ({
  getLogin: apiMocks.getLogin,
  refreshTokenApi: apiMocks.refreshTokenApi,
  getProfileAPI: apiMocks.getProfileAPI
}));
vi.mock("@/services/session-heartbeat", () => ({
  startSessionHeartbeat: heartbeatMocks.start,
  stopSessionHeartbeat: heartbeatMocks.stop
}));

import { registerUserLogoutCleanup, runUserLogoutCleanup, useUserStore } from "./user";

describe("user logout cleanup", () => {
  beforeEach(() => {
    setActivePinia(createPinia());
    registerUserLogoutCleanup(() => undefined);
    heartbeatMocks.start.mockReset();
    heartbeatMocks.stop.mockReset();
    apiMocks.getLogin.mockReset();
  });

  it("starts heartbeats when login establishes authentication", async () => {
    apiMocks.getLogin.mockResolvedValue({
      code: 0,
      message: "",
      data: { accessToken: "access", accessTokenExpires: 2, refreshToken: "refresh", refreshTokenExpires: 3 }
    });
    const store = useUserStore();

    await expect(store.loginByUsername({ username: "admin" })).resolves.toMatchObject({ code: 0 });

    expect(heartbeatMocks.start).toHaveBeenCalledTimes(1);
  });

  it("runs registered download cleanup before clearing local authentication", async () => {
    const cleanup = vi.fn().mockResolvedValue(undefined);
    registerUserLogoutCleanup(cleanup);
    const store = useUserStore();

    await expect(store.logOut()).resolves.toBeUndefined();

    expect(cleanup).toHaveBeenCalledTimes(1);
    expect(authMocks.removeAccessToken).toHaveBeenCalledTimes(1);
    expect(authMocks.removeRefreshToken).toHaveBeenCalledTimes(1);
    expect(authMocks.removeLocalStorage).toHaveBeenCalledWith("user-info");
    expect(heartbeatMocks.stop).toHaveBeenCalledTimes(1);
  });

  it("exposes logout cleanup for authenticated server logout flows", async () => {
    const cleanup = vi.fn().mockResolvedValue(undefined);
    registerUserLogoutCleanup(cleanup);

    await runUserLogoutCleanup();

    expect(cleanup).toHaveBeenCalledTimes(1);
    expect(authMocks.removeAccessToken).not.toHaveBeenCalled();
  });
});

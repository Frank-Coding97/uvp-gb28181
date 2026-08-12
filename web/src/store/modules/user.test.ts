import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";

const authMocks = vi.hoisted(() => ({
  removeAccessToken: vi.fn(),
  removeRefreshToken: vi.fn(),
  removeLocalStorage: vi.fn()
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
  getLogin: vi.fn(),
  refreshTokenApi: vi.fn(),
  getProfileAPI: vi.fn()
}));

import { registerUserLogoutCleanup, runUserLogoutCleanup, useUserStore } from "./user";

describe("user logout cleanup", () => {
  beforeEach(() => {
    setActivePinia(createPinia());
    registerUserLogoutCleanup(() => undefined);
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
  });

  it("exposes logout cleanup for authenticated server logout flows", async () => {
    const cleanup = vi.fn().mockResolvedValue(undefined);
    registerUserLogoutCleanup(cleanup);

    await runUserLogoutCleanup();

    expect(cleanup).toHaveBeenCalledTimes(1);
    expect(authMocks.removeAccessToken).not.toHaveBeenCalled();
  });
});

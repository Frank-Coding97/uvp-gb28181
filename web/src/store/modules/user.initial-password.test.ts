import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";

const authMocks = vi.hoisted(() => ({
  removeAccessToken: vi.fn(),
  removeRefreshToken: vi.fn(),
  removeLocalStorage: vi.fn(),
  setAccessToken: vi.fn(),
  setRefreshToken: vi.fn()
}));
const apiMocks = vi.hoisted(() => ({
  getLogin: vi.fn(),
  refreshTokenApi: vi.fn(),
  getProfileAPI: vi.fn()
}));

vi.mock("@/utils/auth", () => ({
  UserInfoKey: "user-info",
  removeAccessToken: authMocks.removeAccessToken,
  removeRefreshToken: authMocks.removeRefreshToken,
  setAccessToken: authMocks.setAccessToken,
  setRefreshToken: authMocks.setRefreshToken
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
  startSessionHeartbeat: vi.fn(),
  stopSessionHeartbeat: vi.fn()
}));
vi.mock("@/assets/img/admin-avatar.png", () => ({ default: "admin-avatar.png" }));

import { useUserStore } from "./user";

describe("initial password authentication state", () => {
  beforeEach(() => {
    setActivePinia(createPinia());
    apiMocks.getLogin.mockReset();
    apiMocks.getProfileAPI.mockReset();
    authMocks.removeAccessToken.mockReset();
    authMocks.removeRefreshToken.mockReset();
    authMocks.removeLocalStorage.mockReset();
  });

  it("takes the pending initial-password state from login", async () => {
    apiMocks.getLogin.mockResolvedValue({
      code: 0,
      message: "",
      data: {
        accessToken: "access",
        accessTokenExpires: 2,
        refreshToken: "refresh",
        refreshTokenExpires: 3,
        mustChangePassword: true
      }
    });

    const store = useUserStore();
    await store.loginByUsername({ username: "admin", password: "123456" });

    expect(store.mustChangePassword).toBe(true);
  });

  it("refreshes the pending state from profile instead of local storage", async () => {
    apiMocks.getProfileAPI.mockResolvedValue({
      code: 0,
      message: "",
      data: {
        id: 1,
        userName: "admin",
        nickName: "管理员",
        avatar: "",
        roleIDs: [],
        permissions: [],
        mustChangePassword: true
      }
    });

    const store = useUserStore();
    await store.getUserInfo();

    expect(store.mustChangePassword).toBe(true);
  });

  it("clears the pending state together with local authentication", async () => {
    const store = useUserStore();
    store.mustChangePassword = true;

    await store.logOut(false);

    expect(store.mustChangePassword).toBe(false);
    expect(authMocks.removeAccessToken).toHaveBeenCalledTimes(1);
    expect(authMocks.removeRefreshToken).toHaveBeenCalledTimes(1);
  });

  it("uses the default avatar when the profile has no avatar", async () => {
    apiMocks.getProfileAPI.mockResolvedValue({
      code: 0,
      message: "",
      data: {
        id: 1,
        userName: "admin",
        nickName: "管理员",
        avatar: "",
        roleIDs: [],
        permissions: [],
        mustChangePassword: false
      }
    });

    const store = useUserStore();
    await store.getUserInfo();

    expect(store.account.avatar).toBe("admin-avatar.png");
  });
});

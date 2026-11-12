import { defineStore } from "pinia";
import { ref } from "vue";
import pinia from "@/store";
import { setAccessToken, setRefreshToken, removeAccessToken, removeRefreshToken, UserInfoKey } from "@/utils/auth";
//import { type userType } from "@/store/types";
import { getLocalStorage, setLocalStorage, removeLocalStorage } from "@/utils/app";
import { type UserResult, type RefreshTokenResult, getLogin, refreshTokenApi, getProfileAPI } from "@/api/user";
import { userType } from "@/store/types";
import { handleUrl } from "@/utils/app";
import { startSessionHeartbeat, stopSessionHeartbeat } from "@/services/session-heartbeat";
import defaultAvatar from "@/assets/img/admin-avatar.png";

const logoutCleanups = new Set<() => Promise<void> | void>();

export function registerUserLogoutCleanup(cleanup: () => Promise<void> | void) {
  logoutCleanups.add(cleanup);
  return () => logoutCleanups.delete(cleanup);
}

export async function runUserLogoutCleanup() {
  await Promise.all(
    Array.from(logoutCleanups, cleanup =>
      Promise.resolve()
        .then(cleanup)
        .catch(() => undefined)
    )
  );
}

export const useUserStore = defineStore("user", () => {
  const userInfo = getLocalStorage<userType>(UserInfoKey);
  // State
  const account = ref<userType>({
    id: userInfo?.id ?? 0,
    avatar: userInfo?.avatar || defaultAvatar,
    username: userInfo?.username ?? "",
    nickname: userInfo?.nickname ?? "",
    roles: userInfo?.roles ?? [],
    permissions: userInfo?.permissions ?? []
  });
  // 该状态只来自当前会话接口，不从本地缓存恢复，避免绕过服务端初始密码状态。
  const mustChangePassword = ref(false);
  const initialPasswordStatusLoaded = ref(false);

  // action
  /** 登入 */
  const loginByUsername = async (data: any) => {
    return new Promise<UserResult>((resolve, reject) => {
      getLogin(data)
        .then(res => {
          if (res?.code === 0) {
            // 清除 accessToken 、 refreshToken 及登录用户信息
            removeAccessToken();
            removeRefreshToken();
            removeLocalStorage(UserInfoKey);
            // 登录成功后，设置 accessToken 和 refreshToken
            setAccessToken(res?.data?.accessToken, res?.data?.accessTokenExpires);
            setRefreshToken(res?.data?.refreshToken, res?.data?.refreshTokenExpires);
            mustChangePassword.value = Boolean(res?.data?.mustChangePassword);
            initialPasswordStatusLoaded.value = true;
            startSessionHeartbeat();
            resolve(res);
          } else {
            reject(res?.message || "登录失败");
          }
        })
        .catch(error => {
          reject(error);
        });
    });
  };
  /** 前端登出（不调用接口） */
  const logOut = async (cleanup = true) => {
    stopSessionHeartbeat();
    if (cleanup) await runUserLogoutCleanup();
    account.value.id = 0;
    account.value.avatar = defaultAvatar;
    account.value.username = "";
    account.value.nickname = "";
    account.value.roles = [];
    account.value.permissions = [];
    mustChangePassword.value = false;
    initialPasswordStatusLoaded.value = false;
    // 清除 accessToken 、 refreshToken 及登录用户信息
    removeAccessToken();
    removeRefreshToken();
    removeLocalStorage(UserInfoKey);
  };
  /** 刷新`token` */
  const handRefreshToken = async (data: string) => {
    return new Promise<RefreshTokenResult>((resolve, reject) => {
      refreshTokenApi(data)
        .then(res => {
          //console.log("refreshTokenApi", res)
          if (res?.code === 0) {
            setAccessToken(res.data?.accessToken, res.data?.accessTokenExpires);
            setRefreshToken(res.data?.refreshToken, res.data?.refreshTokenExpires);
            resolve(res);
          } else {
            reject(res?.message || "刷新`refreshToken`失败");
          }
        })
        .catch(error => {
          reject(error);
        });
    });
  };
  /** 获取并设置当前登录用户信息 */
  const getUserInfo = async () => {
    const { data } = await getProfileAPI();
    mustChangePassword.value = Boolean(data?.mustChangePassword);
    initialPasswordStatusLoaded.value = true;
    if (data?.id) {
      account.value.id = data.id;
      account.value.username = data.userName;
      account.value.nickname = data.nickName;
      account.value.avatar = handleUrl(data.avatar) || defaultAvatar;
      account.value.roles = data.roleIDs;
      account.value.permissions = data.permissions;
      const { mustChangePassword: _mustChangePassword, ...cachedProfile } = data;
      void _mustChangePassword;
      setLocalStorage(UserInfoKey, cachedProfile);
    }
    return data;
  };
  return {
    // State
    account,
    mustChangePassword,
    initialPasswordStatusLoaded,
    // action
    loginByUsername,
    logOut,
    handRefreshToken,
    getUserInfo
  };
});

export function useUserStoreHook() {
  return useUserStore(pinia);
}

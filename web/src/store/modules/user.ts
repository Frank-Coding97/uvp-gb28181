import { defineStore } from "pinia";
import { ref } from "vue";
import pinia from "@/store";
import { setAccessToken, setRefreshToken, removeAccessToken, removeRefreshToken, UserInfoKey } from "@/utils/auth";
//import { type userType } from "@/store/types";
import { getLocalStorage, setLocalStorage, removeLocalStorage } from "@/utils/app";
import { type UserResult, type RefreshTokenResult, getLogin, refreshTokenApi, getProfileAPI } from "@/api/user";
import { userType } from "@/store/types";
import { handleUrl } from "@/utils/app";

let logoutCleanup: (() => Promise<void> | void) | undefined;

export function registerUserLogoutCleanup(cleanup: () => Promise<void> | void) {
    logoutCleanup = cleanup;
}

export async function runUserLogoutCleanup() {
    await Promise.resolve(logoutCleanup?.()).catch(() => undefined);
}

export const useUserStore = defineStore("user", () => {
    const userInfo = getLocalStorage<userType>(UserInfoKey);
    // State
    const account = ref<userType>({
        id: userInfo?.id ?? 0,
        avatar: userInfo?.avatar ?? "",
        username: userInfo?.username ?? "",
        nickname: userInfo?.nickname ?? "",
        roles: userInfo?.roles ?? [],
        permissions: userInfo?.permissions ?? [],
    });

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
        if (cleanup) await runUserLogoutCleanup();
        account.value.id = 0;
        account.value.avatar = "";
        account.value.username = "";
        account.value.nickname = "";
        account.value.roles = [];
        account.value.permissions = [];
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
        if (data?.id) {
            account.value.id = data.id;
            account.value.username = data.userName;
            account.value.nickname = data.nickName;
            account.value.avatar = handleUrl(data.avatar);
            account.value.roles = data.roleIDs;
            account.value.permissions = data.permissions;
            setLocalStorage(UserInfoKey, data);
        }
        return data;
    };
    return {
        // State
        account,
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

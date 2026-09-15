import { http } from "@/utils/http";
import { baseUrlApi } from "./utils";
import type { BaseResult } from "./types";

export type OnlineUserStatus = "active" | "idle";

export interface OnlineUserSession {
  sid: string;
  userId: number;
  username: string;
  nickName: string;
  departmentId: number;
  departmentName: string;
  clientIp: string;
  loginLocation: string;
  browser: string;
  os: string;
  loginAt: string;
  lastActiveAt: string;
  sessionExpiresAt: string;
  status: OnlineUserStatus;
}

export interface OnlineUserListParams {
  pageNum: number;
  pageSize: number;
  username?: string;
  departmentId?: number;
  clientIp?: string;
  status?: OnlineUserStatus | "";
}

export type OnlineUserListResult = BaseResult<{
  list: OnlineUserSession[];
  total: number;
  currentSid?: string;
}>;

export const getOnlineUsersAPI = (params: OnlineUserListParams) =>
  http.request<OnlineUserListResult>("get", baseUrlApi("sysOnlineUser/list"), { params });

export const forceLogoutOnlineSessionAPI = (sid: string) =>
  http.request<BaseResult<{ offline: boolean }>>("post", baseUrlApi("sysOnlineUser/forceLogout"), {
    data: { sid }
  });

export const sessionHeartbeatAPI = () =>
  http.request<BaseResult<{ online: boolean }>>(
    "post",
    baseUrlApi("users/session/heartbeat"),
    undefined,
    { showErrorMessage: false }
  );

import { http } from "@/utils/http";
import { baseUrlApi } from "./utils";
import type { BaseResult } from "./types";

export type LoginLogResult = "success" | "failure";

export type LoginLogFailureReason =
  | "captcha_invalid"
  | "user_not_found"
  | "user_disabled"
  | "account_locked"
  | "password_incorrect"
  | "session_create_failed"
  | "server_error";

export interface LoginLogItem {
  id: number;
  userId?: number;
  username: string;
  result: LoginLogResult;
  failureReason?: string;
  ip: string;
  location: string;
  browser: string;
  os: string;
  createdAt: string;
}

export interface LoginLogDetail extends LoginLogItem {
  userAgent: string;
}

export interface LoginLogListParams {
  pageNum: number;
  pageSize: number;
  username?: string;
  result?: LoginLogResult | "";
  failureReason?: LoginLogFailureReason | "";
  ip?: string;
  startTime?: string;
  endTime?: string;
}

export type LoginLogListResult = BaseResult<{ list: LoginLogItem[]; total: number }>;
export type LoginLogDetailResult = BaseResult<LoginLogDetail>;

export const getLoginLogsAPI = (params: LoginLogListParams) =>
  http.request<LoginLogListResult>("get", baseUrlApi("sysLoginLog/list"), { params });

export const getLoginLogDetailAPI = (id: number) =>
  http.request<LoginLogDetailResult>("get", baseUrlApi(`sysLoginLog/${id}`));

import { http } from "@/utils/http";
import { baseUrlApi } from "./utils";
import { BaseResult } from "./types";

// 服务器配置参数
export interface SystemConfig {
  systemLogo: string;
  systemIcon: string;
  systemName: string;
  systemCopyright: string;
  systemRecordNo: string;
  defaultusername: string;
  defaultpassword: string;
}

export interface SafeConfig {
  loginLockThreshold: number;
  loginLockExpire: number;
  loginLockDuration: number;
  minPasswordLength: number;
  requireSpecialChar: boolean;
}

// 验证码配置参数
export interface CaptchaConfig {
  open: boolean;
  length: number;
}

export interface LogCleanupConfig {
  sipRetentionDays: number;
  operationRetentionDays: number;
  loginRetentionDays: number;
  jobRetentionDays: number;
  playbackRetentionDays: number;
  schedulerRetentionDays: number;
  configured: boolean;
}

export type LogCleanupUpdateConfig = Omit<LogCleanupConfig, "configured">;
export type LogCleanupDraft = Omit<LogCleanupConfig, keyof LogCleanupUpdateConfig> &
  Record<keyof LogCleanupUpdateConfig, number | null>;

export const defaultLogCleanupConfig: LogCleanupConfig = {
  sipRetentionDays: 7,
  operationRetentionDays: 180,
  loginRetentionDays: 180,
  jobRetentionDays: 30,
  playbackRetentionDays: 7,
  schedulerRetentionDays: 7,
  configured: false
};

// 配置响应数据
export interface ConfigResponseData {
  system: SystemConfig;
  captcha: CaptchaConfig;
  safe: SafeConfig;
  logCleanup?: LogCleanupConfig;
}

export interface RegularConfigRequestData {
  system?: SystemConfig;
  safe?: SafeConfig;
  captcha?: CaptchaConfig;
}

// 普通页签沿用完整配置；日志清理页签可仅更新对应配置段。
export type ConfigRequestData = RegularConfigRequestData & { logCleanup?: LogCleanupUpdateConfig };

// 获取配置响应结果
export type GetConfigResult = BaseResult<ConfigResponseData>;
export type UpdateConfigResult = BaseResult<Partial<ConfigResponseData>>;

/** 获取系统配置 */
export const getConfigAPI = () => {
  return http.request<GetConfigResult>("get", baseUrlApi("config/get"));
};

/** 更新系统配置 */
export const updateConfigAPI = (data: ConfigRequestData) => {
  return http.request<UpdateConfigResult>("put", baseUrlApi("config/update"), { data });
};

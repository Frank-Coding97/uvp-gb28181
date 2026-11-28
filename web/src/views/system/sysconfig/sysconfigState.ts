import { defaultLogCleanupConfig } from "@/api/sysconfig";
import type {
  ConfigRequestData,
  LogCleanupConfig,
  LogCleanupDraft,
  LogCleanupUpdateConfig,
  RegularConfigRequestData
} from "@/api/sysconfig";

export { defaultLogCleanupConfig };

export const logCleanupPageRoute = { path: "/system/sysconfig", query: { tab: "logCleanup" } } as const;

// 清理策略是否已启用（服务端 log_cleanup.configured）。
// ⛔⛔ 未启用时操作日志与定时任务日志**一条都不会删**（其余四类不受影响）。
//   页面上「写着 7 天」不等于生效，所以必须由界面显性提示，否则管理员会
//   以为策略在跑 —— 线上机曾因此留了 151 万行、撑到升级预检失败。
export function isLogCleanupEnabled(config?: Pick<LogCleanupConfig, "configured"> | null): boolean {
  return config?.configured === true;
}

const retentionFields: Array<keyof LogCleanupUpdateConfig> = [
  "sipRetentionDays",
  "operationRetentionDays",
  "loginRetentionDays",
  "jobRetentionDays",
  "playbackRetentionDays",
  "schedulerRetentionDays"
];

export function isLogCleanupConfigValid(config: LogCleanupDraft): boolean {
  return retentionFields.every(field => {
    const value = config[field];
    return typeof value === "number" && Number.isInteger(value) && value >= 1 && value <= 365;
  });
}

export function buildConfigUpdatePayload(
  activeTab: string,
  regularConfig: RegularConfigRequestData,
  logCleanup: LogCleanupDraft
): ConfigRequestData {
  if (activeTab !== "logCleanup") return regularConfig;

  const { configured: _configured, ...logCleanupRequest } = logCleanup;
  return { logCleanup: logCleanupRequest as LogCleanupUpdateConfig };
}

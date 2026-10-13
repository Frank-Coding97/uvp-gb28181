import { defaultLogCleanupConfig } from "@/api/sysconfig";
import type { ConfigRequestData, LogCleanupDraft, LogCleanupUpdateConfig, RegularConfigRequestData } from "@/api/sysconfig";

export { defaultLogCleanupConfig };

export const logCleanupPageRoute = { path: "/system/sysconfig", query: { tab: "logCleanup" } } as const;

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

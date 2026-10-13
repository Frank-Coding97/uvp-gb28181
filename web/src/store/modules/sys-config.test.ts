import { beforeEach, describe, expect, it, vi } from "vitest";
import { createPinia, setActivePinia } from "pinia";

const configApi = vi.hoisted(() => ({
  getConfigAPI: vi.fn(),
  updateConfigAPI: vi.fn(),
  defaultLogCleanupConfig: {
    sipRetentionDays: 7,
    operationRetentionDays: 180,
    loginRetentionDays: 180,
    jobRetentionDays: 30,
    playbackRetentionDays: 7,
    schedulerRetentionDays: 7,
    configured: false
  }
}));

vi.mock("@/api/sysconfig", () => configApi);
vi.mock("@/store/config/index", () => ({ default: () => ({}) }));

import { useSysConfigStore } from "./sys-config";

describe("system config store log cleanup persistence", () => {
  beforeEach(() => {
    setActivePinia(createPinia());
    configApi.getConfigAPI.mockReset();
    configApi.updateConfigAPI.mockReset();
  });

  it("refreshes server-managed configuration after a successful empty update response", async () => {
    configApi.updateConfigAPI.mockResolvedValue({ code: 0, message: "", data: null });
    configApi.getConfigAPI.mockResolvedValue({
      code: 0,
      data: {
        logCleanup: {
          sipRetentionDays: 14,
          operationRetentionDays: 90,
          loginRetentionDays: 120,
          jobRetentionDays: 30,
          playbackRetentionDays: 7,
          schedulerRetentionDays: 7,
          configured: true
        }
      }
    });
    const store = useSysConfigStore();

    const response = await store.updateConfig({
      logCleanup: {
        sipRetentionDays: 14,
        operationRetentionDays: 90,
        loginRetentionDays: 120,
        jobRetentionDays: 30,
        playbackRetentionDays: 7,
        schedulerRetentionDays: 7
      }
    });

    expect(response.data).toBeNull();
    expect(configApi.getConfigAPI).toHaveBeenCalledOnce();
    expect(store.logCleanupConfig.configured).toBe(true);
    expect(store.logCleanupConfig.operationRetentionDays).toBe(90);
  });

  it("surfaces an unsuccessful backend message and does not refresh it as a success", async () => {
    configApi.updateConfigAPI.mockResolvedValue({ code: 1, message: "配置已保存，但 SIP 保留期尚未应用到运行中的服务" });
    const store = useSysConfigStore();

    await expect(
      store.updateConfig({
        logCleanup: {
          sipRetentionDays: 14,
          operationRetentionDays: 90,
          loginRetentionDays: 120,
          jobRetentionDays: 30,
          playbackRetentionDays: 7,
          schedulerRetentionDays: 7
        }
      })
    ).rejects.toThrow("配置已保存，但 SIP 保留期尚未应用到运行中的服务");
    expect(configApi.getConfigAPI).not.toHaveBeenCalled();
  });
});

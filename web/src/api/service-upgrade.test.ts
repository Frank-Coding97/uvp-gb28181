import { beforeEach, describe, expect, it, vi } from "vitest";

const request = vi.hoisted(() => vi.fn());

vi.mock("@/utils/http", () => ({ http: { request } }));
vi.mock("./utils", () => ({ baseUrlApi: (path: string) => `/api/${path}` }));

import {
  applyServiceUpgradeTask,
  createServiceUpgradeTask,
  getServiceUpgradeMigrationPlan,
  getServiceUpgradeImport,
  importServiceUpgrade,
  rollbackServiceUpgradeTask,
  updateServiceUpgradePolicy
} from "./service-upgrade";

describe("服务升级 API", () => {
  beforeEach(() => {
    request.mockReset();
    request.mockResolvedValue({ code: 0, data: {} });
  });

  it("任务创建只发送幂等键和摘要，应用/回退使用固定任务路由", async () => {
    await createServiceUpgradeTask("task-key", "a".repeat(64));
    expect(request).toHaveBeenLastCalledWith("post", "/api/system/upgrade/tasks", {
      data: { idempotencyKey: "task-key", targetDigest: "a".repeat(64) }
    });

    await applyServiceUpgradeTask("task/1");
    expect(request).toHaveBeenLastCalledWith("post", "/api/system/upgrade/tasks/task%2F1/apply");

    await rollbackServiceUpgradeTask("task/1");
    expect(request).toHaveBeenLastCalledWith("post", "/api/system/upgrade/tasks/task%2F1/rollback");
  });

  it("策略和迁移计划走独立 API，离线包通过 multipart 上传并查询异步任务", async () => {
    await updateServiceUpgradePolicy({ enabled: false } as any);
    expect(request).toHaveBeenLastCalledWith("put", "/api/system/upgrade/policy", { data: { enabled: false } });

    await getServiceUpgradeMigrationPlan();
    expect(request).toHaveBeenLastCalledWith("get", "/api/system/upgrade/migration-plan");

    const file = new File(["archive"], "upgrade.tar", { type: "application/x-tar" });
    await importServiceUpgrade(file);
    const [, url, options] = request.mock.calls.at(-1)!;
    expect(url).toBe("/api/system/upgrade/import");
    expect(options.headers).toEqual({ "Content-Type": "multipart/form-data" });
    expect(options.timeout).toBe(30 * 60 * 1000);
    expect(options.data).toBeInstanceOf(FormData);
    expect((options.data as FormData).get("file")).toBe(file);

    await getServiceUpgradeImport("job/1");
    expect(request).toHaveBeenLastCalledWith("get", "/api/system/upgrade/import/job%2F1");
  });
});

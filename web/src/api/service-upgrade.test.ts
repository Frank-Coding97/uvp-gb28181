import { beforeEach, describe, expect, it, vi } from "vitest";

const request = vi.hoisted(() => vi.fn());

vi.mock("@/utils/http", () => ({ http: { request } }));
vi.mock("./utils", () => ({ baseUrlApi: (path: string) => `/api/${path}` }));

import {
  applyServiceUpgradeTask,
  clearIgnoredServiceUpgradeNotice,
  createServiceUpgradeTask,
  getServiceUpgradeMigrationPlan,
  getServiceUpgradeImport,
  getServiceUpgradeNotice,
  ignoreServiceUpgradeNotice,
  importServiceUpgrade,
  rollbackServiceUpgradeTask,
  setServiceUpgradeAutoCheck
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

  it("迁移计划走独立 API，离线包通过 multipart 上传并查询异步任务", async () => {
    await getServiceUpgradeMigrationPlan();
    expect(request).toHaveBeenCalledWith("get", "/api/system/upgrade/migration-plan", undefined, { showErrorMessage: false });

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

  it("上传进度回调接收 HTTP 的实际字节事件", async () => {
    const onProgress = vi.fn();
    await importServiceUpgrade(new File(["archive"], "upgrade.tar"), onProgress);
    const options = request.mock.calls.at(-1)![2];
    expect(options.onUploadProgress).toBeTypeOf("function");
    const event = { loaded: 4, total: 10, lengthComputable: true };
    options.onUploadProgress(event);
    expect(onProgress).toHaveBeenCalledWith(event);
  });

  // 徽标读的是助手缓存：GET 必须静默失败（普通账号没有权限时不该弹错），
  // 忽略与取消忽略分别对应 POST / DELETE 同一路由。
  it("更新提示读取静默，忽略与取消忽略走同一路由", async () => {
    await getServiceUpgradeNotice();
    expect(request).toHaveBeenLastCalledWith("get", "/api/system/upgrade/notice", undefined, {
      showErrorMessage: false
    });

    await ignoreServiceUpgradeNotice("1.2.0");
    expect(request).toHaveBeenLastCalledWith("post", "/api/system/upgrade/notice/ignore", {
      data: { version: "1.2.0" }
    });

    await clearIgnoredServiceUpgradeNotice();
    expect(request).toHaveBeenLastCalledWith("delete", "/api/system/upgrade/notice/ignore");
  });

  // 定时检查开关与「忽略版本」是两个独立设置：一个控制整台机器的探测节奏，
  // 一个只藏一个版本，所以各走各的路由，不能合并。
  it("定时检查开关单独一条路由，只发 enabled", async () => {
    await setServiceUpgradeAutoCheck(false);
    expect(request).toHaveBeenLastCalledWith("post", "/api/system/upgrade/notice/auto-check", {
      data: { enabled: false }
    });

    await setServiceUpgradeAutoCheck(true);
    expect(request).toHaveBeenLastCalledWith("post", "/api/system/upgrade/notice/auto-check", {
      data: { enabled: true }
    });
  });
});

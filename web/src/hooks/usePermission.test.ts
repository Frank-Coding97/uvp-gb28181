import { beforeEach, describe, expect, it, vi } from "vitest";

/**
 * `@/hooks/usePermission` 是全平台**唯一**的权限判定入口：
 * 20+ 个页面各自那份 `hasPermission` 实现都收敛到了这里。
 *
 * ⛔ 这里锁的是「通配符放行」这条规则本身 —— 它以前散在 20 多个文件里，
 *    任何一处漏改都会表现成"超管看不到某个按钮"。改成单一实现后，
 *    这条用例就是全平台唯一的守卫点。
 */

const account: { permissions: string[] } = { permissions: [] };

vi.mock("@/store/modules/user", () => ({
  useUserStoreHook: () => ({ account })
}));

import { hasAnyPermission, hasPermission } from "./usePermission";

beforeEach(() => {
  account.permissions = [];
});

describe("usePermission", () => {
  it("持有权限点时放行", () => {
    account.permissions = ["gb28181:device:add"];
    expect(hasPermission("gb28181:device:add")).toBe(true);
  });

  it("未持有的权限点不放行，且不做前缀/子串误判", () => {
    account.permissions = ["gb28181:device:add"];
    expect(hasPermission("gb28181:device:delete")).toBe(false);
    expect(hasPermission("gb28181:device:ad")).toBe(false);
    expect(hasPermission("gb28181:device:add:extra")).toBe(false);
  });

  it("超级管理员通配权限 `*:*:*` 对任意权限点放行", () => {
    account.permissions = ["*:*:*"];
    expect(hasPermission("system:role:add")).toBe(true);
    expect(hasPermission("gb28181:zlm:node:manage")).toBe(true);
  });

  it("hasAnyPermission 命中任一即放行", () => {
    account.permissions = ["gb28181:home:layout:save"];
    expect(hasAnyPermission(["gb28181:home:layout:save", "gb28181:home:layout:reset"])).toBe(true);
    expect(hasAnyPermission(["gb28181:home:layout:reset", "gb28181:home:layout:save"])).toBe(true);
    expect(hasAnyPermission(["gb28181:home:layout:reset"])).toBe(false);
    expect(hasAnyPermission([])).toBe(false);
  });

  it("每次调用都现读 store —— 权限撤销后立刻收紧（不缓存）", () => {
    account.permissions = ["gb28181:play:start"];
    expect(hasPermission("gb28181:play:start")).toBe(true);
    account.permissions = [];
    expect(hasPermission("gb28181:play:start")).toBe(false);
  });
});

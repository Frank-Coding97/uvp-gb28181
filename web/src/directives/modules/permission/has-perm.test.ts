import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { beforeEach, describe, expect, it, vi } from "vitest";

/**
 * `v-hasPerm` 指令契约：**无权限时置灰禁用，而不是把节点从页面上删掉**。
 *
 * ⛔ 这里锁的是「功能可见性」这个产品口径：用户没权限也要**看得到**平台有哪些操作，
 *    只是点不动。旧实现是 `removeChild` —— 一旦有人把它改回去，本文件立刻红。
 */

const account: { permissions: string[] } = { permissions: [] };

vi.mock("@/store/modules/user", () => ({
  useUserStoreHook: () => ({ account })
}));

import hasPerm from "./has-perm";

type Hook = (el: HTMLElement, binding: { value: unknown }) => void;
const mounted = hasPerm.mounted as unknown as Hook;
const updated = hasPerm.updated as unknown as Hook;

const makeButton = () => {
  const el = document.createElement("button");
  el.className = "arco-btn arco-btn-primary";
  document.body.append(el);
  return el;
};

const makeLink = () => {
  const el = document.createElement("a");
  el.className = "arco-link uvp-table-action uvp-table-action--edit";
  document.body.append(el);
  return el;
};

beforeEach(() => {
  account.permissions = [];
  document.body.replaceChildren();
});

describe("v-hasPerm 指令", () => {
  it("持有权限时不添加任何禁用标记", () => {
    account.permissions = ["system:role:add"];
    const el = makeButton();
    mounted(el, { value: ["system:role:add"] });
    expect(el.hasAttribute("disabled")).toBe(false);
    expect(el.hasAttribute("aria-disabled")).toBe(false);
    expect(el.classList.contains("uvp-perm-disabled")).toBe(false);
    expect(el.classList.contains("arco-btn-disabled")).toBe(false);
  });

  it("无权限的按钮：仍留在 DOM 中，但置灰且不可点", () => {
    const el = makeButton();
    mounted(el, { value: ["system:role:add"] });
    expect(document.body.contains(el)).toBe(true);
    expect(el.hasAttribute("disabled")).toBe(true);
    expect(el.getAttribute("aria-disabled")).toBe("true");
    expect(el.classList.contains("arco-btn-disabled")).toBe(true);
    expect(el.classList.contains("uvp-perm-disabled")).toBe(true);
  });

  it("无权限的链接：加 arco-link-disabled，但不加 disabled 属性（a 标签没有该语义）", () => {
    const el = makeLink();
    mounted(el, { value: ["system:role:edit"] });
    expect(document.body.contains(el)).toBe(true);
    expect(el.classList.contains("arco-link-disabled")).toBe(true);
    expect(el.classList.contains("uvp-perm-disabled")).toBe(true);
    expect(el.hasAttribute("disabled")).toBe(false);
  });

  it("指令挂在包裹层上时（如 a-upload 包的触发按钮），把禁用态下沉到内部控件", () => {
    // 场景：`system/affix` 的 `<a-upload v-hasPerm="['system:affix:upload']">`，
    // 真正可点的是它 `#upload-button` 插槽里的那个 a-button。
    const host = document.createElement("div");
    host.className = "arco-upload";
    const inner = document.createElement("button");
    inner.className = "arco-btn arco-btn-primary";
    host.append(inner);
    document.body.append(host);

    mounted(host, { value: ["system:affix:upload"] });
    expect(host.classList.contains("uvp-perm-disabled")).toBe(true);
    // ⛔ 宿主是 div，不该被压上链接禁用色（那是给 a 标签的）
    expect(host.classList.contains("arco-link-disabled")).toBe(false);
    expect(inner.hasAttribute("disabled")).toBe(true);
    expect(inner.classList.contains("arco-btn-disabled")).toBe(true);

    account.permissions = ["system:affix:upload"];
    updated(host, { value: ["system:affix:upload"] });
    expect(host.classList.contains("uvp-perm-disabled")).toBe(false);
    expect(inner.hasAttribute("disabled")).toBe(false);
    expect(inner.classList.contains("arco-btn-disabled")).toBe(false);
  });

  it("声明多个权限码时命中任一即放行", () => {
    account.permissions = ["system:role:delete"];
    const el = makeButton();
    mounted(el, { value: ["system:role:add", "system:role:delete"] });
    expect(el.classList.contains("uvp-perm-disabled")).toBe(false);
  });

  it("超级管理员通配权限放行", () => {
    account.permissions = ["*:*:*"];
    const el = makeButton();
    mounted(el, { value: ["system:role:add"] });
    expect(el.classList.contains("uvp-perm-disabled")).toBe(false);
  });

  it("重复执行（mounted + updated 反复触发）是幂等的", () => {
    const el = makeButton();
    mounted(el, { value: ["system:role:add"] });
    updated(el, { value: ["system:role:add"] });
    updated(el, { value: ["system:role:add"] });
    expect(el.className.split(/\s+/).filter(c => c === "arco-btn-disabled")).toHaveLength(1);
    expect(el.className.split(/\s+/).filter(c => c === "uvp-perm-disabled")).toHaveLength(1);
  });

  it("权限恢复后还原可用，但不越权解除元素自身业务原因的 disabled", () => {
    // 场景：按钮因「没选中记录」本就禁用（:disabled），与权限无关
    const ownDisabled = makeButton();
    ownDisabled.setAttribute("disabled", "");
    mounted(ownDisabled, { value: ["system:role:add"] });
    account.permissions = ["system:role:add"];
    updated(ownDisabled, { value: ["system:role:add"] });
    expect(ownDisabled.classList.contains("uvp-perm-disabled")).toBe(false);
    expect(ownDisabled.hasAttribute("disabled")).toBe(true);

    // 场景：按钮本就可用，权限恢复后应完全还原
    const normal = makeButton();
    mounted(normal, { value: ["system:role:add"] });
    updated(normal, { value: ["system:role:add"] });
    expect(normal.hasAttribute("disabled")).toBe(false);
    expect(normal.hasAttribute("aria-disabled")).toBe(false);
  });

  it("绑定值缺失时按无权限处理，但绝不删除节点", () => {
    const error = vi.spyOn(console, "error").mockImplementation(() => undefined);
    const el = makeButton();
    mounted(el, { value: undefined });
    expect(document.body.contains(el)).toBe(true);
    expect(el.classList.contains("uvp-perm-disabled")).toBe(true);
    expect(error).toHaveBeenCalled();
    error.mockRestore();
  });
});

describe("权限置灰的样式兜底", () => {
  it("uvp-perm-disabled 必须同时置灰与阻断交互（不能只改颜色）", () => {
    const scss = readFileSync(resolve(process.cwd(), "src/style/model/uvp-ui-language.scss"), "utf8");
    const block = scss.slice(scss.indexOf(".uvp-perm-disabled {"));
    const rule = block.slice(0, block.indexOf("}"));
    expect(rule).toContain("pointer-events: none");
    expect(rule).toContain("cursor: not-allowed");
  });
});

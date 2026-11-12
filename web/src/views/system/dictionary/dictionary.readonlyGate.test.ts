import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";
import { hasMarkup, squash } from "@/test/source-assert";

/**
 * 字典管理页的「系统内置字典只读闸门」——台账 §3.2 方案 B（零改表，纯前端）。
 *
 * ⛔ 为什么用源码断言：本页五个改动入口（字典的改/删、字典项的新增/改/删）都靠模板属性 +
 *    函数守卫拦住，桩测看不见 prop 落点（本仓踩过「按我们以为的 prop 建模」的坑）。
 *    这里直接钉「每个入口都有守卫」，漏一个就红。
 */
const source = readFileSync(resolve(process.cwd(), "src/views/system/dictionary/dictionary.vue"), "utf8");

/** 取出某个 handler 的函数体（到第一个行首 `};` 为止），压成单行便于 token 比对。 */
function bodyOf(name: string): string {
  const matched = source.match(new RegExp(`const ${name} = [\\s\\S]*?\\n\\};`));
  if (!matched) throw new Error(`找不到 ${name} 的函数体`);
  return squash(matched[0]);
}

describe("字典管理页 · 系统内置字典只读闸门", () => {
  it("模板：外层字典的「修改 / 删除」按 code 置灰（修改、删除链接、确认气泡 共 3 处）", () => {
    const token = squash(':disabled="isSystemDict(record.code)"');
    expect(squash(source).split(token).length - 1).toBe(3);
  });

  it("模板：详情弹窗的「新增」与逐项「修改 / 删除」按当前字典置灰（新增、改、删、确认气泡 共 4 处）", () => {
    const token = squash(':disabled="currentDictReadonly"');
    expect(squash(source).split(token).length - 1).toBe(4);
  });

  it("详情弹窗有可见的只读说明条（不是只靠置灰）", () => {
    expect(hasMarkup(source, 'class="dict-readonly-hint"')).toBe(true);
    expect(hasMarkup(source, "{{ SYSTEM_DICT_READONLY_HINT }}")).toBe(true);
  });

  it("编码列有锁标提示（tooltip 挂 SYSTEM_DICT_READONLY_HINT）", () => {
    expect(hasMarkup(source, "<icon-lock")).toBe(true);
    expect(hasMarkup(source, ':content="SYSTEM_DICT_READONLY_HINT"')).toBe(true);
  });

  it("⛔ 每个「改动」入口都有函数守卫（按钮置灰之外的双保险）", () => {
    // 外层字典：改 / 删
    expect(bodyOf("onUpdate")).toContain(squash("if (isSystemDict(record.code)) return;"));
    expect(bodyOf("onDelete")).toContain(squash("if (isSystemDict(record.code)) return;"));
    // 内层字典项：新增 / 改 / 删
    expect(bodyOf("onAddDetail")).toContain(squash("if (currentDictReadonly.value) return;"));
    expect(bodyOf("onDetailUpdate")).toContain(squash("if (currentDictReadonly.value) return;"));
    expect(bodyOf("onDeleteDetail")).toContain(squash("if (currentDictReadonly.value) return;"));
  });

  it("只读判定来自 systemDictCodes（单一真源，不在页面里另写一份名单）", () => {
    expect(hasMarkup(source, 'import { SYSTEM_DICT_READONLY_HINT, isSystemDict } from "./systemDictCodes"')).toBe(true);
    expect(hasMarkup(source, "const currentDictReadonly = computed(() => isSystemDict(currentDict.value?.code))")).toBe(true);
  });
});

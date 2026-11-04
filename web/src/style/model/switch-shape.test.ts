import { readdirSync, readFileSync } from "node:fs";
import { join, relative, resolve } from "node:path";
import { describe, expect, it } from "vitest";

/**
 * 开关形状门禁：全站开关一律圆角（Arco `type="circle"`，也是默认值）。
 *
 * `type="round"` 是「方角滑块」，和页面其它圆角控件混在一起很突兀；
 * 历史上有 17 处用它，2026-11-04 已统一，这条断言防止被写回。
 * `type="line"`（细线型）是另一种刻意样式（Tabs、视频预览），不在禁止范围。
 */
const SRC = resolve(process.cwd(), "src");

const walk = (dir: string): string[] =>
  readdirSync(dir, { withFileTypes: true }).flatMap(entry => {
    const full = join(dir, entry.name);
    if (entry.isDirectory()) return walk(full);
    return entry.name.endsWith(".vue") ? [full] : [];
  });

const files = walk(SRC);
const rel = (file: string) => relative(process.cwd(), file);
// 全仓扫描放模块加载期：机器繁忙时逐文件同步读会顶到默认 5s 超时（曾误报过一条红灯）
const sources = new Map(files.map(file => [file, readFileSync(file, "utf8")]));

describe("开关形状：一律圆角", () => {
  it("扫描到的 .vue 文件数正常（防止目录写错后静默通过）", () => {
    expect(files.length).toBeGreaterThan(100);
  });

  it("没有页面使用方角滑块的 type=round", () => {
    const offenders = files.filter(file => /<a-switch[^>]*\stype="round"/.test(sources.get(file) ?? ""));

    expect(offenders.map(rel)).toEqual([]);
  });

  it("账号管理的状态开关显式声明 circle（不依赖组件库默认值）", () => {
    const account = sources.get(resolve(SRC, "views/system/account/account.vue")) ?? "";

    expect(account).toMatch(/<a-switch type="circle" :checked-value="1" :unchecked-value="0"/);
  });

  it("全局兜底：任何 type 的开关滑块都强制圆形", () => {
    const overrides = readFileSync(resolve(SRC, "styles/arco-overrides.scss"), "utf8");

    expect(overrides).toMatch(/\.arco-switch\s+\.arco-switch-handle\s*\{[^}]*border-radius:\s*50%/);
  });

  it("分配权限抽屉：文字与开关用 flex 留间距，不靠 inline 空白", () => {
    const role = sources.get(resolve(SRC, "views/system/role/role.vue")) ?? "";
    const drawer = role.match(/<template #title> 分配权限 <\/template>[\s\S]*?<\/a-drawer>/)?.[0] ?? "";

    expect(drawer).toContain('class="perm-switch"');
    expect(drawer).not.toContain("text-right-gap");
    expect(role).toMatch(/\.perm-switch\s*\{[^}]*gap:\s*\d+px/);
  });
});

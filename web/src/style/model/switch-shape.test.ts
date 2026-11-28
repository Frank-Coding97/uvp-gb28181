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

  it("全局兜底：任何 type 的开关滑块都强制圆形", () => {
    const overrides = readFileSync(resolve(SRC, "styles/arco-overrides.scss"), "utf8");

    expect(overrides).toMatch(/\.arco-switch\s+\.arco-switch-handle\s*\{[^}]*border-radius:\s*50%/);
  });

  it("系统管理下的开关：全部显式 circle，且不出现 small 尺寸", () => {
    const systemFiles = files.filter(file => rel(file).includes("views/system/"));
    const offenders: string[] = [];

    for (const file of systemFiles) {
      const source = sources.get(file) ?? "";
      for (const [tag] of source.matchAll(/<a-switch\b[^>]*>/g)) {
        if (!/type="circle"/.test(tag) || /size="small"/.test(tag)) offenders.push(`${rel(file)} → ${tag.replace(/\s+/g, " ")}`);
      }
    }

    expect(offenders).toEqual([]);
  });

  it("「文字 + 开关」统一用 uvp-switch-row 留间距，不靠 inline 空白或局部 margin", () => {
    const language = readFileSync(resolve(SRC, "style/model/uvp-ui-language.scss"), "utf8");

    // 全局真源：间距只在这里定义，页面不再各写一套
    expect(language).toMatch(/\.uvp-switch-row\s*\{[^}]*gap:\s*10px/);

    const role = sources.get(resolve(SRC, "views/system/role/role.vue")) ?? "";
    const drawer = role.match(/<template #title> 分配权限 <\/template>[\s\S]*?<\/a-drawer>/)?.[0] ?? "";
    expect(drawer).toContain('class="uvp-switch-row"');

    const datascope = sources.get(resolve(SRC, "views/system/role/components/datascope.vue")) ?? "";
    expect(datascope).toContain('class="uvp-switch-row"');

    const upgrade = sources.get(resolve(SRC, "views/system/service-upgrade/index.vue")) ?? "";
    expect(upgrade).toContain('class="uvp-switch-row"');

    // 旧写法：抄了类名却忘了在页面里定义，实测间距为 0
    const deadClass = files.filter(file => /class="[^"]*\btext-right-gap\b/.test(sources.get(file) ?? ""));
    expect(deadClass.map(rel)).toEqual([]);
  });
});

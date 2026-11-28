import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

/**
 * ⛔⛔ 侧栏「双滚动条」守卫。
 *
 * Arco 的 LayoutSider 自带 `.arco-layout-sider-children { height:100%; overflow:auto }`，
 * Arco 的 Menu 又自带 `.arco-menu-inner { height:100%; overflow:auto }`；而三个侧栏
 * （Aside / layout-mixing / drawer-aside）在 sider 里另套了一层 a-scrollbar。
 * 只要中间任一层判定「内容高出几个像素」，它就会在 a-scrollbar 的 4px 自绘滑块旁边
 * 再画一条 6px 原生滚动条 —— 2026-11-09 实机截图取证：两条滑块相距约 6px，
 * 外侧那条是 #c3cfdd 实色的 --uvp-scrollbar-thumb（原生），内侧是 0.8 透明度的同色（Arco）。
 *
 * ⇒ 滚动权只能归 a-scrollbar：sider-children 收起 overflow，menu-inner 放开
 *   （menu-inner 若保持 auto 且高度确定，会把菜单内容裁掉且滚不动）。
 */
const SIDEBAR_FILES = [
  "src/layout/components/Aside/index.vue",
  "src/layout/layout-mixing/index.vue",
  "src/layout/components/drawer-aside/index.vue"
];

function styleBlockOf(source: string, selector: string) {
  // 注释里会写「Arco 原本是 xxx { overflow: auto }」这类说明，先剥掉再匹配，避免命中注释
  const code = source.replace(/\/\*[\s\S]*?\*\//g, "").replace(/^\s*\/\/.*$/gm, "");
  const escaped = selector.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  // 组件里既可能裸写选择器，也可能包在 `:deep(...)` 里
  const pattern = new RegExp(`(?::deep\\(\\s*)?${escaped}\\s*\\)?\\s*\\{([^}]*)\\}`, "s");
  const match = code.match(pattern);
  return match ? match[1] : null;
}

describe("sidebar keeps a single scroll layer", () => {
  for (const file of SIDEBAR_FILES) {
    const source = readFileSync(resolve(process.cwd(), file), "utf8");

    it(`${file} hides the overflow of Arco's LayoutSider children`, () => {
      const block = styleBlockOf(source, ".arco-layout-sider-children");
      expect(block, `${file} 缺少 .arco-layout-sider-children 收敛规则`).not.toBeNull();
      expect(block).toMatch(/overflow:\s*hidden/);
    });

    it(`${file} releases the menu inner overflow so the menu is not clipped`, () => {
      const block = styleBlockOf(source, ".arco-menu-inner");
      expect(block, `${file} 缺少 .arco-menu-inner 规则`).not.toBeNull();
      expect(block).toMatch(/overflow:\s*visible/);
    });

    it(`${file} still lets the a-scrollbar wrapper fill the sider`, () => {
      expect(source).toMatch(/\.scrollbar\s*\{[^}]*height:\s*100%/s);
    });
  }

  it("keeps a global fallback for future sidebars that wrap a-scrollbar", () => {
    const overrides = readFileSync(resolve(process.cwd(), "src/styles/arco-overrides.scss"), "utf8");
    expect(overrides).toMatch(/\.arco-layout-sider-children:has\(> \.arco-scrollbar\)\s*\{[^}]*overflow:\s*hidden/s);
  });
});

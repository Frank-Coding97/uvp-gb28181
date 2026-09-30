import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";
import { hasRuleBlock, squash } from "@/test/source-assert";

const source = readFileSync(resolve(process.cwd(), "src/style/model/uvp-ui-language.scss"), "utf8");

/** 取出「选择器含 `.uvp-system-drawer` 但没往下打到面板」的规则块 —— 即落在 Arco 全屏容器上的那批。 */
function containerScopedDrawerRules(): { selector: string; body: string }[] {
  return [...source.matchAll(/([^{}]+)\{([^{}]*)\}/gs)]
    .map(([, selector, body]) => ({ selector: squash(selector), body: squash(body) }))
    .filter(rule => rule.selector.includes(".uvp-system-drawer") && !rule.selector.includes(".arco-drawer"));
}

describe("table action spacing", () => {
  it("keeps one compact 4px gap between operation icons and labels", () => {
    expect(source).toMatch(/\.uvp-data-table \.uvp-table-action,\s*\.uvp-data-table \.arco-link\s*\{[^}]*gap:\s*4px;/s);
    expect(source).toMatch(/\.uvp-data-table \.arco-link-icon\s*\{[^}]*margin-right:\s*0;/s);
    expect(source).toMatch(
      /\.uvp-data-table \.arco-btn:not\(\.arco-btn-only-icon\) \.arco-btn-icon\s*\{[^}]*margin-right:\s*4px;/s
    );
  });

  it("centers slotted SVG icons against action labels without baseline drift", () => {
    expect(source).toMatch(
      /\.uvp-data-table \.arco-link-icon\s*\{[^}]*display:\s*inline-flex;[^}]*align-items:\s*center;[^}]*justify-content:\s*center;[^}]*line-height:\s*1;/s
    );
    expect(source).toMatch(/\.uvp-data-table \.arco-link-icon\s*>\s*svg\s*\{[^}]*display:\s*block;/s);
  });
});

describe("media workbench theme tokens", () => {
  it("maps ZLM surfaces to the active UVP body theme for legacy and canonical routes", () => {
    expect(source).toMatch(/body,\s*\.gb28181-page,[^{]*\{[^}]*--zlm-card:\s*var\(--uvp-panel-bg\)/s);
    expect(source).toMatch(/body\[arco-theme="dark"\],\s*body\[arco-theme="dark"\]\s+\.gb28181-page,[^{]*\{/s);
  });
});

describe("table loading mask theme", () => {
  it("uses a dark translucent surface instead of the light loading mask in dark mode", () => {
    expect(source).toMatch(
      /body\[arco-theme="dark"\]\s+\.uvp-data-table\s+\.arco-spin-mask\s*\{[^}]*background:\s*rgb\(16 25 35\s*\/\s*78%\)/s
    );
  });
});

describe("system drawer surface scoping", () => {
  // `class="uvp-system-drawer"` 落在 Arco 的 `.arco-drawer-container`（fixed 全屏、z-index 1001）上，
  // 不是抽屉面板；面板是内层 `.arco-drawer`。表面样式一旦写在容器上，整屏会被不透明底盖住，
  // 打开抽屉时背后页面「整片消失」（2026-09-23 实测：采样像素 rgb(119,122,127) = 白底 + 60% 遮罩）。
  it("paints the drawer surface on the inner panel, not on Arco's full-screen container", () => {
    expect(hasRuleBlock(source, ".uvp-system-drawer .arco-drawer", "background: var(--uvp-dialog-bg)")).toBe(true);
    expect(hasRuleBlock(source, ".uvp-system-drawer .arco-drawer", "border-radius: 16px 0 0 16px")).toBe(true);
  });

  it("keeps the container-scoped drawer rules free of any opaque surface", () => {
    const rules = containerScopedDrawerRules();
    // 容器上只允许留「自定义属性」这类规则（如 --uvp-dialog-control-bg）
    for (const rule of rules) {
      for (const banned of ["background", "box-shadow", "border-radius", "border:"]) {
        expect(`${rule.selector} => ${rule.body}`).not.toContain(banned);
      }
    }
    expect(rules.length).toBeGreaterThan(0); // 保住「自定义属性仍挂在容器上」这条事实
  });
});

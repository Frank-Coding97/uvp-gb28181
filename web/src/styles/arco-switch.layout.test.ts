import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const source = readFileSync(resolve(process.cwd(), "src/styles/arco-overrides.scss"), "utf8");
const buttons = readFileSync(resolve(process.cwd(), "src/styles/uvp-buttons.scss"), "utf8");

describe("global switch styling", () => {
  it("distinguishes usable off, on and disabled states in dark mode", () => {
    expect(source).toMatch(/\.arco-switch\s*\{[^}]*background-color:\s*var\(--uvp-panel-border\)/s);
    expect(source).toMatch(/&\.arco-switch-checked\s*\{[^}]*background-color:\s*var\(--uvp-brand\)/s);
    expect(source).toMatch(/&\[disabled\]\s*\{[^}]*opacity:\s*0\.5/s);
  });

  it("keeps the checked switch crisp in dark mode without translucent overlays", () => {
    expect(source).toMatch(
      /body\[arco-theme="dark"\]\s+\.arco-switch\.arco-switch-checked\s*\{[^}]*background-color:\s*#2563eb\s*!important;[^}]*box-shadow:\s*none\s*!important;/s
    );
    expect(source).toMatch(
      /body\[arco-theme="dark"\]\s+\.arco-switch\.arco-switch-checked\s+\.arco-switch-handle\s*\{[^}]*background-color:\s*#(?:fff|ffffff)\s*!important;[^}]*box-shadow:\s*none\s*!important;/s
    );
  });

  it("provides a distinct adaptive color for refresh actions", () => {
    // 2026-11-04：刷新语义从 arco-overrides 迁入唯一真源 uvp-buttons.scss
    // （那边原来是 `!important` 硬压，与「六级语义从一处出」冲突）。
    expect(buttons).toMatch(/\.uvp-refresh-btn\s*\{[^}]*background:\s*var\(--uvp-refresh-btn-bg\)/s);
    expect(buttons).toMatch(/\.uvp-refresh-btn\s*\{[^}]*border-color:\s*var\(--uvp-refresh-btn-border\)/s);
    expect(buttons).toMatch(/\.uvp-refresh-btn[\s\S]*&:hover[^}]*background:\s*var\(--uvp-refresh-btn-hover-bg\)/s);
    // 反向：overrides 里不许再留第二份定义。
    expect(source).not.toMatch(/\.uvp-refresh-btn\s*\{/);
  });

  it("defines shared page-action sizing and create semantics", () => {
    // 2026-11-04 起尺寸与配色都走 --uvp-btn-* token（唯一真源 uvp-buttons.scss），
    // 这里只锁「页头 CTA = lg 档 + 统一圆角」「新增 = 绿实心」两条语义，不再钉字面色值。
    expect(source).toMatch(/\.uvp-page-action-btn\s*\{[^}]*height:\s*var\(--uvp-btn-h-lg\)/s);
    expect(source).toMatch(/\.uvp-page-action-btn\s*\{[^}]*border-radius:\s*var\(--uvp-btn-radius\)/s);
    expect(source).toMatch(/\.uvp-create-btn\s*\{[^}]*background:\s*var\(--uvp-btn-success-bg\)/s);
    expect(source).toMatch(/\.uvp-create-btn[\s\S]*&:hover[^}]*background:\s*var\(--uvp-btn-success-bg-hover\)/s);
  });
});

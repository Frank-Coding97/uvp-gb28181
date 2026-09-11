import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const source = readFileSync(resolve(process.cwd(), "src/layout/components/Tabs/index.vue"), "utf8");

describe("workspace tabs", () => {
  it("exposes display actions in desktop tabs and directly in the mobile header", () => {
    const headerRightSource = readFileSync(
      resolve(process.cwd(), "src/layout/components/Header/components/header-right/index.vue"),
      "utf8"
    );
    const fullscreenIndex = source.indexOf('id="system-tabs-fullscreen"');
    const themeIndex = source.indexOf('id="system-tabs-theme"');
    const refreshIndex = source.indexOf('id="system-tabs-refresh"');
    const settingIndex = source.indexOf('id="system-tabs-setting"');

    expect(refreshIndex).toBe(-1);
    expect(settingIndex).toBe(-1);
    expect(fullscreenIndex).toBeGreaterThan(-1);
    expect(source).toContain('trigger="contextMenu"');
    expect(themeIndex).toBeGreaterThan(fullscreenIndex);
    expect(source).toContain("@click=\"onFullScreen\"");
    expect(source).toContain("@click=\"toggleThemeMode\"");
    expect(source).toContain("darkMode ? '明亮' : '暗色'");
    expect(source).not.toContain("切换至夜间蓝灰");
    expect(source).not.toContain("切换至明亮模式");
    const actions = readFileSync(resolve(process.cwd(), "src/layout/components/Header/useHeaderDisplayActions.ts"), "utf8");
    expect(actions).toContain('document.addEventListener("fullscreenchange", syncFullScreen)');
    expect(headerRightSource).toContain('<div v-if="isMobile" class="header-display-actions">');
    expect(headerRightSource).toContain('@click="onFullScreen"');
    expect(headerRightSource).toContain('@click="toggleThemeMode"');
    expect(headerRightSource).toContain("@click=\"onSystemSetting\"");
  });

  it("renders each opened route icon before its localized title", () => {
    expect(source).toContain('<template #title>');
    expect(source).toContain("<MenuItemIcon v-if=\"item.meta.svgIcon || item.meta.icon\" :svg-icon=\"item.meta.svgIcon\" :icon=\"item.meta.icon\" />");
    expect(source).toContain("$t(`menu.${item.meta.title}`)");
    expect(source).toContain(".arco-tabs-tab-close-btn svg");
    expect(source).toContain(".arco-tabs-nav-type-line .arco-tabs-tab");
    expect(source).toContain("margin: 0 2px;");
  });

  it("renders the active workspace tab with the reference browser-tab shape", () => {
    expect(source).toContain(":deep(.arco-tabs-tab-active)");
    expect(source).toContain("background: var(--color-primary-light-1);");
    expect(source).toContain("height: 34px;");
    expect(source).toContain("margin: 0 2px;");
    expect(source).toContain("padding: 6px 10px;");
    expect(source).toContain("border-radius: 8px;");
    expect(source).toContain(".arco-tabs-tab-closable");
    expect(source).toContain("width: 1em;");
    expect(source).toContain(":deep(.arco-tabs-nav-ink)");
    expect(source).toContain("display: none;");
    expect(source).not.toContain("box-shadow: inset 0 -2px 0 rgb(var(--primary-6));");
    expect(source).not.toContain(".arco-tabs-tab-active)::before");
    expect(source).not.toContain(".arco-tabs-tab-active)::after");
  });
});

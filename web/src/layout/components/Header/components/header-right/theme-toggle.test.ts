import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it } from "vitest";
import { ref, watch } from "vue";
import { useThemeMethods } from "@/hooks/useThemeMethods";
import { useThemeConfig } from "@/store/modules/theme-config";

const readSource = (path: string) => readFileSync(resolve(process.cwd(), path), "utf8");

describe("header theme toggle", () => {
  beforeEach(() => {
    Object.assign(globalThis, { ref, watch });
    setActivePinia(createPinia());
    document.body.removeAttribute("arco-theme");
    document.body.removeAttribute("uvp-dark-style");
  });

  it("switches directly between light and night blue-gray without a style dropdown", () => {
    const headerSource = readSource("src/layout/components/Header/components/header-right/index.vue");
    const settingsSource = readSource("src/layout/components/Header/components/theme-settings/index.vue");

    expect(headerSource).toContain('@click="toggleThemeMode"');
    expect(headerSource).not.toContain("onThemeModeSelect");
    expect(settingsSource).not.toContain("暗色风格");
  });

  it("removes the retired system notice entry from the header", () => {
    const headerSource = readSource("src/layout/components/Header/components/header-right/index.vue");

    expect(headerSource).not.toContain("system-notice");
    expect(headerSource).not.toContain("<Notice />");
    expect(headerSource).not.toContain("components/Header/components/Notice");
  });

  it("removes the retired frosted-black theme from runtime and styles", () => {
    const sources = [
      "src/store/modules/theme-config.ts",
      "src/hooks/useThemeMethods.ts",
      "src/layout/components/Header/components/header-right/index.vue",
      "src/layout/components/Header/components/theme-settings/index.vue",
      "src/views/gb28181/device-record-playback/index.vue",
      "src/style/var/uvp-ui-tokens.scss",
      "src/style/model/uvp-ui-language.scss"
    ].map(readSource);

    expect(sources.join("\n")).not.toContain("frostedBlack");
  });

  it("clears a stale dark-style attribute when enabling the night blue-gray theme", () => {
    const themeStore = useThemeConfig();
    document.body.setAttribute("uvp-dark-style", "frostedBlack");

    themeStore.darkMode = true;
    useThemeMethods().setDarkMode();

    expect(document.body.getAttribute("arco-theme")).toBe("dark");
    expect(document.body.hasAttribute("uvp-dark-style")).toBe(false);
  });

  it("uses adaptive theme tokens for select and dropdown surfaces", () => {
    const overridesSource = readSource("src/styles/arco-overrides.scss");

    expect(overridesSource).toMatch(/\.arco-select-view\s*\{[^}]*background:\s*var\(--uvp-dialog-control-bg\)/s);
    expect(overridesSource).toMatch(/\.arco-select-dropdown\s*\{[^}]*background:\s*var\(--uvp-popconfirm-bg\)/s);
    expect(overridesSource).toMatch(/\.arco-select-option\s*\{[^}]*background:\s*var\(--uvp-popconfirm-bg\)/s);
    expect(overridesSource).toMatch(/\.arco-dropdown\s*\{[^}]*background:\s*var\(--uvp-popconfirm-bg\)/s);
  });

  it("keeps custom primary buttons visually active in dark mode", () => {
    const overridesSource = readSource("src/styles/arco-overrides.scss");

    expect(overridesSource).toMatch(/body\[arco-theme="dark"\]\s+button\.btn-primary\s*\{/);
    expect(overridesSource).toMatch(/button\.btn-primary:not\(:disabled\):hover/);
    expect(overridesSource).toMatch(/button\.btn-primary:not\(:disabled\):active/);
    expect(overridesSource).toMatch(/button\.btn-primary:disabled/);
  });

  it("keeps the dark sidebar brand area on the navigation surface", () => {
    const tokensSource = readSource("src/style/var/uvp-ui-tokens.scss");

    expect(tokensSource).toMatch(/body\[arco-theme="dark"\]\s*\{[^}]*--uvp-sidebar-bg:\s*var\(--uvp-navigation-bg\)/s);
  });

  it("switches themes without injecting a page transition", () => {
    const headerSource = readSource("src/layout/components/Header/components/header-right/index.vue");
    const themeStyles = readSource("src/style/model/uvp-ui-language.scss");

    expect(headerSource).not.toContain("uvp-theme-transitioning");
    expect(headerSource).not.toContain("startViewTransition");
    expect(themeStyles).not.toContain("uvp-theme-diagonal-wipe");
  });
});

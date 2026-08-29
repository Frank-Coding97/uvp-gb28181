import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const readSource = (path: string) => readFileSync(resolve(process.cwd(), path), "utf8");

describe("default header workspace layout", () => {
  it("renders dynamic tabs between the collapse control and account actions", () => {
    const header = readSource("src/layout/components/Header/index.vue");
    const headerLeft = readSource("src/layout/components/Header/components/header-left/index.vue");
    const main = readSource("src/layout/components/Main/index.vue");

    expect(header).toContain('<div v-if="isTabs" class="header_tabs">');
    expect(header).toContain("<Tabs />");
    expect(header).toContain("grid-template-columns: auto minmax(0, 1fr) auto;");
    expect(headerLeft).not.toContain("Breadcrumb");
    expect(main).not.toContain("<Tabs");
  });

  it("keeps the tabs enabled by default and removes breadcrumb rendering by default", () => {
    const themeConfig = readSource("src/store/modules/theme-config.ts");
    const systemSettings = readSource("src/layout/components/Header/components/system-settings/index.vue");

    expect(themeConfig).toContain("const isTabs = ref<boolean>(true);");
    expect(themeConfig).toContain("const isBreadcrumb = ref<boolean>(false);");
    expect(systemSettings).not.toContain("system.breadcrumb");
    expect(systemSettings).not.toContain("v-model=\"isBreadcrumb\"");
  });

  it("keeps workspace tabs centered in the header slot", () => {
    const header = readSource("src/layout/components/Header/index.vue");

    expect(header).toContain("height: 40px;");
    expect(header).toContain("align-items: center;");
    expect(header).toContain("overflow: hidden;");
    expect(header).not.toContain("margin: -8px 0;");
  });

  it("gives the sidebar collapse control an accessible stateful name", () => {
    const collapseButton = readSource("src/layout/components/Header/components/button-collapsed/index.vue");

    expect(collapseButton).toContain(':aria-label="collapsed ? \'展开侧栏\' : \'收起侧栏\'"');
  });
});

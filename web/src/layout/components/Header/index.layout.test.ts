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

    expect(themeConfig).toContain("const isTabs = ref<boolean>(true);");
    expect(themeConfig).toContain("const isBreadcrumb = ref<boolean>(false);");
  });
});

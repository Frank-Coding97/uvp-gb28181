import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const source = readFileSync(resolve(process.cwd(), "src/layout/components/Tabs/index.vue"), "utf8");

describe("workspace tabs", () => {
  it("renders each opened route icon before its localized title", () => {
    expect(source).toContain('<template #title>');
    expect(source).toContain("<MenuItemIcon v-if=\"item.meta.svgIcon || item.meta.icon\" :svg-icon=\"item.meta.svgIcon\" :icon=\"item.meta.icon\" />");
    expect(source).toContain("$t(`menu.${item.meta.title}`)");
    expect(source).toContain(".arco-tabs-tab-close-btn svg");
    expect(source).toContain(".arco-tabs-nav-type-line .arco-tabs-tab");
    expect(source).toContain("margin: 0 10px;");
  });
});

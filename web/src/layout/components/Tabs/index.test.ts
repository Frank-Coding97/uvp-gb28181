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

import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";
import { hasMarkup, squash } from "@/test/source-assert";

const source = readFileSync(resolve(process.cwd(), "src/views/system/menu/menu.vue"), "utf8");

describe("menu page workspace layout", () => {
  it("uses the fill shell so the page never scrolls its own container", () => {
    expect(source).toContain('class="snow-fill menu-page"');
    expect(source).toContain('class="snow-fill-inner uvp-page-shell-flat menu-page__inner"');
    expect(source).not.toContain('class="snow-page menu-page"');
    expect(source).not.toContain('class="snow-inner uvp-page-shell-flat menu-page__inner"');
  });

  it("stacks the shell as a flex column that keeps the table as the only growable row", () => {
    expect(source).toMatch(/\.menu-page\s*{[^}]*overflow:\s*hidden;/s);
    expect(source).toMatch(/\.menu-page__inner\s*{[^}]*display:\s*flex;[^}]*flex-direction:\s*column;[^}]*overflow:\s*hidden;/s);
    expect(source).toMatch(/\.menu-page__inner > :deep\(\.uvp-search-panel\)\s*{[^}]*flex:\s*0 0 auto;/s);
    expect(source).toMatch(/\.menu-table-wrap\s*{[^}]*flex:\s*1;[^}]*min-height:\s*0;[^}]*overflow:\s*hidden;/s);
    expect(source).toMatch(/\.menu-table-wrap :deep\(\.uvp-data-table\)\s*{[^}]*height:\s*100%;[^}]*min-height:\s*0;/s);
  });

  it("moves the vertical scrollbar onto the table body once rows exist", () => {
    // ⛔ 无条件给 y: "100%" 会把空表也撑成固定高度，空态那行被拉满
    expect(source).toContain('...(displayMenuList.value.length > 0 ? { y: "100%" } : {})');
    expect(source).not.toMatch(/scroll="\{[^}]*y:\s*"100%"\s*\}"/);
    expect(source).toContain('<div class="menu-table-wrap">');
    expect(source).toContain('class="uvp-data-table"');
    expect(source).toContain(':scroll="tableScroll"');
  });

  it("keeps the search panel above the scrolling table", () => {
    const searchIndex = source.indexOf("<s-layout-search>");
    const tableIndex = source.indexOf('class="menu-table-wrap"');
    expect(searchIndex).toBeGreaterThan(-1);
    expect(tableIndex).toBeGreaterThan(searchIndex);
  });

  it("gives every row action an icon, including API权限", () => {
    const start = source.indexOf('class="uvp-table-actions"');
    const end = source.indexOf("</a-table-column>", start);
    expect(start).toBeGreaterThan(-1);
    expect(end).toBeGreaterThan(start);
    const actions = source.slice(start, end);

    const links = actions.match(/class="uvp-table-action /g) ?? [];
    const icons = squash(actions).match(/<template#icon>/g) ?? [];
    expect(links.length).toBe(4);
    // 四个动作一律「图标 + 文案」，避免 API权限 单独少一个图标
    expect(icons.length).toBe(links.length);
    expect(hasMarkup(actions, "<template #icon><icon-safe /></template>")).toBe(true);
    expect(hasMarkup(actions, "<span>API权限</span>")).toBe(true);
  });
});

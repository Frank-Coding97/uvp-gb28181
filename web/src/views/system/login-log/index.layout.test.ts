import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const source = readFileSync(resolve(process.cwd(), "src/views/system/login-log/index.vue"), "utf8");

describe("login log page workspace layout", () => {
  it("uses the fill shell so the page never scrolls its own container", () => {
    expect(source).toContain('class="snow-fill login-log-page"');
    expect(source).toContain('class="snow-fill-inner uvp-page-shell-flat login-log-page__inner"');
    expect(source).not.toContain('class="snow-page login-log-page"');
    expect(source).not.toContain('class="snow-inner uvp-page-shell-flat login-log-page__inner"');
  });

  it("stacks the shell as a flex column that keeps the table as the only growable row", () => {
    expect(source).toMatch(/\.login-log-page\s*{[^}]*overflow:\s*hidden;/s);
    expect(source).toMatch(
      /\.login-log-page__inner\s*{[^}]*display:\s*flex;[^}]*flex-direction:\s*column;[^}]*overflow:\s*hidden;/s
    );
    expect(source).toMatch(/\.login-log-page__inner > :deep\(\.uvp-search-panel\)\s*{[^}]*flex:\s*0 0 auto;/s);
    expect(source).toMatch(/\.login-log-batch-bar\s*{[^}]*flex:\s*0 0 auto;/s);
    expect(source).toMatch(/\.login-log-table-wrap\s*{[^}]*flex:\s*1;[^}]*min-height:\s*0;[^}]*overflow:\s*hidden;/s);
    expect(source).toMatch(/\.login-log-table-wrap :deep\(\.uvp-data-table\)\s*{[^}]*height:\s*100%;[^}]*min-height:\s*0;/s);
  });

  it("moves the vertical scrollbar onto the table body once rows exist", () => {
    expect(source).toContain(
      'const tableScroll = computed(() => ({ x: "100%", minWidth: 1060, ...(logs.value.length ? { y: "100%" } : {}) }));'
    );
    expect(source).toContain('<div v-if="!error" class="login-log-table-wrap">');
    expect(source).toContain('class="uvp-data-table login-log-table"');
    expect(source).toContain(':scroll="tableScroll"');
  });

  it("keeps the search panel, error banner and batch bar above the scrolling table", () => {
    const searchIndex = source.indexOf("<s-layout-search>");
    const errorIndex = source.indexOf('class="login-log-error"');
    const batchIndex = source.indexOf('class="login-log-batch-bar"');
    const tableIndex = source.indexOf('class="login-log-table-wrap"');
    expect(searchIndex).toBeGreaterThan(-1);
    expect(errorIndex).toBeGreaterThan(searchIndex);
    expect(batchIndex).toBeGreaterThan(errorIndex);
    expect(tableIndex).toBeGreaterThan(batchIndex);
  });
});

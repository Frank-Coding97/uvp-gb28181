import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const source = readFileSync(resolve(process.cwd(), "src/views/system/online-user/index.vue"), "utf8");

describe("online user page workspace layout", () => {
  it("keeps vertical scrolling inside the table body", () => {
    expect(source).toContain('class="snow-fill online-user-page"');
    expect(source).toContain('class="snow-fill-inner uvp-page-shell-flat online-user-page__inner"');
    expect(source).toContain('...(sessions.value.length ? { y: "100%" } : {})');
    expect(source).toContain('<div v-else class="online-user-table-wrap">');
    expect(source).toContain(':scroll="tableScroll"');
  });

  it("keeps the search and error state outside the scrolling table region", () => {
    expect(source).toMatch(/\.online-user-page,\s*\.online-user-page__inner\s*\{[^}]*overflow:\s*hidden;/s);
    expect(source).toMatch(/\.online-user-page__inner\s*\{[^}]*display:\s*flex;[^}]*flex-direction:\s*column;/s);
    expect(source).toMatch(/\.online-user-table-wrap\s*\{[^}]*flex:\s*1;[^}]*min-height:\s*0;[^}]*overflow:\s*hidden;/s);
    expect(source).toMatch(/\.online-user-table-wrap :deep\(\.uvp-data-table\)\s*\{[^}]*height:\s*100%;[^}]*min-height:\s*0;/s);
  });
});

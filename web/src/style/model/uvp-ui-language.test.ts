import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const source = readFileSync(resolve(process.cwd(), "src/style/model/uvp-ui-language.scss"), "utf8");

describe("table action spacing", () => {
  it("keeps one compact 4px gap between operation icons and labels", () => {
    expect(source).toMatch(/\.uvp-data-table \.uvp-table-action,\s*\.uvp-data-table \.arco-link\s*\{[^}]*gap:\s*4px;/s);
    expect(source).toMatch(/\.uvp-data-table \.arco-link-icon\s*\{[^}]*margin-right:\s*0;/s);
    expect(source).toMatch(/\.uvp-data-table \.arco-btn:not\(\.arco-btn-only-icon\) \.arco-btn-icon\s*\{[^}]*margin-right:\s*4px;/s);
  });
});

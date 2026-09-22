import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

import { hasRuleBlock } from "@/test/source-assert";

const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/playback-log/index.vue"), "utf8");

describe("playback log layout", () => {
  it("keeps vertical scrolling inside the table region", () => {
    expect(source).toContain('class="snow-fill playback-log-page"');
    expect(source).toContain('class="playback-log-table"');
    expect(source).toMatch(/\{\s*x:\s*1320,\s*y:\s*["']100%["']\s*\}/);
    // ⛔ 别钉声明顺序：stylelint-config-recess-order 会重排，格式化一次红一次。
    expect(hasRuleBlock(source, ".playback-log-page", "overflow: hidden")).toBe(true);
    expect(hasRuleBlock(source, ".playback-log-table", "flex: 1", "min-height: 0", "overflow: hidden")).toBe(true);
  });
});

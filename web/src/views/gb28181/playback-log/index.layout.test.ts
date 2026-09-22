import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/playback-log/index.vue"), "utf8");

describe("playback log layout", () => {
  it("keeps vertical scrolling inside the table region", () => {
    expect(source).toContain('class="snow-fill playback-log-page"');
    expect(source).toContain('class="playback-log-table"');
    expect(source).toMatch(/\{\s*x:\s*1320,\s*y:\s*["']100%["']\s*\}/);
    expect(source).toMatch(/\.playback-log-page\s*\{[^}]*overflow:\s*hidden/s);
    expect(source).toMatch(/\.playback-log-table\s*\{[^}]*min-height:\s*0[^}]*flex:\s*1[^}]*overflow:\s*hidden/s);
  });
});

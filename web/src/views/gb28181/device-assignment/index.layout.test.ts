import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/device-assignment/index.vue"), "utf-8");

describe("device assignment department tree", () => {
  it("expands the fully loaded department tree by default", () => {
    const sidebarTree = source.match(/<a-tree[\s\S]*?<\/a-tree>/)?.[0] ?? "";

    expect(sidebarTree).toContain('v-if="deptTree.length"');
    expect(sidebarTree).toContain("default-expand-all");
  });

  it("keeps vertical scrolling inside the device table", () => {
    expect(source).toMatch(/\.right-box\s*\{[\s\S]*?min-height:\s*0;[\s\S]*?overflow:\s*hidden;/);
  });

  it("merges device name and device code into one two-line cell", () => {
    expect(source).toContain('title="设备"');
    expect(source).not.toContain('title="设备编号"');
    expect(source).not.toContain('title="设备名称"');
    expect(source).toContain("device-cell__name");
    expect(source).toContain("device-cell__code");

    const cellStyle = source.match(/\.device-cell\s*\{[\s\S]*?\n\}/)?.[0] ?? "";
    expect(cellStyle).toContain("flex-direction: column");
    expect(cellStyle).toContain("min-width: 0");
  });
});

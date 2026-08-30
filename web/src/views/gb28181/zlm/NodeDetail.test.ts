import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/zlm/NodeDetail.vue"), "utf8");

describe("legacy node detail route", () => {
  it("reuses the canonical detail panel without keeping a second page implementation", () => {
    expect(source).toContain("NodeDetailPanel");
    expect(source).toContain(":canonical=\"false\"");
    expect(source).not.toContain("getZLMNode");
  });
});

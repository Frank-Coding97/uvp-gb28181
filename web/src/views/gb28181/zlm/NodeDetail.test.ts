import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/zlm/NodeDetail.vue"), "utf8");

describe("legacy node service config route", () => {
  it("reuses the canonical service config panel without keeping a second page implementation", () => {
    expect(source).toContain("NodeDetailPanel");
    expect(source).toContain(":canonical=\"false\"");
    expect(source).not.toContain("getZLMNode");
  });
});

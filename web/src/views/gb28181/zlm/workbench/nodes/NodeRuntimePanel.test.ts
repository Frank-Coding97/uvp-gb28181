import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/zlm/workbench/nodes/NodeRuntimePanel.vue"), "utf8");

describe("NodeRuntimePanel", () => {
  it("uses the route node id directly and does not load a second node directory", () => {
    expect(source).toContain("RuntimeSummaryPanel");
    expect(source).toContain("nodeId");
    expect(source).not.toContain("listZLMNodes");
    expect(source).not.toContain("ZLMNodeContextBar");
  });

  it("clears trend samples when the canonical node changes and keeps unavailable values honest", () => {
    expect(source).toContain("active");
    expect(source).toContain(":node-id=\"nodeId\"");
    expect(source).not.toContain(":scope=");
    expect(source).toContain("drilldown");
  });
});

import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/zlm/workbench/NodeManagement.vue"), "utf8");

describe("canonical node management workbench", () => {
  it("places the scheduling strategy above the node list without adding a local tab", () => {
    expect(source).toContain("MediaWorkspaceShell");
    expect(source).toContain("SchedulerStrategyPanel");
    expect(source).toContain("NodeListPanel");
    expect(source).toContain('useMediaWorkspaceRoute("nodes")');
    expect(source).toContain('key: "list"');
    expect(source).toContain(':show-scope="false"');
    expect(source.indexOf("<SchedulerStrategyPanel")).toBeLessThan(source.indexOf("<NodeListPanel"));
    expect(source).toContain('class="node-management-content"');
    expect(source).not.toContain('key: "strategy"');
  });

  it("does not own a duplicate node catalog or rewrite the initial view query", () => {
    expect(source).not.toContain("listZLMNodes");
    expect(source).not.toContain("router.replace");
    expect(source).toContain("workspace.nodes.value");
  });

  it("shows the full node ledger even when the URL carries a selected node", () => {
    expect(source.match(/\bscope="all"/g)).toHaveLength(2);
    expect(source).not.toContain(':scope="workspace.scope.value"');
  });
});

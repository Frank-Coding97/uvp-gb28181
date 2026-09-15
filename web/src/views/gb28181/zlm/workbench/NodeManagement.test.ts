import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/zlm/workbench/NodeManagement.vue"), "utf8");

describe("canonical node management workbench", () => {
  it("provides a list view through the shared workbench shell", () => {
    expect(source).toContain("MediaWorkspaceShell");
    expect(source).toContain("NodeListPanel");
    expect(source).toContain('useMediaWorkspaceRoute("nodes")');
    expect(source).toContain('key: "list"');
  });

  it("does not own a duplicate node catalog or rewrite the initial view query", () => {
    expect(source).not.toContain("listZLMNodes");
    expect(source).not.toContain("router.replace");
    expect(source).toContain("workspace.nodes.value");
  });
});

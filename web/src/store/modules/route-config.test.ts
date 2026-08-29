import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

describe("route config remains the authorization truth", () => {
  it("stores the backend route tree and never injects visual media groups", () => {
    const source = readFileSync(resolve(process.cwd(), "src/store/modules/route-config.ts"), "utf8");

    expect(source).toContain("routeTree.value = data");
    expect(source).toContain("routeList.value = flatRoute");
    expect(source).not.toMatch(/media-menu-sections|menu-item-group|media-section-/i);
  });
});

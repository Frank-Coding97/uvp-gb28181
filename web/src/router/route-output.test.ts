import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

describe("route output", () => {
  it("stores tabs and cache only after current-route highlighting", () => {
    const source = readFileSync(resolve(process.cwd(), "src/router/route-output.ts"), "utf8");
    const currentIndex = source.indexOf("store.setCurrentRoute(route)");
    const tabsIndex = source.indexOf("store.setTabs(route)", currentIndex);
    const cacheIndex = source.indexOf("store.setRoutePaths(resolveMediaRouteRenderKey(current))");

    expect(currentIndex).toBeGreaterThan(-1);
    expect(tabsIndex).toBeGreaterThan(currentIndex);
    expect(cacheIndex).toBeGreaterThan(currentIndex);
  });

  it("no longer special-cases retired media compatibility routes", () => {
    const source = readFileSync(resolve(process.cwd(), "src/router/route-output.ts"), "utf8");
    expect(source).not.toContain("shouldSkipRouteHistory");
    expect(source).not.toContain("legacyMedia");
  });
});

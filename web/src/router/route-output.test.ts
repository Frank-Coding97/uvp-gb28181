import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

import { shouldSkipRouteHistory } from "./route-output";

describe("transient legacy media route history", () => {
  it("skips tabs and cache only for explicit compatibility routes", () => {
    expect(shouldSkipRouteHistory({ meta: { legacyMedia: true } })).toBe(true);
    expect(shouldSkipRouteHistory({ meta: { legacyMedia: false } })).toBe(false);
    expect(shouldSkipRouteHistory({ meta: {} })).toBe(false);
  });

  it("updates current-route highlighting before returning without tabs or cache", () => {
    const source = readFileSync(resolve(process.cwd(), "src/router/route-output.ts"), "utf8");
    const currentIndex = source.indexOf("store.setCurrentRoute(route)");
    const legacyIndex = source.indexOf("shouldSkipRouteHistory(route)", currentIndex);
    const tabsIndex = source.indexOf("store.setTabs(route)", legacyIndex);
    const cacheIndex = source.indexOf("store.setRoutePaths(resolveMediaRouteRenderKey(current))");

    expect(currentIndex).toBeGreaterThan(-1);
    expect(legacyIndex).toBeGreaterThan(currentIndex);
    expect(tabsIndex).toBeGreaterThan(legacyIndex);
    expect(cacheIndex).toBeGreaterThan(legacyIndex);
  });
});

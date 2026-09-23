import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const source = readFileSync(resolve(process.cwd(), "src/layout/components/Main/index.vue"), "utf8");

describe("layout main route identity", () => {
  it("uses the scoped media render-key helper for component identity", () => {
    expect(source).toContain('import { resolveMediaRouteRenderKey } from "./mediaRouteKey"');
    expect(source).toContain(':key="resolveMediaRouteRenderKey(route)"');
    expect(source).not.toContain(':key="route.fullPath"');
  });

  it("keeps the wrapper identity aligned with the rendered and cached media route key", () => {
    expect(source).toContain("const wrapperName = resolveMediaRouteRenderKey(route)");
    expect(source).not.toContain("const wrapperName = route.fullPath");
    expect(source).toContain("if (!route.meta?.keepAlive) return component");
  });
});

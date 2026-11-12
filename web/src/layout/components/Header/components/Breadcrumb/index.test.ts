import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const source = readFileSync(resolve(process.cwd(), "src/layout/components/Header/components/Breadcrumb/index.vue"), "utf8");

describe("system breadcrumb", () => {
  it("uses the actual home route and does not prepend the current route twice", () => {
    expect(source).toContain("item.path === HOME_PATH");
    expect(source).toContain("homeRoute.name !== route.name");
  });
});

import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";
import { STANDALONE_SETUP_PATH } from "@/api/standalone-setup";
import { staticRoutes } from "./route";

describe("standalone setup route", () => {
  it("is a hidden anonymous static route beside login", () => {
    const route = staticRoutes.find(item => item.path === STANDALONE_SETUP_PATH);
    expect(route).toMatchObject({
      path: "/standalone-setup",
      name: "standalone-setup",
      meta: { hide: true }
    });
    expect(staticRoutes.some(item => item.path === "/login")).toBe(true);
  });

  it("probes setup status before applying the legacy refresh-token guard", () => {
    const source = readFileSync(resolve(process.cwd(), "src/router/index.ts"), "utf8");
    expect(source).toContain("const standaloneProbe = await loadStandaloneSetupStatus();");
    expect(source.indexOf("standaloneProbe")).toBeLessThan(source.indexOf("const tokenExist = hasRefreshToken();"));
    expect(source).toContain("if (standaloneNavigation) return next(standaloneNavigation);");
  });
});
